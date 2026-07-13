package db_test

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
	bolt "go.etcd.io/bbolt"
)

func TestCompanyCRUD(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)

	t.Run("create assigns id and timestamps", func(t *testing.T) {
		got, err := store.CreateCompany(models.Company{Name: "Acme", Industry: "Tech"})
		if err != nil {
			t.Fatalf("CreateCompany: %v", err)
		}
		if got.ID == 0 {
			t.Errorf("ID not assigned: %+v", got)
		}
		if got.CreatedAt.IsZero() || !got.CreatedAt.Equal(got.UpdatedAt) {
			t.Errorf("timestamps wrong: %+v", got)
		}
	})

	t.Run("empty name rejected", func(t *testing.T) {
		if _, err := store.CreateCompany(models.Company{Name: "  "}); err == nil {
			t.Error("expected error for empty name, got nil")
		}
	})

	t.Run("get unknown is ErrNotFound", func(t *testing.T) {
		if _, err := store.GetCompany(99999); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("GetCompany(unknown) err = %v, want ErrNotFound", err)
		}
	})

	t.Run("update preserves id/createdAt, advances updatedAt", func(t *testing.T) {
		c, _ := store.CreateCompany(models.Company{Name: "Initech"})
		clk.advance(time.Hour)
		c.Name = "Initech LLC"
		got, err := store.UpdateCompany(c)
		if err != nil {
			t.Fatalf("UpdateCompany: %v", err)
		}
		if got.Name != "Initech LLC" {
			t.Errorf("name not updated: %+v", got)
		}
		if !got.CreatedAt.Equal(c.CreatedAt) || !got.UpdatedAt.After(c.CreatedAt) {
			t.Errorf("timestamps wrong: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
		}
	})
}

func TestSearchCompanies(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	if _, err := store.CreateCompany(models.Company{Name: "Acme", Industry: "Manufacturing", Website: "acme.example"}); err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	if _, err := store.CreateCompany(models.Company{Name: "Globex", Industry: "Energy"}); err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "name substring", query: "glob", want: 1},
		{name: "industry substring", query: "manufact", want: 1},
		{name: "website substring", query: "acme.example", want: 1},
		{name: "no match", query: "zzz", want: 0},
		{name: "blank returns all", query: "", want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.SearchCompanies(tc.query)
			if err != nil {
				t.Fatalf("SearchCompanies: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("SearchCompanies(%q) = %d, want %d", tc.query, len(got), tc.want)
			}
		})
	}
}

func TestQueryCompanies(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)

	c1, _ := store.CreateCompany(models.Company{Name: "Acme", Industry: "Manufacturing"})
	clk.advance(time.Hour)
	if _, err := store.CreateCompany(models.Company{Name: "Globex", Industry: "Energy"}); err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	clk.advance(time.Hour)
	c3, _ := store.CreateCompany(models.Company{Name: "Initech"})

	first := func(p db.CompanyPage) uint64 {
		if len(p.Companies) == 0 {
			return 0
		}
		return p.Companies[0].ID
	}

	t.Run("default last-updated-first with metadata", func(t *testing.T) {
		got, err := store.QueryCompanies(db.CompanyQuery{})
		if err != nil {
			t.Fatalf("QueryCompanies: %v", err)
		}
		if got.Total != 3 || first(got) != c3.ID {
			t.Errorf("default first = %d, want %d (total %d)", first(got), c3.ID, got.Total)
		}
	})

	t.Run("updated jumps to front", func(t *testing.T) {
		clk.advance(time.Hour)
		if _, err := store.UpdateCompany(c1); err != nil {
			t.Fatalf("UpdateCompany: %v", err)
		}
		got, _ := store.QueryCompanies(db.CompanyQuery{})
		if first(got) != c1.ID {
			t.Errorf("updated-first = %d, want %d", first(got), c1.ID)
		}
	})

	t.Run("search filter", func(t *testing.T) {
		got, _ := store.QueryCompanies(db.CompanyQuery{Search: "glob"})
		if got.Total != 1 {
			t.Errorf("search total = %d, want 1", got.Total)
		}
	})

	t.Run("pagination clamp and invalid sort rejected", func(t *testing.T) {
		clamp, _ := store.QueryCompanies(db.CompanyQuery{PageSize: 1000})
		if clamp.PageSize != 50 {
			t.Errorf("page_size = %d, want clamped to 50", clamp.PageSize)
		}
		if _, err := store.QueryCompanies(db.CompanyQuery{SortBy: db.CompanySort("bogus")}); err == nil {
			t.Error("expected error for bad sort")
		}
	})
}

