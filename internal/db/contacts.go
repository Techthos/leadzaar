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

// errInvalidContactSort is returned for an unrecognized QueryContacts sort field.
var errInvalidContactSort = errors.New("invalid contact sort field (want created or updated)")

// ContactSort selects the field QueryContacts orders by. An empty value defaults
// to ContactSortUpdated (last-updated first).
type ContactSort string

const (
	ContactSortCreated ContactSort = "created" // by ID (creation order)
	ContactSortUpdated ContactSort = "updated" // by UpdatedAt, ties broken by ID
)

// Valid reports whether o is a recognized sort field.
func (o ContactSort) Valid() bool {
	switch o {
	case ContactSortCreated, ContactSortUpdated:
		return true
	default:
		return false
	}
}

// ContactQuery parameterizes QueryContacts (UC-8): an optional exact Email lookup
// (via idx_contact_by_email), an optional Tag membership filter, an optional
// case-insensitive substring Search over name/company/email/tags, a sort field +
// direction, and 1-based pagination. Zero values mean "no filter"; SortBy ""
// defaults to last-updated order.
type ContactQuery struct {
	Email    string
	Tag      string
	Search   string
	SortBy   ContactSort
	Asc      bool // false (zero value) = descending: newest/most-recently-updated first
	Page     int
	PageSize int
}

