package db_test

import (
	"errors"
	"testing"

	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// mustLead creates a lead for offer tests, failing the test on error.
func mustLead(t *testing.T, store *db.Store, name string) models.Lead {
	t.Helper()
	l, err := store.CreateLead(models.Lead{Name: name})
	if err != nil {
		t.Fatalf("CreateLead(%q): %v", name, err)
	}
	return l
}

func TestCreateOffer(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	lead := mustLead(t, store, "Prospect")

	t.Run("valid offer persists with id and index", func(t *testing.T) {
		got, err := store.CreateOffer(models.Offer{
			LeadID: lead.ID, Title: "Q3 Proposal", Subject: "Our proposal", Body: "Dear customer,\n…",
		})
		if err != nil {
			t.Fatalf("CreateOffer: %v", err)
		}
		if got.ID == 0 || got.CreatedAt.IsZero() {
			t.Errorf("id/timestamps not set: %+v", got)
		}
		offers, err := store.OffersForLead(lead.ID)
		if err != nil {
			t.Fatalf("OffersForLead: %v", err)
		}
		if len(offers) != 1 {
			t.Errorf("OffersForLead = %d, want 1", len(offers))
		}
	})

	tests := []struct {
		name  string
		offer models.Offer
	}{
		{name: "empty title", offer: models.Offer{LeadID: lead.ID, Title: " "}},
		{name: "missing lead", offer: models.Offer{LeadID: 99999, Title: "X"}},
	}
	for _, tc := range tests {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if _, err := store.CreateOffer(tc.offer); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestGetOffer(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	lead := mustLead(t, store, "Prospect")
	created, err := store.CreateOffer(models.Offer{LeadID: lead.ID, Title: "O"})
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}

	t.Run("known id round-trips", func(t *testing.T) {
		got, err := store.GetOffer(created.ID)
		if err != nil {
			t.Fatalf("GetOffer: %v", err)
		}
		if got.Title != "O" || got.LeadID != lead.ID {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("unknown id is ErrNotFound", func(t *testing.T) {
		if _, err := store.GetOffer(99999); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestListOffers(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	a := mustLead(t, store, "A")
	b := mustLead(t, store, "B")

	seed := []models.Offer{
		{LeadID: a.ID, Title: "a1"},
		{LeadID: a.ID, Title: "a2"},
		{LeadID: b.ID, Title: "b1"},
	}
	for _, o := range seed {
		if _, err := store.CreateOffer(o); err != nil {
			t.Fatalf("CreateOffer: %v", err)
		}
	}

	tests := []struct {
		name   string
		filter db.OfferFilter
		want   int
	}{
		{name: "all", filter: db.OfferFilter{}, want: 3},
		{name: "by lead", filter: db.OfferFilter{LeadID: a.ID}, want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.ListOffers(tc.filter)
			if err != nil {
				t.Fatalf("ListOffers: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("ListOffers(%+v) = %d, want %d", tc.filter, len(got), tc.want)
			}
		})
	}
}

func TestUpdateOffer(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)
	a := mustLead(t, store, "A")
	b := mustLead(t, store, "B")
	created, err := store.CreateOffer(models.Offer{LeadID: a.ID, Title: "O", Body: "v1"})
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}

	t.Run("edits body and UpdatedAt, keeps CreatedAt", func(t *testing.T) {
		clk.advance(30 * 60 * 1e9) // 30 minutes
		upd := created
		upd.Body = "v2"
		got, err := store.UpdateOffer(upd)
		if err != nil {
			t.Fatalf("UpdateOffer: %v", err)
		}
		if got.Body != "v2" {
			t.Errorf("Body = %q, want v2", got.Body)
		}
		if !got.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("CreatedAt changed")
		}
		if !got.UpdatedAt.After(created.UpdatedAt) {
			t.Errorf("UpdatedAt did not advance")
		}
	})

	t.Run("reassigning lead rewrites the index", func(t *testing.T) {
		cur, err := store.GetOffer(created.ID)
		if err != nil {
			t.Fatalf("GetOffer: %v", err)
		}
		cur.LeadID = b.ID
		if _, err := store.UpdateOffer(cur); err != nil {
			t.Fatalf("UpdateOffer: %v", err)
		}
		aOffers, _ := store.OffersForLead(a.ID)
		bOffers, _ := store.OffersForLead(b.ID)
		if len(aOffers) != 0 {
			t.Errorf("old lead still has %d offers", len(aOffers))
		}
		if len(bOffers) != 1 {
			t.Errorf("new lead has %d offers, want 1", len(bOffers))
		}
	})

	t.Run("unknown id is ErrNotFound", func(t *testing.T) {
		_, err := store.UpdateOffer(models.Offer{ID: 99999, Title: "Ghost", LeadID: a.ID})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestDeleteOffer(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	lead := mustLead(t, store, "A")
	created, err := store.CreateOffer(models.Offer{LeadID: lead.ID, Title: "O"})
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}

	if err := store.DeleteOffer(created.ID); err != nil {
		t.Fatalf("DeleteOffer: %v", err)
	}
	if _, err := store.GetOffer(created.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("offer still present after delete: %v", err)
	}
	if offers, _ := store.OffersForLead(lead.ID); len(offers) != 0 {
		t.Errorf("index entry survived delete: %d", len(offers))
	}
	if err := store.DeleteOffer(99999); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("delete unknown: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteLeadCascadesOffers(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	lead := mustLead(t, store, "Prospect")
	o1, _ := store.CreateOffer(models.Offer{LeadID: lead.ID, Title: "o1"})
	o2, _ := store.CreateOffer(models.Offer{LeadID: lead.ID, Title: "o2"})

	t.Run("removes lead and all its offers", func(t *testing.T) {
		deleted, err := store.DeleteLead(lead.ID)
		if err != nil {
			t.Fatalf("DeleteLead: %v", err)
		}
		if len(deleted) != 2 {
			t.Errorf("deleted offer ids = %v, want 2", deleted)
		}
		if _, err := store.GetLead(lead.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("lead survived: %v", err)
		}
		for _, id := range []uint64{o1.ID, o2.ID} {
			if _, err := store.GetOffer(id); !errors.Is(err, db.ErrNotFound) {
				t.Errorf("offer %d survived cascade: %v", id, err)
			}
		}
		if offers, _ := store.OffersForLead(lead.ID); len(offers) != 0 {
			t.Errorf("index entries survived: %d", len(offers))
		}
	})

	t.Run("unknown lead is ErrNotFound", func(t *testing.T) {
		if _, err := store.DeleteLead(99999); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
