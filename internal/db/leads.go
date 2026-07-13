package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/techthos/leadzaar/internal/models"
	bolt "go.etcd.io/bbolt"
)

// errInvalidSource / errInvalidStatus / errInvalidQuality / errInvalidLeadSort
// are lead validation failures.
var (
	errInvalidSource   = errors.New("invalid lead source")
	errInvalidStatus   = errors.New("invalid lead status")
	errInvalidQuality  = errors.New("quality must be between 1 and 10 (0 = unscored)")
	errInvalidLeadSort = errors.New("invalid lead sort field (want created, quality, or updated)")
)

// LeadSort selects the field QueryLeads orders by. An empty value defaults to
// LeadSortCreated (creation order, i.e. by ID).
type LeadSort string

const (
	LeadSortCreated LeadSort = "created" // by ID (creation order)
	LeadSortQuality LeadSort = "quality" // by Quality score, ties broken by ID
	LeadSortUpdated LeadSort = "updated" // by UpdatedAt, ties broken by ID
)

// Valid reports whether o is a recognized sort field.
func (o LeadSort) Valid() bool {
	switch o {
	case LeadSortCreated, LeadSortQuality, LeadSortUpdated:
		return true
	default:
		return false
	}
}

// Lead listing page-size bounds for QueryLeads.
const (
	maxLeadPageSize     = 50
	defaultLeadPageSize = 50
)

// LeadQuery parameterizes QueryLeads (UC-2): an optional status filter, an
// optional case-insensitive substring Search over name/company/email/tags, a
// sort field + direction, and 1-based pagination. Zero values mean "no
// filter"; Page < 1 becomes 1, PageSize is clamped to [1, maxLeadPageSize]
// (0 takes the default), and SortBy "" defaults to last-updated order.
type LeadQuery struct {
	Status   models.LeadStatus
	Search   string
	SortBy   LeadSort
	Asc      bool // false (zero value) = descending: newest/highest first, the default
	Page     int
	PageSize int
}

// LeadPage is one page of QueryLeads results plus the pagination metadata an
// agent needs to walk subsequent pages. Total/TotalPages describe the full
// filtered set, not just this page.
type LeadPage struct {
	Leads      []models.Lead `json:"leads"`
	Page       int           `json:"page"`
	PageSize   int           `json:"pageSize"`
	Total      int           `json:"total"`
	TotalPages int           `json:"totalPages"`
	HasMore    bool          `json:"hasMore"`
}

// CreateLead inserts a new lead (UC-1). Name is required; Status defaults to
// "new" when blank and must otherwise be a valid value; a non-empty Source must
// be valid. Leads are not email-indexed in v1.
func (s *Store) CreateLead(l models.Lead) (models.Lead, error) {
	if strings.TrimSpace(l.Name) == "" {
		return models.Lead{}, fmt.Errorf("create lead: %w", errEmptyName)
	}
	if l.Status == "" {
		l.Status = models.StatusNew
	}
	if err := validateLeadEnums(l); err != nil {
		return models.Lead{}, fmt.Errorf("create lead: %w", err)
	}
	now := s.now()
	l.CreatedAt = now
	l.UpdatedAt = now

	err := s.update(func(tx *bolt.Tx) error {
		if err := checkCompanyRef(tx, l.CompanyID); err != nil {
			return err
		}
		b := tx.Bucket(bucketLeads)
		id, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("next lead id: %w", err)
		}
		l.ID = id
		if err := putJSON(b, itob(id), l); err != nil {
			return fmt.Errorf("put lead %d: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return models.Lead{}, err
	}
	return l, nil
}

// GetLead fetches a lead by ID (UC-3), returning ErrNotFound if absent.
func (s *Store) GetLead(id uint64) (models.Lead, error) {
	var l models.Lead
	err := s.view(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketLeads).Get(itob(id))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &l)
	})
	if err != nil {
		return models.Lead{}, fmt.Errorf("get lead %d: %w", id, err)
	}
	return l, nil
}