// ContactPage is one page of QueryContacts results plus pagination metadata
// describing the full filtered set (mirrors LeadPage).
type ContactPage struct {
	Contacts   []models.Contact `json:"contacts"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
	Total      int              `json:"total"`
	TotalPages int              `json:"totalPages"`
	HasMore    bool             `json:"hasMore"`
}

// QueryContacts is the flexible, paginated contact listing behind list_contacts
// (UC-8). It seeds from the email index when Email is set (else a full scan),
// applies the Tag and Search filters in memory, orders by q.SortBy (default
// last-updated), and slices out one page. Full-scan + in-memory sort, like the
// rest of v1 — no per-field index.
func (s *Store) QueryContacts(q ContactQuery) (ContactPage, error) {
	if q.SortBy != "" && !q.SortBy.Valid() {
		return ContactPage{}, fmt.Errorf("query contacts: %w", errInvalidContactSort)
	}
	page, size := normalizePage(q.Page, q.PageSize)
	byUpdated := q.SortBy != ContactSortCreated
	search := strings.ToLower(strings.TrimSpace(q.Search))
	tag := strings.TrimSpace(q.Tag)

	var matched []models.Contact
	err := s.view(func(tx *bolt.Tx) error {
		names := companyNames(tx)
		consider := func(c models.Contact) {
			if tag != "" && !contactHasTag(c, tag) {
				return
			}
			if search != "" && !contactMatches(c, names[c.CompanyID], search) {
				return
			}
			matched = append(matched, c)
		}

		if q.Email != "" {
			prefix := contactEmailIndexPrefix(q.Email)
			if len(prefix) == 1 { // just the 0x00 separator → empty email, nothing to match
				return nil
			}
			contacts := tx.Bucket(bucketContacts)
			cur := tx.Bucket(bucketContactByEmail).Cursor()
			for k, _ := cur.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cur.Next() {
				v := contacts.Get(itob(btoi(k[len(k)-8:])))
				if v == nil {
					continue // index/primary skew; tolerate
				}
				var c models.Contact
				if err := json.Unmarshal(v, &c); err != nil {
					return err
				}
				consider(c)
			}
			return nil
		}

		return tx.Bucket(bucketContacts).ForEach(func(_, v []byte) error {
			var c models.Contact
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			consider(c)
			return nil
		})
	})
	if err != nil {
		return ContactPage{}, fmt.Errorf("query contacts: %w", err)
	}

	sortByCreatedUpdated(matched, byUpdated, q.Asc,
		func(c models.Contact) uint64 { return c.ID },
		func(c models.Contact) time.Time { return c.UpdatedAt })

	items, total, totalPages, hasMore := paginate(matched, page, size)
	return ContactPage{
		Contacts:   items,
		Page:       page,
		PageSize:   size,
		Total:      total,
		TotalPages: totalPages,
		HasMore:    hasMore,
	}, nil
}

// contactHasTag reports whether contact c carries a tag equal (case-insensitive)
// to tag.
func contactHasTag(c models.Contact, tag string) bool {
	for _, t := range c.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// CreateContact inserts a new contact (UC-7). Name is required. A fresh
// big-endian surrogate ID is assigned, timestamps are set, and an
// idx_contact_by_email entry is written when an email is present.
func (s *Store) CreateContact(c models.Contact) (models.Contact, error) {
	if strings.TrimSpace(c.Name) == "" {
		return models.Contact{}, fmt.Errorf("create contact: %w", errEmptyName)
	}
	now := s.now()
	c.CreatedAt = now
	c.UpdatedAt = now

	err := s.update(func(tx *bolt.Tx) error {
		if err := checkCompanyRef(tx, c.CompanyID); err != nil {
			return err
		}
		b := tx.Bucket(bucketContacts)
		id, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("next contact id: %w", err)
		}
		c.ID = id
		if err := putJSON(b, itob(id), c); err != nil {
			return fmt.Errorf("put contact %d: %w", id, err)
		}
		if normEmail(c.Email) != "" {
			if err := tx.Bucket(bucketContactByEmail).Put(contactEmailIndexKey(c.Email, id), nil); err != nil {
				return fmt.Errorf("index contact email %d: %w", id, err)
			}
		}
		return nil
	})
	if err != nil {
		return models.Contact{}, err
	}
	return c, nil
}

// GetContact fetches a contact by ID (UC-9), returning ErrNotFound if absent.
func (s *Store) GetContact(id uint64) (models.Contact, error) {
	var c models.Contact
	err := s.view(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketContacts).Get(itob(id))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &c)
	})
	if err != nil {
		return models.Contact{}, fmt.Errorf("get contact %d: %w", id, err)
	}
	return c, nil
}

// ListContacts returns all contacts in creation (ascending ID) order (UC-8).
func (s *Store) ListContacts() ([]models.Contact, error) {
	var out []models.Contact
	err := s.view(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketContacts).ForEach(func(_, v []byte) error {
			var c models.Contact
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			out = append(out, c)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	return out, nil
}

// FindContactsByEmail returns every contact whose (normalized) email matches,
// via the idx_contact_by_email index (UC-8). An empty query returns nil.
func (s *Store) FindContactsByEmail(email string) ([]models.Contact, error) {
	prefix := contactEmailIndexPrefix(email)
	if len(prefix) == 1 { // just the 0x00 separator → empty email, nothing to match
		return nil, nil
	}
	var out []models.Contact
	err := s.view(func(tx *bolt.Tx) error {
		contacts := tx.Bucket(bucketContacts)
		c := tx.Bucket(bucketContactByEmail).Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			id := btoi(k[len(k)-8:])
			v := contacts.Get(itob(id))
			if v == nil {
				continue // index/primary skew; tolerate
			}
			var contact models.Contact
			if err := json.Unmarshal(v, &contact); err != nil {
				return err
			}
			out = append(out, contact)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("find contacts by email %q: %w", email, err)
	}
	return out, nil
}

// SearchContacts returns contacts whose name, company, email, or any tag
// contains query (case-insensitive) via a full scan (UC-8). A blank query
// returns all contacts.
func (s *Store) SearchContacts(query string) ([]models.Contact, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return s.ListContacts()
	}
	var out []models.Contact
	err := s.view(func(tx *bolt.Tx) error {
		names := companyNames(tx)
		return tx.Bucket(bucketContacts).ForEach(func(_, v []byte) error {
			var c models.Contact
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			if contactMatches(c, names[c.CompanyID], q) {
				out = append(out, c)
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("search contacts %q: %w", query, err)
	}
	return out, nil
}

// contactMatches reports whether contact c matches the already-lowercased query.
// companyName is the contact's linked company name ("" when unlinked), resolved
// by the caller so the company is searchable even though it is now a reference.
func contactMatches(c models.Contact, companyName, q string) bool {
	if strings.Contains(strings.ToLower(c.Name), q) ||
		strings.Contains(strings.ToLower(companyName), q) ||
		strings.Contains(strings.ToLower(c.Email), q) {
		return true
	}
	for _, t := range c.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

// companyNames builds an id→name map of all companies within tx, for resolving
// a record's CompanyID to a display/search name.
func companyNames(tx *bolt.Tx) map[uint64]string {
	names := make(map[uint64]string)
	_ = tx.Bucket(bucketCompanies).ForEach(func(_, v []byte) error {
		var c models.Company
		if err := json.Unmarshal(v, &c); err != nil {
			return err
		}
		names[c.ID] = c.Name
		return nil
	})
	return names
}

// DeleteContact deletes a contact and cascades to all of its deals (UC-11),
// atomically: every deal owned by the contact, those deals' idx_deal_by_contact
// entries, and the contact's email-index entry are removed in one transaction.
// It returns the IDs of the deleted deals. Returns ErrNotFound if the contact
// does not exist.
func (s *Store) DeleteContact(id uint64) ([]uint64, error) {
	var deletedDeals []uint64
	err := s.update(func(tx *bolt.Tx) error {
		contacts := tx.Bucket(bucketContacts)
		raw := contacts.Get(itob(id))
		if raw == nil {
			return ErrNotFound
		}
		var c models.Contact
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}

		// Collect the contact's deals before mutating (don't delete mid-scan).
		dealIdx := tx.Bucket(bucketDealByContact)
		prefix := itob(id)
		var idxKeys [][]byte
		cur := dealIdx.Cursor()
		for k, _ := cur.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cur.Next() {
			deletedDeals = append(deletedDeals, btoi(k[8:]))
			idxKeys = append(idxKeys, append([]byte(nil), k...)) // copy: key is txn-scoped
		}

		deals := tx.Bucket(bucketDeals)
		for i, dealID := range deletedDeals {
			if err := deals.Delete(itob(dealID)); err != nil {
				return fmt.Errorf("delete deal %d: %w", dealID, err)
			}
			if err := dealIdx.Delete(idxKeys[i]); err != nil {
				return fmt.Errorf("delete deal index for %d: %w", dealID, err)
			}
		}
		if normEmail(c.Email) != "" {
			if err := tx.Bucket(bucketContactByEmail).Delete(contactEmailIndexKey(c.Email, id)); err != nil {
				return fmt.Errorf("delete contact email index: %w", err)
			}
		}
		return contacts.Delete(itob(id))
	})
	if err != nil {
		return nil, fmt.Errorf("delete contact %d: %w", id, err)
	}
	return deletedDeals, nil
}

// UpdateContact persists field changes to an existing contact (UC-10). ID and
// CreatedAt are immutable (preserved from the stored record); UpdatedAt advances.
// The email index is rewritten when the email changes. Returns ErrNotFound if
// the contact does not exist.
func (s *Store) UpdateContact(c models.Contact) (models.Contact, error) {
	if strings.TrimSpace(c.Name) == "" {
		return models.Contact{}, fmt.Errorf("update contact: %w", errEmptyName)
	}
	err := s.update(func(tx *bolt.Tx) error {
		if err := checkCompanyRef(tx, c.CompanyID); err != nil {
			return err
		}
		b := tx.Bucket(bucketContacts)
		existingRaw := b.Get(itob(c.ID))
		if existingRaw == nil {
			return ErrNotFound
		}
		var existing models.Contact
		if err := json.Unmarshal(existingRaw, &existing); err != nil {
			return err
		}
		c.CreatedAt = existing.CreatedAt
		c.UpdatedAt = s.now()

		oldEmail, newEmail := normEmail(existing.Email), normEmail(c.Email)
		if oldEmail != newEmail {
			idx := tx.Bucket(bucketContactByEmail)
			if oldEmail != "" {
				if err := idx.Delete(contactEmailIndexKey(existing.Email, c.ID)); err != nil {
					return fmt.Errorf("delete old email index: %w", err)
				}
			}
			if newEmail != "" {
				if err := idx.Put(contactEmailIndexKey(c.Email, c.ID), nil); err != nil {
					return fmt.Errorf("put new email index: %w", err)
				}
			}
		}
		if err := putJSON(b, itob(c.ID), c); err != nil {
			return fmt.Errorf("put contact %d: %w", c.ID, err)
		}
		return nil
	})
	if err != nil {
		return models.Contact{}, fmt.Errorf("update contact %d: %w", c.ID, err)
	}
	return c, nil
}
