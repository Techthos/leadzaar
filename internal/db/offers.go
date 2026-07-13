package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/techthos/leadzaar/internal/models"
	bolt "go.etcd.io/bbolt"
)

// errMissingLead / errInvalidOfferSort are offer validation failures.
var (
	errMissingLead      = errors.New("lead does not exist")
	errInvalidOfferSort = errors.New("invalid offer sort field (want created or updated)")
)

// OfferFilter narrows a ListOffers query. A zero LeadID means "no filter"
// (every offer); a non-zero LeadID lists only that lead's offers.
type OfferFilter struct {
	LeadID uint64
}

// OfferSort selects the field QueryOffers orders by. An empty value defaults to
// OfferSortUpdated (last-updated first).
type OfferSort string

const (
	OfferSortCreated OfferSort = "created" // by ID (creation order)
	OfferSortUpdated OfferSort = "updated" // by UpdatedAt, ties broken by ID
)

// Valid reports whether o is a recognized sort field.
func (o OfferSort) Valid() bool {
	switch o {
	case OfferSortCreated, OfferSortUpdated:
		return true
	default:
		return false
	}
}

// OfferQuery parameterizes QueryOffers (UC-25): an optional LeadID filter, an
// optional case-insensitive substring Search over title/subject, a sort field +
// direction, and 1-based pagination. SortBy "" defaults to last-updated order.
type OfferQuery struct {
	LeadID   uint64
	Search   string
	SortBy   OfferSort
	Asc      bool // false (zero value) = descending: most-recently-updated first
	Page     int
	PageSize int
}

// OfferPage is one page of QueryOffers results plus pagination metadata
// describing the full filtered set (mirrors LeadPage).
type OfferPage struct {
	Offers     []models.Offer `json:"offers"`
	Page       int            `json:"page"`
	PageSize   int            `json:"pageSize"`
	Total      int            `json:"total"`
	TotalPages int            `json:"totalPages"`
	HasMore    bool           `json:"hasMore"`
}

// CreateOffer inserts a new offer for an existing lead. Title is required and
// the LeadID must reference an existing lead. A fresh big-endian surrogate ID is
// assigned, timestamps are set, and an idx_offer_by_lead entry is written.
func (s *Store) CreateOffer(o models.Offer) (models.Offer, error) {
	if strings.TrimSpace(o.Title) == "" {
		return models.Offer{}, fmt.Errorf("create offer: %w", errEmptyName)
	}
	now := s.now()
	o.CreatedAt = now
	o.UpdatedAt = now

	err := s.update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketLeads).Get(itob(o.LeadID)) == nil {
			return fmt.Errorf("lead %d: %w", o.LeadID, errMissingLead)
		}
		b := tx.Bucket(bucketOffers)
		id, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("next offer id: %w", err)
		}
		o.ID = id
		if err := putJSON(b, itob(id), o); err != nil {
			return fmt.Errorf("put offer %d: %w", id, err)
		}
		if err := tx.Bucket(bucketOfferByLead).Put(offerByLeadIndexKey(o.LeadID, id), nil); err != nil {
			return fmt.Errorf("index offer %d by lead: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return models.Offer{}, err
	}
	return o, nil
}

// GetOffer fetches an offer by ID, returning ErrNotFound if absent.
func (s *Store) GetOffer(id uint64) (models.Offer, error) {
	var o models.Offer
	err := s.view(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketOffers).Get(itob(id))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &o)
	})
	if err != nil {
		return models.Offer{}, fmt.Errorf("get offer %d: %w", id, err)
	}
	return o, nil
}

// ListOffers returns offers matching the filter. When LeadID is set it scans the
// idx_offer_by_lead index; otherwise it scans all offers. Results are in
// offer-creation (ascending ID) order.
func (s *Store) ListOffers(f OfferFilter) ([]models.Offer, error) {
	var out []models.Offer
	err := s.view(func(tx *bolt.Tx) error {
		offers := tx.Bucket(bucketOffers)
		appendOffer := func(v []byte) error {
			var o models.Offer
			if err := json.Unmarshal(v, &o); err != nil {
				return err
			}
			out = append(out, o)
			return nil
		}

		if f.LeadID != 0 {
			prefix := itob(f.LeadID)
			c := tx.Bucket(bucketOfferByLead).Cursor()
			for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
				v := offers.Get(itob(btoi(k[8:])))
				if v == nil {
					continue
				}
				if err := appendOffer(v); err != nil {
					return err
				}
			}
			return nil
		}
		return offers.ForEach(func(_, v []byte) error { return appendOffer(v) })
	})
	if err != nil {
		return nil, fmt.Errorf("list offers: %w", err)
	}
	return out, nil
}

// OffersForLead returns every offer made to a lead, via the index.
func (s *Store) OffersForLead(leadID uint64) ([]models.Offer, error) {
	return s.ListOffers(OfferFilter{LeadID: leadID})
}

