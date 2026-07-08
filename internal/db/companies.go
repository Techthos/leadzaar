package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/techthos/leadzaar/internal/models"
	bolt "go.etcd.io/bbolt"
)

// errMissingCompany is returned when a CompanyID references a company that does
// not exist (mirrors errMissingContact for deals).
var errMissingCompany = errors.New("company does not exist")

// CreateCompany inserts a new company. Name is required; a fresh big-endian
// surrogate ID is assigned and timestamps are set.
func (s *Store) CreateCompany(c models.Company) (models.Company, error) {
	if strings.TrimSpace(c.Name) == "" {
		return models.Company{}, fmt.Errorf("create company: %w", errEmptyName)
	}
	now := s.now()
	c.CreatedAt = now
	c.UpdatedAt = now

	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCompanies)
		id, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("next company id: %w", err)
		}
		c.ID = id
		if err := putJSON(b, itob(id), c); err != nil {
			return fmt.Errorf("put company %d: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return models.Company{}, err
	}
	return c, nil
}

// GetCompany fetches a company by ID, returning ErrNotFound if absent.
func (s *Store) GetCompany(id uint64) (models.Company, error) {
	var c models.Company
	err := s.view(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketCompanies).Get(itob(id))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &c)
	})
	if err != nil {
		return models.Company{}, fmt.Errorf("get company %d: %w", id, err)
	}
	return c, nil
}

