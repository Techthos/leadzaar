package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/techthos/leadzaar/internal/models"
	bolt "go.etcd.io/bbolt"
)

// errInvalidStage / errMissingContact / errCurrencyRequired are validation
// failures surfaced by the deal operations.
var (
	errInvalidStage     = errors.New("invalid deal stage")
	errMissingContact   = errors.New("contact does not exist")
	errCurrencyRequired = errors.New("currency required for non-zero value")
)

// DealFilter narrows a ListDeals query. Zero values mean "no filter": a 0
// ContactID matches any contact, an empty Stage matches any stage. The two
// compose (both must match).
type DealFilter struct {
	ContactID uint64
	Stage     models.DealStage
}

// CreateDeal inserts a new deal for an existing contact (UC-13). Title is
// required, Stage must be a valid enum value, and a non-zero Value requires a
// currency. A fresh ID is assigned and an idx_deal_by_contact entry is written.
func (s *Store) CreateDeal(d models.Deal) (models.Deal, error) {
	if err := validateDeal(d); err != nil {
		return models.Deal{}, fmt.Errorf("create deal: %w", err)
	}
	now := s.now()
	d.CreatedAt = now
	d.UpdatedAt = now

	err := s.update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketContacts).Get(itob(d.ContactID)) == nil {
			return fmt.Errorf("contact %d: %w", d.ContactID, errMissingContact)
		}
		if err := checkCompanyRef(tx, d.CompanyID); err != nil {
			return err
		}
		b := tx.Bucket(bucketDeals)
		id, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("next deal id: %w", err)
		}
		d.ID = id
		if err := putJSON(b, itob(id), d); err != nil {
			return fmt.Errorf("put deal %d: %w", id, err)
		}
		if err := tx.Bucket(bucketDealByContact).Put(dealByContactIndexKey(d.ContactID, id), nil); err != nil {
			return fmt.Errorf("index deal %d by contact: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return models.Deal{}, err
	}
	return d, nil
}

// GetDeal fetches a deal by ID (UC-15), returning ErrNotFound if absent.
func (s *Store) GetDeal(id uint64) (models.Deal, error) {
	var d models.Deal
	err := s.view(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketDeals).Get(itob(id))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &d)
	})
	if err != nil {
		return models.Deal{}, fmt.Errorf("get deal %d: %w", id, err)
	}
	return d, nil
}

// ListDeals returns deals matching the filter (UC-14). When ContactID is set it
// scans the idx_deal_by_contact index; otherwise it scans all deals. A Stage
// filter is applied on top. Results are in deal-creation (ascending ID) order.
func (s *Store) ListDeals(f DealFilter) ([]models.Deal, error) {
	if f.Stage != "" && !f.Stage.Valid() {
		return nil, fmt.Errorf("list deals: %w", errInvalidStage)
	}
	var out []models.Deal
	err := s.view(func(tx *bolt.Tx) error {
		deals := tx.Bucket(bucketDeals)
		appendIfMatch := func(v []byte) error {
			var d models.Deal
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			if f.Stage == "" || d.Stage == f.Stage {
				out = append(out, d)
			}
			return nil
		}

		if f.ContactID != 0 {
			prefix := itob(f.ContactID)
			c := tx.Bucket(bucketDealByContact).Cursor()
			for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
				v := deals.Get(itob(btoi(k[8:])))
				if v == nil {
					continue
				}
				if err := appendIfMatch(v); err != nil {
					return err
				}
			}
			return nil
		}
		return deals.ForEach(func(_, v []byte) error { return appendIfMatch(v) })
	})
	if err != nil {
		return nil, fmt.Errorf("list deals: %w", err)
	}
	return out, nil
}

// DealsForContact returns every deal owned by a contact (UC-12), via the index.
func (s *Store) DealsForContact(contactID uint64) ([]models.Deal, error) {
	return s.ListDeals(DealFilter{ContactID: contactID})
}

// UpdateDeal persists field changes to an existing deal (UC-16). ID and
// CreatedAt are immutable; UpdatedAt advances. If ContactID changes, the new
// contact must exist and idx_deal_by_contact is rewritten. Returns ErrNotFound
// if the deal does not exist.
func (s *Store) UpdateDeal(d models.Deal) (models.Deal, error) {
	if err := validateDeal(d); err != nil {
		return models.Deal{}, fmt.Errorf("update deal: %w", err)
	}
	err := s.update(func(tx *bolt.Tx) error {
		if err := checkCompanyRef(tx, d.CompanyID); err != nil {
			return err
		}
		b := tx.Bucket(bucketDeals)
		existingRaw := b.Get(itob(d.ID))
		if existingRaw == nil {
			return ErrNotFound
		}
		var existing models.Deal
		if err := json.Unmarshal(existingRaw, &existing); err != nil {
			return err
		}
		d.CreatedAt = existing.CreatedAt
		d.UpdatedAt = s.now()

		if d.ContactID != existing.ContactID {
			if tx.Bucket(bucketContacts).Get(itob(d.ContactID)) == nil {
				return fmt.Errorf("contact %d: %w", d.ContactID, errMissingContact)
			}
			idx := tx.Bucket(bucketDealByContact)
			if err := idx.Delete(dealByContactIndexKey(existing.ContactID, d.ID)); err != nil {
				return fmt.Errorf("delete old deal index: %w", err)
			}
			if err := idx.Put(dealByContactIndexKey(d.ContactID, d.ID), nil); err != nil {
				return fmt.Errorf("put new deal index: %w", err)
			}
		}
		if err := putJSON(b, itob(d.ID), d); err != nil {
			return fmt.Errorf("put deal %d: %w", d.ID, err)
		}
		return nil
	})
	if err != nil {
		return models.Deal{}, fmt.Errorf("update deal %d: %w", d.ID, err)
	}
	return d, nil
}

// DeleteDeal removes a deal and its idx_deal_by_contact entry (UC-17). Returns
// ErrNotFound if the deal does not exist.
func (s *Store) DeleteDeal(id uint64) error {
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDeals)
		raw := b.Get(itob(id))
		if raw == nil {
			return ErrNotFound
		}
		var d models.Deal
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		if err := tx.Bucket(bucketDealByContact).Delete(dealByContactIndexKey(d.ContactID, id)); err != nil {
			return fmt.Errorf("delete deal index: %w", err)
		}
		return b.Delete(itob(id))
	})
	if err != nil {
		return fmt.Errorf("delete deal %d: %w", id, err)
	}
	return nil
}

// validateDeal enforces the deal invariants shared by create and update.
func validateDeal(d models.Deal) error {
	if strings.TrimSpace(d.Title) == "" {
		return errEmptyName
	}
	if !d.Stage.Valid() {
		return errInvalidStage
	}
	if d.Value != 0 && strings.TrimSpace(d.Currency) == "" {
		return errCurrencyRequired
	}
	return nil
}
