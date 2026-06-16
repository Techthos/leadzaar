package db_test

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// clock is a deterministic, advanceable time source for tests (no time.Sleep).
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)}
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// openTestStore opens a fresh Store backed by a temp file, with an injectable
// clock. The store is closed automatically when the test ends.
func openTestStore(t *testing.T, clk *clock) *db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crm.db")
	store, err := db.Open(path, db.WithClock(clk.now))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return store
}

func TestCreateContact(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)

	t.Run("assigns id and timestamps", func(t *testing.T) {
		got, err := store.CreateContact(models.Contact{Name: "Ada Lovelace", Email: "ada@example.com"})
		if err != nil {
			t.Fatalf("CreateContact: %v", err)
		}
		if got.ID == 0 {
			t.Errorf("ID = 0, want a fresh sequence id")
		}
		if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
			t.Errorf("timestamps not set: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
		}
	})

	t.Run("ids are monotonic", func(t *testing.T) {
		a, err := store.CreateContact(models.Contact{Name: "First"})
		if err != nil {
			t.Fatalf("CreateContact: %v", err)
		}
		b, err := store.CreateContact(models.Contact{Name: "Second"})
		if err != nil {
			t.Fatalf("CreateContact: %v", err)
		}
		if b.ID <= a.ID {
			t.Errorf("ids not monotonic: a=%d b=%d", a.ID, b.ID)
		}
	})

	t.Run("empty name rejected", func(t *testing.T) {
		if _, err := store.CreateContact(models.Contact{Name: "   "}); err == nil {
			t.Fatal("expected error for empty name, got nil")
		}
	})
}

func TestGetContact(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())

	created, err := store.CreateContact(models.Contact{Name: "Grace Hopper"})
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}

	t.Run("known id round-trips", func(t *testing.T) {
		got, err := store.GetContact(created.ID)
		if err != nil {
			t.Fatalf("GetContact: %v", err)
		}
		if got.Name != "Grace Hopper" {
			t.Errorf("Name = %q, want %q", got.Name, "Grace Hopper")
		}
	})

	t.Run("unknown id is ErrNotFound", func(t *testing.T) {
		_, err := store.GetContact(99999)
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestFindContactsByEmail(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())

	if _, err := store.CreateContact(models.Contact{Name: "A", Email: "Shared@Example.com"}); err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if _, err := store.CreateContact(models.Contact{Name: "B", Email: "shared@example.com"}); err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if _, err := store.CreateContact(models.Contact{Name: "C", Email: "other@example.com"}); err != nil {
		t.Fatalf("CreateContact: %v", err)
	}

	t.Run("case-insensitive index match returns duplicates", func(t *testing.T) {
		got, err := store.FindContactsByEmail("shared@example.com")
		if err != nil {
			t.Fatalf("FindContactsByEmail: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d contacts, want 2", len(got))
		}
	})

	t.Run("empty email returns nothing", func(t *testing.T) {
		got, err := store.FindContactsByEmail("")
		if err != nil {
			t.Fatalf("FindContactsByEmail: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %d, want 0", len(got))
		}
	})
}

func TestSearchContacts(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())

	bletchley, _ := store.CreateCompany(models.Company{Name: "Bletchley"})
	acme, _ := store.CreateCompany(models.Company{Name: "Acme"})
	if _, err := store.CreateContact(models.Contact{Name: "Alan Turing", CompanyID: bletchley.ID, Tags: []string{"vip"}}); err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if _, err := store.CreateContact(models.Contact{Name: "Bob", CompanyID: acme.ID}); err != nil {
		t.Fatalf("CreateContact: %v", err)
	}

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "name substring", query: "turing", want: 1},
		{name: "company substring", query: "acme", want: 1},
		{name: "tag match", query: "vip", want: 1},
		{name: "no match", query: "zzz", want: 0},
		{name: "blank returns all", query: "", want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.SearchContacts(tc.query)
			if err != nil {
				t.Fatalf("SearchContacts: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("SearchContacts(%q) = %d results, want %d", tc.query, len(got), tc.want)
			}
		})
	}
}

func TestUpdateContact(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)

	created, err := store.CreateContact(models.Contact{Name: "Old Name", Email: "old@example.com"})
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}

	t.Run("persists changes, advances UpdatedAt, keeps CreatedAt", func(t *testing.T) {
		clk.advance(time.Hour)
		updated := created
		updated.Name = "New Name"
		got, err := store.UpdateContact(updated)
		if err != nil {
			t.Fatalf("UpdateContact: %v", err)
		}
		if got.Name != "New Name" {
			t.Errorf("Name = %q, want %q", got.Name, "New Name")
		}
		if !got.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("CreatedAt changed: %v -> %v", created.CreatedAt, got.CreatedAt)
		}
		if !got.UpdatedAt.After(created.UpdatedAt) {
			t.Errorf("UpdatedAt did not advance: %v -> %v", created.UpdatedAt, got.UpdatedAt)
		}
	})

	t.Run("email change rewrites the index", func(t *testing.T) {
		c, err := store.GetContact(created.ID)
		if err != nil {
			t.Fatalf("GetContact: %v", err)
		}
		c.Email = "new@example.com"
		if _, err := store.UpdateContact(c); err != nil {
			t.Fatalf("UpdateContact: %v", err)
		}
		oldHits, err := store.FindContactsByEmail("old@example.com")
		if err != nil {
			t.Fatalf("FindContactsByEmail: %v", err)
		}
		if len(oldHits) != 0 {
			t.Errorf("old email still indexed: %d hits", len(oldHits))
		}
		newHits, err := store.FindContactsByEmail("new@example.com")
		if err != nil {
			t.Fatalf("FindContactsByEmail: %v", err)
		}
		if len(newHits) != 1 {
			t.Errorf("new email hits = %d, want 1", len(newHits))
		}
	})

	t.Run("unknown id is ErrNotFound", func(t *testing.T) {
		_, err := store.UpdateContact(models.Contact{ID: 99999, Name: "Ghost"})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
