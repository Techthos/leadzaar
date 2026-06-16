package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/techthos/microapp-crm/internal/models"
	bolt "go.etcd.io/bbolt"
)

// errInvalidSource / errInvalidStatus / errInvalidQuality are lead validation
// failures.
var (
	errInvalidSource  = errors.New("invalid lead source")
	errInvalidStatus  = errors.New("invalid lead status")
	errInvalidQuality = errors.New("quality must be between 1 and 10 (0 = unscored)")
)

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

// DeleteLead removes a lead by ID (UC-6). Returns ErrNotFound if absent.
func (s *Store) DeleteLead(id uint64) error {
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketLeads)
		if b.Get(itob(id)) == nil {
			return ErrNotFound
		}
		return b.Delete(itob(id))
	})
	if err != nil {
		return fmt.Errorf("delete lead %d: %w", id, err)
	}
	return nil
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