// ListLeads returns leads newest-first (descending ID), optionally filtered by
// status (UC-2). An empty status means "all"; an invalid status is rejected.
func (s *Store) ListLeads(status models.LeadStatus) ([]models.Lead, error) {
	if status != "" && !status.Valid() {
		return nil, fmt.Errorf("list leads: %w", errInvalidStatus)
	}
	var out []models.Lead
	err := s.view(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketLeads).Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var l models.Lead
			if err := json.Unmarshal(v, &l); err != nil {
				return err
			}
			if status == "" || l.Status == status {
				out = append(out, l)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list leads: %w", err)
	}
	return out, nil
}

// QueryLeads is the flexible, paginated lead listing behind the list_leads MCP
// tool (UC-2). It filters by status and/or a case-insensitive substring Search
// over name, linked company name, email, and tags; orders the full matching set
// by q.SortBy (default last-updated, q.Asc to reverse); then returns a single
// page sized per q.PageSize (clamped to [1, maxLeadPageSize]) along with the
// totals needed to page through the rest. Like the rest of v1, this is a full
// primary-bucket scan with in-memory filter/sort — no status or quality index.
func (s *Store) QueryLeads(q LeadQuery) (LeadPage, error) {
	if q.Status != "" && !q.Status.Valid() {
		return LeadPage{}, fmt.Errorf("query leads: %w", errInvalidStatus)
	}
	if q.SortBy != "" && !q.SortBy.Valid() {
		return LeadPage{}, fmt.Errorf("query leads: %w", errInvalidLeadSort)
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	switch {
	case size < 1:
		size = defaultLeadPageSize
	case size > maxLeadPageSize:
		size = maxLeadPageSize
	}
	sortBy := q.SortBy
	if sortBy == "" {
		sortBy = LeadSortUpdated
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))

	var matched []models.Lead
	err := s.view(func(tx *bolt.Tx) error {
		var names map[uint64]string
		if search != "" {
			names = companyNames(tx)
		}
		return tx.Bucket(bucketLeads).ForEach(func(_, v []byte) error {
			var l models.Lead
			if err := json.Unmarshal(v, &l); err != nil {
				return err
			}
			if q.Status != "" && l.Status != q.Status {
				return nil
			}
			if search != "" && !leadMatches(l, names[l.CompanyID], search) {
				return nil
			}
			matched = append(matched, l)
			return nil
		})
	})
	if err != nil {
		return LeadPage{}, fmt.Errorf("query leads: %w", err)
	}

	sortLeads(matched, sortBy, q.Asc)

	total := len(matched)
	totalPages := (total + size - 1) / size
	start := (page - 1) * size
	var pageLeads []models.Lead
	if start < total {
		end := start + size
		if end > total {
			end = total
		}
		pageLeads = matched[start:end]
	}
	return LeadPage{
		Leads:      pageLeads,
		Page:       page,
		PageSize:   size,
		Total:      total,
		TotalPages: totalPages,
		HasMore:    start+len(pageLeads) < total,
	}, nil
}

// sortLeads orders leads by the chosen field with ID as a stable, unique
// tiebreaker. The base comparison is ascending; when asc is false (the default)
// the whole order is reversed to descending. Because the tiebreak yields a
// strict total order, negating is well-defined.
func sortLeads(leads []models.Lead, by LeadSort, asc bool) {
	sort.SliceStable(leads, func(i, j int) bool {
		a, b := leads[i], leads[j]
		var less bool
		switch by {
		case LeadSortQuality:
			if a.Quality != b.Quality {
				less = a.Quality < b.Quality
			} else {
				less = a.ID < b.ID
			}
		case LeadSortUpdated:
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				less = a.UpdatedAt.Before(b.UpdatedAt)
			} else {
				less = a.ID < b.ID
			}
		default: // LeadSortCreated
			less = a.ID < b.ID
		}
		if asc {
			return less
		}
		return !less
	})
}

