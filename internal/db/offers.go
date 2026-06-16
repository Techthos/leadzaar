package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/techthos/microapp-crm/internal/models"
	bolt "go.etcd.io/bbolt"
)

// errMissingLead is returned when an Offer references a Lead that does not exist
// (mirrors errMissingContact for deals).
var errMissingLead = errors.New("lead does not exist")

// OfferFilter narrows a ListOffers query. A zero LeadID means "no filter"
// (every offer); a non-zero LeadID lists only that lead's offers.
type OfferFilter struct {
	LeadID uint64
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