// ListCompanies returns all companies in creation (ascending ID) order.
func (s *Store) ListCompanies() ([]models.Company, error) {
	var out []models.Company
	err := s.view(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCompanies).ForEach(func(_, v []byte) error {
			var c models.Company
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			out = append(out, c)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	return out, nil
}

// SearchCompanies returns companies whose name, website, or industry contains
// query (case-insensitive) via a full scan. A blank query returns all companies.
func (s *Store) SearchCompanies(query string) ([]models.Company, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return s.ListCompanies()
	}
	var out []models.Company
	err := s.view(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCompanies).ForEach(func(_, v []byte) error {
			var c models.Company
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			if companyMatches(c, q) {
				out = append(out, c)
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("search companies %q: %w", query, err)
	}
	return out, nil
}

// companyMatches reports whether company c matches the already-lowercased query.
func companyMatches(c models.Company, q string) bool {
	return strings.Contains(strings.ToLower(c.Name), q) ||
		strings.Contains(strings.ToLower(c.Website), q) ||
		strings.Contains(strings.ToLower(c.Industry), q)
}

// CompanyNames returns an id→name map of every company, for resolving a record's
// CompanyID to a display name without a round-trip per record. The zero ID maps
// to "" implicitly (absent from the map).
func (s *Store) CompanyNames() (map[uint64]string, error) {
	var names map[uint64]string
	err := s.view(func(tx *bolt.Tx) error {
		names = companyNames(tx)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("company names: %w", err)
	}
	return names, nil
}

// UpdateCompany persists field changes to an existing company. ID and CreatedAt
// are immutable (preserved from the stored record); UpdatedAt advances. Returns
// ErrNotFound if the company does not exist.
func (s *Store) UpdateCompany(c models.Company) (models.Company, error) {
	if strings.TrimSpace(c.Name) == "" {
		return models.Company{}, fmt.Errorf("update company: %w", errEmptyName)
	}
	err := s.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCompanies)
		existingRaw := b.Get(itob(c.ID))
		if existingRaw == nil {
			return ErrNotFound
		}
		var existing models.Company
		if err := json.Unmarshal(existingRaw, &existing); err != nil {
			return err
		}
		c.CreatedAt = existing.CreatedAt
		c.UpdatedAt = s.now()
		if err := putJSON(b, itob(c.ID), c); err != nil {
			return fmt.Errorf("put company %d: %w", c.ID, err)
		}
		return nil
	})
	if err != nil {
		return models.Company{}, fmt.Errorf("update company %d: %w", c.ID, err)
	}
	return c, nil
}

// DeleteCompany deletes a company and unlinks it from every referencing record,
// atomically: any Lead, Contact, or Deal whose CompanyID is this company has its
// CompanyID reset to 0 (the records themselves are kept). It returns the number
// of records unlinked. Returns ErrNotFound if the company does not exist.
func (s *Store) DeleteCompany(id uint64) (int, error) {
	var unlinked int
	err := s.update(func(tx *bolt.Tx) error {
		companies := tx.Bucket(bucketCompanies)
		if companies.Get(itob(id)) == nil {
			return ErrNotFound
		}

		n, err := unlinkCompanyFromLeads(tx, id)
		if err != nil {
			return err
		}
		unlinked += n
		n, err = unlinkCompanyFromContacts(tx, id)
		if err != nil {
			return err
		}
		unlinked += n
		n, err = unlinkCompanyFromDeals(tx, id)
		if err != nil {
			return err
		}
		unlinked += n

		return companies.Delete(itob(id))
	})
	if err != nil {
		return 0, fmt.Errorf("delete company %d: %w", id, err)
	}
	return unlinked, nil
}

// unlinkCompanyFromLeads resets CompanyID to 0 on every lead referencing the
// company. It collects matches first, then rewrites them, since a bucket must
// not be mutated mid-ForEach.
func unlinkCompanyFromLeads(tx *bolt.Tx, companyID uint64) (int, error) {
	b := tx.Bucket(bucketLeads)
	var matches []models.Lead
	if err := b.ForEach(func(_, v []byte) error {
		var l models.Lead
		if err := json.Unmarshal(v, &l); err != nil {
			return err
		}
		if l.CompanyID == companyID {
			l.CompanyID = 0
			matches = append(matches, l)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	for _, l := range matches {
		if err := putJSON(b, itob(l.ID), l); err != nil {
			return 0, fmt.Errorf("unlink company from lead %d: %w", l.ID, err)
		}
	}
	return len(matches), nil
}

// unlinkCompanyFromContacts resets CompanyID to 0 on every contact referencing
// the company.
func unlinkCompanyFromContacts(tx *bolt.Tx, companyID uint64) (int, error) {
	b := tx.Bucket(bucketContacts)
	var matches []models.Contact
	if err := b.ForEach(func(_, v []byte) error {
		var c models.Contact
		if err := json.Unmarshal(v, &c); err != nil {
			return err
		}
		if c.CompanyID == companyID {
			c.CompanyID = 0
			matches = append(matches, c)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	for _, c := range matches {
		if err := putJSON(b, itob(c.ID), c); err != nil {
			return 0, fmt.Errorf("unlink company from contact %d: %w", c.ID, err)
		}
	}
	return len(matches), nil
}

// unlinkCompanyFromDeals resets CompanyID to 0 on every deal referencing the
// company.
func unlinkCompanyFromDeals(tx *bolt.Tx, companyID uint64) (int, error) {
	b := tx.Bucket(bucketDeals)
	var matches []models.Deal
	if err := b.ForEach(func(_, v []byte) error {
		var d models.Deal
		if err := json.Unmarshal(v, &d); err != nil {
			return err
		}
		if d.CompanyID == companyID {
			d.CompanyID = 0
			matches = append(matches, d)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	for _, d := range matches {
		if err := putJSON(b, itob(d.ID), d); err != nil {
			return 0, fmt.Errorf("unlink company from deal %d: %w", d.ID, err)
		}
	}
	return len(matches), nil
}

// migrateLegacyCompany upgrades records persisted before Company became a
// first-class entity: a Lead or Contact stored as {"company":"Acme", ...} (a
// plain string) is converted to a CompanyID reference. For each such record it
// find-or-creates a Company by normalized name, sets companyId, and drops the
// legacy company key. It is idempotent — once a record is rewritten it no longer
// carries the legacy key, so a second run finds nothing. On a fresh/empty DB it
// is a no-op.
func (s *Store) migrateLegacyCompany(tx *bolt.Tx) error {
	companies := tx.Bucket(bucketCompanies)

	// Dedup index of existing companies by normalized name.
	byName := make(map[string]uint64)
	if err := companies.ForEach(func(_, v []byte) error {
		var c models.Company
		if err := json.Unmarshal(v, &c); err != nil {
			return err
		}
		byName[strings.ToLower(strings.TrimSpace(c.Name))] = c.ID
		return nil
	}); err != nil {
		return err
	}

	findOrCreate := func(name string) (uint64, error) {
		key := strings.ToLower(strings.TrimSpace(name))
		if id, ok := byName[key]; ok {
			return id, nil
		}
		id, err := companies.NextSequence()
		if err != nil {
			return 0, fmt.Errorf("next company id: %w", err)
		}
		now := s.now()
		c := models.Company{ID: id, Name: strings.TrimSpace(name), CreatedAt: now, UpdatedAt: now}
		if err := putJSON(companies, itob(id), c); err != nil {
			return 0, fmt.Errorf("put migrated company %d: %w", id, err)
		}
		byName[key] = id
		return id, nil
	}

	for _, bucket := range [][]byte{bucketLeads, bucketContacts} {
		if err := migrateLegacyCompanyBucket(tx.Bucket(bucket), findOrCreate); err != nil {
			return fmt.Errorf("migrate legacy company in %q: %w", bucket, err)
		}
	}
	return nil
}

// migrateLegacyCompanyBucket rewrites every record in b that still carries a
// legacy string "company" field. Records are collected first (a bucket must not
// be mutated mid-ForEach) and rewritten after.
func migrateLegacyCompanyBucket(b *bolt.Bucket, findOrCreate func(string) (uint64, error)) error {
	type pending struct {
		key  []byte
		data map[string]json.RawMessage
	}
	var todo []pending
	if err := b.ForEach(func(k, v []byte) error {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(v, &m); err != nil {
			return err
		}
		if _, ok := m["company"]; !ok {
			return nil // already migrated / never had it
		}
		todo = append(todo, pending{key: append([]byte(nil), k...), data: m})
		return nil
	}); err != nil {
		return err
	}

	for _, p := range todo {
		var legacy string
		if err := json.Unmarshal(p.data["company"], &legacy); err != nil {
			return err
		}
		var companyID uint64
		if raw, ok := p.data["companyId"]; ok {
			if err := json.Unmarshal(raw, &companyID); err != nil {
				return err
			}
		}
		delete(p.data, "company") // drop the legacy key either way
		if strings.TrimSpace(legacy) != "" && companyID == 0 {
			id, err := findOrCreate(legacy)
			if err != nil {
				return err
			}
			idJSON, err := json.Marshal(id)
			if err != nil {
				return err
			}
			p.data["companyId"] = idJSON
		}
		encoded, err := json.Marshal(p.data)
		if err != nil {
			return err
		}
		if err := b.Put(p.key, encoded); err != nil {
			return err
		}
	}
	return nil
}

// checkCompanyRef verifies that a non-zero CompanyID references an existing
// company within tx, returning errMissingCompany otherwise. A zero ID (no link)
// always passes. Shared by lead/contact/deal create+update.
func checkCompanyRef(tx *bolt.Tx, companyID uint64) error {
	if companyID == 0 {
		return nil
	}
	if tx.Bucket(bucketCompanies).Get(itob(companyID)) == nil {
		return fmt.Errorf("company %d: %w", companyID, errMissingCompany)
	}
	return nil
}