// leadMatches reports whether lead l matches the already-lowercased query q.
// companyName is the lead's linked company name ("" when unlinked), resolved by
// the caller so the company stays searchable even though it is now a reference.
func leadMatches(l models.Lead, companyName, q string) bool {
	if strings.Contains(strings.ToLower(l.Name), q) ||
		strings.Contains(strings.ToLower(companyName), q) ||
		strings.Contains(strings.ToLower(l.Email), q) {
		return true
	}
	for _, t := range l.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

// UpdateLead persists edits to a lead's editable fields (UC-4). ID, CreatedAt,
// and the conversion back-references (ContactID, DealID) are preserved from the
// stored record — only Convert sets those. UpdatedAt advances. Returns
// ErrNotFound if the lead does not exist.
func (s *Store) UpdateLead(l models.Lead) (models.Lead, error) {
	if strings.TrimSpace(l.Name) == "" {
		return models.Lead{}, fmt.Errorf("update lead: %w", errEmptyName)
	}
	if l.Status == "" {
		l.Status = models.StatusNew
	}
	if err := validateLeadEnums(l); err != nil {
		return models.Lead{}, fmt.Errorf("update lead: %w", err)
	}
	err := s.update(func(tx *bolt.Tx) error {
		if err := checkCompanyRef(tx, l.CompanyID); err != nil {
			return err
		}
		b := tx.Bucket(bucketLeads)
		raw := b.Get(itob(l.ID))
		if raw == nil {
			return ErrNotFound
		}
		var existing models.Lead
		if err := json.Unmarshal(raw, &existing); err != nil {
			return err
		}
		l.CreatedAt = existing.CreatedAt
		l.ContactID = existing.ContactID
		l.DealID = existing.DealID
		l.UpdatedAt = s.now()
		if err := putJSON(b, itob(l.ID), l); err != nil {
			return fmt.Errorf("put lead %d: %w", l.ID, err)
		}
		return nil
	})
	if err != nil {
		return models.Lead{}, fmt.Errorf("update lead %d: %w", l.ID, err)
	}
	return l, nil
}

// DeleteLead removes a lead by ID and cascades to all of its offers (UC-6),
// atomically: every offer made to the lead and those offers' idx_offer_by_lead
// entries are removed in the same transaction. It returns the IDs of the deleted
// offers. Returns ErrNotFound if the lead does not exist.
func (s *Store) DeleteLead(id uint64) ([]uint64, error) {
	var deletedOffers []uint64
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketLeads)
		if b.Get(itob(id)) == nil {
			return ErrNotFound
		}

		// Collect the lead's offers before mutating (don't delete mid-scan).
		offerIdx := tx.Bucket(bucketOfferByLead)
		prefix := itob(id)
		var idxKeys [][]byte
		cur := offerIdx.Cursor()
		for k, _ := cur.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cur.Next() {
			deletedOffers = append(deletedOffers, btoi(k[8:]))
			idxKeys = append(idxKeys, append([]byte(nil), k...)) // copy: key is txn-scoped
		}

		offers := tx.Bucket(bucketOffers)
		for i, offerID := range deletedOffers {
			if err := offers.Delete(itob(offerID)); err != nil {
				return fmt.Errorf("delete offer %d: %w", offerID, err)
			}
			if err := offerIdx.Delete(idxKeys[i]); err != nil {
				return fmt.Errorf("delete offer index for %d: %w", offerID, err)
			}
		}
		return b.Delete(itob(id))
	})
	if err != nil {
		return nil, fmt.Errorf("delete lead %d: %w", id, err)
	}
	return deletedOffers, nil
}

// validateLeadEnums checks Source (if set) and Status against their enums, and
// the optional Quality score against its 1–10 range (0 = unscored).
func validateLeadEnums(l models.Lead) error {
	if l.Source != "" && !l.Source.Valid() {
		return errInvalidSource
	}
	if !l.Status.Valid() {
		return errInvalidStatus
	}
	if l.Quality < 0 || l.Quality > 10 {
		return errInvalidQuality
	}
	return nil
}
