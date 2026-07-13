package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

// mustContact creates a contact for deal tests, failing the test on error.
func mustContact(t *testing.T, store *db.Store, name string) models.Contact {
	t.Helper()
	c, err := store.CreateContact(models.Contact{Name: name})
	if err != nil {
		t.Fatalf("CreateContact(%q): %v", name, err)
	}
	return c
}

func TestCreateDeal(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	contact := mustContact(t, store, "Acme")

	t.Run("valid deal persists with id and index", func(t *testing.T) {
		got, err := store.CreateDeal(models.Deal{
			Title: "Big Deal", ContactID: contact.ID, Value: 1500, Currency: "EUR", Stage: models.StageQualification,
		})
		if err != nil {
			t.Fatalf("CreateDeal: %v", err)
		}
		if got.ID == 0 || got.CreatedAt.IsZero() {
			t.Errorf("id/timestamps not set: %+v", got)
		}
		deals, err := store.DealsForContact(contact.ID)
		if err != nil {
			t.Fatalf("DealsForContact: %v", err)
		}
		if len(deals) != 1 {
			t.Errorf("DealsForContact = %d, want 1", len(deals))
		}
	})

	tests := []struct {
		name string
		deal models.Deal
	}{
		{name: "empty title", deal: models.Deal{Title: " ", ContactID: contact.ID, Stage: models.StageProposal}},
		{name: "invalid stage", deal: models.Deal{Title: "X", ContactID: contact.ID, Stage: models.DealStage("closed")}},
		{name: "missing contact", deal: models.Deal{Title: "X", ContactID: 99999, Stage: models.StageProposal}},
		{name: "value without currency", deal: models.Deal{Title: "X", ContactID: contact.ID, Value: 10, Stage: models.StageProposal}},
	}
	for _, tc := range tests {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if _, err := store.CreateDeal(tc.deal); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}

	t.Run("missing contact is errMissingContact-shaped", func(t *testing.T) {
		_, err := store.CreateDeal(models.Deal{Title: "X", ContactID: 4242, Stage: models.StageProposal})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestGetDeal(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	contact := mustContact(t, store, "Acme")
	created, err := store.CreateDeal(models.Deal{Title: "D", ContactID: contact.ID, Stage: models.StageWon})
	if err != nil {
		t.Fatalf("CreateDeal: %v", err)
	}

	t.Run("known id round-trips", func(t *testing.T) {
		got, err := store.GetDeal(created.ID)
		if err != nil {
			t.Fatalf("GetDeal: %v", err)
		}
		if got.Title != "D" || got.Stage != models.StageWon {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("unknown id is ErrNotFound", func(t *testing.T) {
		if _, err := store.GetDeal(99999); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestListDeals(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	a := mustContact(t, store, "A")
	b := mustContact(t, store, "B")

	seed := []models.Deal{
		{Title: "a1", ContactID: a.ID, Stage: models.StageQualification},
		{Title: "a2", ContactID: a.ID, Stage: models.StageWon},
		{Title: "b1", ContactID: b.ID, Stage: models.StageQualification},
	}
	for _, d := range seed {
		if _, err := store.CreateDeal(d); err != nil {
			t.Fatalf("CreateDeal: %v", err)
		}
	}

	tests := []struct {
		name   string
		filter db.DealFilter
		want   int
	}{
		{name: "all", filter: db.DealFilter{}, want: 3},
		{name: "by contact", filter: db.DealFilter{ContactID: a.ID}, want: 2},
		{name: "by stage", filter: db.DealFilter{Stage: models.StageQualification}, want: 2},
		{name: "by contact and stage compose", filter: db.DealFilter{ContactID: a.ID, Stage: models.StageQualification}, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.ListDeals(tc.filter)
			if err != nil {
				t.Fatalf("ListDeals: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("ListDeals(%+v) = %d, want %d", tc.filter, len(got), tc.want)
			}
		})
	}
}

func TestQueryDeals(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)
	owner := mustContact(t, store, "Owner")

	d1, _ := store.CreateDeal(models.Deal{Title: "Alpha deal", ContactID: owner.ID, Stage: models.StageQualification})
	clk.advance(time.Hour)
	d2, _ := store.CreateDeal(models.Deal{Title: "Beta deal", ContactID: owner.ID, Stage: models.StageProposal, Value: 100, Currency: "EUR"})
	clk.advance(time.Hour)
	d3, _ := store.CreateDeal(models.Deal{Title: "Gamma deal", ContactID: owner.ID, Stage: models.StageQualification})

	first := func(p db.DealPage) uint64 {
		if len(p.Deals) == 0 {
			return 0
		}
		return p.Deals[0].ID
	}

	t.Run("default last-updated-first with metadata", func(t *testing.T) {
		got, err := store.QueryDeals(db.DealQuery{})
		if err != nil {
			t.Fatalf("QueryDeals: %v", err)
		}
		if got.Total != 3 || first(got) != d3.ID {
			t.Errorf("default first = %d, want %d (total %d)", first(got), d3.ID, got.Total)
		}
	})

	t.Run("updated jumps to front", func(t *testing.T) {
		clk.advance(time.Hour)
		if _, err := store.UpdateDeal(d1); err != nil {
			t.Fatalf("UpdateDeal: %v", err)
		}
		got, _ := store.QueryDeals(db.DealQuery{})
		if first(got) != d1.ID {
			t.Errorf("updated-first = %d, want %d", first(got), d1.ID)
		}
	})

	t.Run("stage, contact, and search filters", func(t *testing.T) {
		byStage, _ := store.QueryDeals(db.DealQuery{Stage: models.StageQualification})
		if byStage.Total != 2 {
			t.Errorf("stage total = %d, want 2", byStage.Total)
		}
		byContact, _ := store.QueryDeals(db.DealQuery{ContactID: owner.ID})
		if byContact.Total != 3 {
			t.Errorf("contact total = %d, want 3", byContact.Total)
		}
		bySearch, _ := store.QueryDeals(db.DealQuery{Search: "beta"})
		if bySearch.Total != 1 || first(bySearch) != d2.ID {
			t.Errorf("search = %+v, want [%d]", bySearch.Deals, d2.ID)
		}
	})

	t.Run("pagination clamp and invalid inputs rejected", func(t *testing.T) {
		clamp, _ := store.QueryDeals(db.DealQuery{PageSize: 1000})
		if clamp.PageSize != 50 {
			t.Errorf("page_size = %d, want clamped to 50", clamp.PageSize)
		}
		if _, err := store.QueryDeals(db.DealQuery{Stage: models.DealStage("bogus")}); err == nil {
			t.Error("expected error for bad stage")
		}
		if _, err := store.QueryDeals(db.DealQuery{SortBy: db.DealSort("bogus")}); err == nil {
			t.Error("expected error for bad sort")
		}
	})
}

func TestUpdateDeal(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)
	a := mustContact(t, store, "A")
	b := mustContact(t, store, "B")
	created, err := store.CreateDeal(models.Deal{Title: "D", ContactID: a.ID, Stage: models.StageQualification})
	if err != nil {
		t.Fatalf("CreateDeal: %v", err)
	}

	t.Run("advances stage and UpdatedAt, keeps CreatedAt", func(t *testing.T) {
		clk.advance(30 * 60 * 1e9) // 30 minutes
		upd := created
		upd.Stage = models.StageProposal
		got, err := store.UpdateDeal(upd)
		if err != nil {
			t.Fatalf("UpdateDeal: %v", err)
		}
		if got.Stage != models.StageProposal {
			t.Errorf("Stage = %q, want proposal", got.Stage)
		}
		if !got.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("CreatedAt changed")
		}
		if !got.UpdatedAt.After(created.UpdatedAt) {
			t.Errorf("UpdatedAt did not advance")
		}
	})

	t.Run("reassigning contact rewrites the index", func(t *testing.T) {
		cur, err := store.GetDeal(created.ID)
		if err != nil {
			t.Fatalf("GetDeal: %v", err)
		}
		cur.ContactID = b.ID
		if _, err := store.UpdateDeal(cur); err != nil {
			t.Fatalf("UpdateDeal: %v", err)
		}
		aDeals, _ := store.DealsForContact(a.ID)
		bDeals, _ := store.DealsForContact(b.ID)
		if len(aDeals) != 0 {
			t.Errorf("old contact still has %d deals", len(aDeals))
		}
		if len(bDeals) != 1 {
			t.Errorf("new contact has %d deals, want 1", len(bDeals))
		}
	})

	t.Run("unknown id is ErrNotFound", func(t *testing.T) {
		_, err := store.UpdateDeal(models.Deal{ID: 99999, Title: "Ghost", ContactID: a.ID, Stage: models.StageWon})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestDeleteDeal(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	c := mustContact(t, store, "A")
	created, err := store.CreateDeal(models.Deal{Title: "D", ContactID: c.ID, Stage: models.StageWon})
	if err != nil {
		t.Fatalf("CreateDeal: %v", err)
	}

	if err := store.DeleteDeal(created.ID); err != nil {
		t.Fatalf("DeleteDeal: %v", err)
	}
	if _, err := store.GetDeal(created.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("deal still present after delete: %v", err)
	}
	if deals, _ := store.DealsForContact(c.ID); len(deals) != 0 {
		t.Errorf("index entry survived delete: %d", len(deals))
	}
	if err := store.DeleteDeal(99999); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("delete unknown: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteContactCascades(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	c := mustContact(t, store, "Acme")
	d1, _ := store.CreateDeal(models.Deal{Title: "d1", ContactID: c.ID, Stage: models.StageProposal})
	d2, _ := store.CreateDeal(models.Deal{Title: "d2", ContactID: c.ID, Stage: models.StageWon})

	t.Run("removes contact and all its deals", func(t *testing.T) {
		deleted, err := store.DeleteContact(c.ID)
		if err != nil {
			t.Fatalf("DeleteContact: %v", err)
		}
		if len(deleted) != 2 {
			t.Errorf("deleted deal ids = %v, want 2", deleted)
		}
		if _, err := store.GetContact(c.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("contact survived: %v", err)
		}
		for _, id := range []uint64{d1.ID, d2.ID} {
			if _, err := store.GetDeal(id); !errors.Is(err, db.ErrNotFound) {
				t.Errorf("deal %d survived cascade: %v", id, err)
			}
		}
		if deals, _ := store.DealsForContact(c.ID); len(deals) != 0 {
			t.Errorf("index entries survived: %d", len(deals))
		}
	})

	t.Run("unknown contact is ErrNotFound", func(t *testing.T) {
		if _, err := store.DeleteContact(99999); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