// QueryOffers is the flexible, paginated offer listing behind list_offers
// (UC-25). It seeds from idx_offer_by_lead when LeadID is set (else a full scan),
// applies the Search filter in memory, orders by q.SortBy (default last-updated),
// and slices out one page.
func (s *Store) QueryOffers(q OfferQuery) (OfferPage, error) {
	if q.SortBy != "" && !q.SortBy.Valid() {
		return OfferPage{}, fmt.Errorf("query offers: %w", errInvalidOfferSort)
	}
	page, size := normalizePage(q.Page, q.PageSize)
	byUpdated := q.SortBy != OfferSortCreated
	search := strings.ToLower(strings.TrimSpace(q.Search))

	var matched []models.Offer
	err := s.view(func(tx *bolt.Tx) error {
		consider := func(o models.Offer) {
			if search != "" && !offerMatches(o, search) {
				return
			}
			matched = append(matched, o)
		}

		offers := tx.Bucket(bucketOffers)
		if q.LeadID != 0 {
			prefix := itob(q.LeadID)
			c := tx.Bucket(bucketOfferByLead).Cursor()
			for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
				v := offers.Get(itob(btoi(k[8:])))
				if v == nil {
					continue
				}
				var o models.Offer
				if err := json.Unmarshal(v, &o); err != nil {
					return err
				}
				consider(o)
			}
			return nil
		}
		return offers.ForEach(func(_, v []byte) error {
			var o models.Offer
			if err := json.Unmarshal(v, &o); err != nil {
				return err
			}
			consider(o)
			return nil
		})
	})
	if err != nil {
		return OfferPage{}, fmt.Errorf("query offers: %w", err)
	}

	sortByCreatedUpdated(matched, byUpdated, q.Asc,
		func(o models.Offer) uint64 { return o.ID },
		func(o models.Offer) time.Time { return o.UpdatedAt })

	items, total, totalPages, hasMore := paginate(matched, page, size)
	return OfferPage{
		Offers:     items,
		Page:       page,
		PageSize:   size,
		Total:      total,
		TotalPages: totalPages,
		HasMore:    hasMore,
	}, nil
}

// offerMatches reports whether offer o matches the already-lowercased query q
// over its title and email subject. Description and body are not searched.
func offerMatches(o models.Offer, q string) bool {
	return strings.Contains(strings.ToLower(o.Title), q) ||
		strings.Contains(strings.ToLower(o.Subject), q)
}

// UpdateOffer persists field changes to an existing offer. ID and CreatedAt are
// immutable; UpdatedAt advances. If LeadID changes, the new lead must exist and
// idx_offer_by_lead is rewritten. Returns ErrNotFound if the offer does not
// exist.
func (s *Store) UpdateOffer(o models.Offer) (models.Offer, error) {
	if strings.TrimSpace(o.Title) == "" {
		return models.Offer{}, fmt.Errorf("update offer: %w", errEmptyName)
	}
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketOffers)
		existingRaw := b.Get(itob(o.ID))
		if existingRaw == nil {
			return ErrNotFound
		}
		var existing models.Offer
		if err := json.Unmarshal(existingRaw, &existing); err != nil {
			return err
		}
		o.CreatedAt = existing.CreatedAt
		o.UpdatedAt = s.now()

		if o.LeadID != existing.LeadID {
			if tx.Bucket(bucketLeads).Get(itob(o.LeadID)) == nil {
				return fmt.Errorf("lead %d: %w", o.LeadID, errMissingLead)
			}
			idx := tx.Bucket(bucketOfferByLead)
			if err := idx.Delete(offerByLeadIndexKey(existing.LeadID, o.ID)); err != nil {
				return fmt.Errorf("delete old offer index: %w", err)
			}
			if err := idx.Put(offerByLeadIndexKey(o.LeadID, o.ID), nil); err != nil {
				return fmt.Errorf("put new offer index: %w", err)
			}
		}
		if err := putJSON(b, itob(o.ID), o); err != nil {
			return fmt.Errorf("put offer %d: %w", o.ID, err)
		}
		return nil
	})
	if err != nil {
		return models.Offer{}, fmt.Errorf("update offer %d: %w", o.ID, err)
	}
	return o, nil
}

// DeleteOffer removes an offer and its idx_offer_by_lead entry. Returns
// ErrNotFound if the offer does not exist.
func (s *Store) DeleteOffer(id uint64) error {
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketOffers)
		raw := b.Get(itob(id))
		if raw == nil {
			return ErrNotFound
		}
		var o models.Offer
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		if err := tx.Bucket(bucketOfferByLead).Delete(offerByLeadIndexKey(o.LeadID, id)); err != nil {
			return fmt.Errorf("delete offer index: %w", err)
		}
		return b.Delete(itob(id))
	})
	if err != nil {
		return fmt.Errorf("delete offer %d: %w", id, err)
	}
	return nil
}