func TestCompanyReferenceValidation(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	company, _ := store.CreateCompany(models.Company{Name: "Acme"})
	contact, _ := store.CreateContact(models.Contact{Name: "Buyer"})

	t.Run("non-existent company rejected on create", func(t *testing.T) {
		if _, err := store.CreateLead(models.Lead{Name: "L", CompanyID: 99999}); err == nil {
			t.Error("CreateLead with bad CompanyID: expected error, got nil")
		}
		if _, err := store.CreateContact(models.Contact{Name: "C", CompanyID: 99999}); err == nil {
			t.Error("CreateContact with bad CompanyID: expected error, got nil")
		}
		if _, err := store.CreateDeal(models.Deal{Title: "D", ContactID: contact.ID, CompanyID: 99999, Stage: models.StageProposal}); err == nil {
			t.Error("CreateDeal with bad CompanyID: expected error, got nil")
		}
	})

	t.Run("existing company accepted, zero accepted", func(t *testing.T) {
		if _, err := store.CreateLead(models.Lead{Name: "L", CompanyID: company.ID}); err != nil {
			t.Errorf("CreateLead with valid CompanyID: %v", err)
		}
		if _, err := store.CreateContact(models.Contact{Name: "C", CompanyID: 0}); err != nil {
			t.Errorf("CreateContact with no company: %v", err)
		}
	})
}

func TestDeleteCompanyUnlinks(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	company, _ := store.CreateCompany(models.Company{Name: "Acme"})
	other, _ := store.CreateCompany(models.Company{Name: "Globex"})

	lead, _ := store.CreateLead(models.Lead{Name: "L", CompanyID: company.ID})
	contact, _ := store.CreateContact(models.Contact{Name: "C", CompanyID: company.ID})
	deal, _ := store.CreateDeal(models.Deal{Title: "D", ContactID: contact.ID, CompanyID: company.ID, Stage: models.StageProposal})
	// A record on the other company must be left untouched.
	keep, _ := store.CreateContact(models.Contact{Name: "Keep", CompanyID: other.ID})

	unlinked, err := store.DeleteCompany(company.ID)
	if err != nil {
		t.Fatalf("DeleteCompany: %v", err)
	}
	if unlinked != 3 {
		t.Errorf("unlinked = %d, want 3 (lead+contact+deal)", unlinked)
	}
	if _, err := store.GetCompany(company.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("company still present after delete: %v", err)
	}

	gotLead, _ := store.GetLead(lead.ID)
	gotContact, _ := store.GetContact(contact.ID)
	gotDeal, _ := store.GetDeal(deal.ID)
	if gotLead.CompanyID != 0 || gotContact.CompanyID != 0 || gotDeal.CompanyID != 0 {
		t.Errorf("references not cleared: lead=%d contact=%d deal=%d",
			gotLead.CompanyID, gotContact.CompanyID, gotDeal.CompanyID)
	}
	// The kept records must survive with fields intact.
	if gotLead.Name != "L" || gotDeal.Title != "D" {
		t.Errorf("unlinked records lost data: lead=%+v deal=%+v", gotLead, gotDeal)
	}
	gotKeep, _ := store.GetContact(keep.ID)
	if gotKeep.CompanyID != other.ID {
		t.Errorf("unrelated contact's company changed: %d, want %d", gotKeep.CompanyID, other.ID)
	}

	t.Run("delete unknown company is ErrNotFound", func(t *testing.T) {
		if _, err := store.DeleteCompany(99999); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("DeleteCompany(unknown) err = %v, want ErrNotFound", err)
		}
	})
}

// TestLegacyCompanyMigration seeds a pre-Company record (a lead stored with a
// plain-string "company" field) directly into the bbolt file, then reopens the
// store so the startup migration upgrades it to a CompanyID reference.
func TestLegacyCompanyMigration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "crm.db")

	// First open creates the buckets.
	store, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = store.Close()

	// Inject a legacy lead record carrying a string "company" field.
	seedLegacyLead(t, path, 1, `{"id":1,"name":"Jane","company":"Acme","status":"new"}`)
	seedLegacyLead(t, path, 2, `{"id":2,"name":"Bob","company":"Acme","status":"new"}`)

	// Reopen → migration runs.
	migrated, err := db.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = migrated.Close() })

	companies, err := migrated.SearchCompanies("Acme")
	if err != nil {
		t.Fatalf("SearchCompanies: %v", err)
	}
	if len(companies) != 1 {
		t.Fatalf("migration created %d companies, want 1 (deduped by name)", len(companies))
	}
	acmeID := companies[0].ID

	lead, err := migrated.GetLead(1)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if lead.CompanyID != acmeID {
		t.Errorf("lead CompanyID = %d, want %d", lead.CompanyID, acmeID)
	}

	// Idempotent: reopening again must not create a second company.
	again, err := db.Open(path)
	if err != nil {
		t.Fatalf("reopen 2: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	companies2, _ := again.SearchCompanies("Acme")
	if len(companies2) != 1 {
		t.Errorf("migration not idempotent: %d companies after second reopen", len(companies2))
	}
}

// seedLegacyLead writes a raw lead JSON value under the big-endian id key in the
// leads bucket, bypassing the Store API (which no longer has a company string).
func seedLegacyLead(t *testing.T, path string, id uint64, raw string) {
	t.Helper()
	bdb, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	defer func() { _ = bdb.Close() }()
	if err := bdb.Update(func(tx *bolt.Tx) error {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)
		// Validate it is well-formed JSON before storing.
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return err
		}
		return tx.Bucket([]byte("leads")).Put(key, []byte(raw))
	}); err != nil {
		t.Fatalf("seed legacy lead: %v", err)
	}
}
