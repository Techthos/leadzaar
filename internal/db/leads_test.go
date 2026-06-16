package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

func TestCreateLead(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())

	t.Run("defaults status to new and sets timestamps", func(t *testing.T) {
		got, err := store.CreateLead(models.Lead{Name: "Prospect", Source: models.SourceWeb})
		if err != nil {
			t.Fatalf("CreateLead: %v", err)
		}
		if got.ID == 0 || got.CreatedAt.IsZero() {
			t.Errorf("id/timestamps not set: %+v", got)
		}
		if got.Status != models.StatusNew {
			t.Errorf("Status = %q, want new", got.Status)
		}
	})

	t.Run("empty name rejected", func(t *testing.T) {
		if _, err := store.CreateLead(models.Lead{Name: " "}); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid source rejected", func(t *testing.T) {
		if _, err := store.CreateLead(models.Lead{Name: "X", Source: models.Source("linkedin")}); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("quality score validated", func(t *testing.T) {
		// 0 (unscored) and 1–10 are accepted; out-of-range is rejected.
		for _, q := range []int{0, 1, 10} {
			if _, err := store.CreateLead(models.Lead{Name: "Q", Quality: q}); err != nil {
				t.Errorf("CreateLead quality=%d: unexpected error %v", q, err)
			}
		}
		for _, q := range []int{-1, 11, 100} {
			if _, err := store.CreateLead(models.Lead{Name: "Q", Quality: q}); err == nil {
				t.Errorf("CreateLead quality=%d: expected error, got nil", q)
			}
		}
	})
}

func TestListLeads(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())

	first, _ := store.CreateLead(models.Lead{Name: "First", Status: models.StatusNew})
	_, _ = store.CreateLead(models.Lead{Name: "Second", Status: models.StatusContacted})
	last, _ := store.CreateLead(models.Lead{Name: "Third", Status: models.StatusNew})

	t.Run("all leads newest-first", func(t *testing.T) {
		got, err := store.ListLeads("")
		if err != nil {
			t.Fatalf("ListLeads: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d leads, want 3", len(got))
		}
		if got[0].ID != last.ID {
			t.Errorf("first result ID = %d, want newest %d", got[0].ID, last.ID)
		}
		if got[len(got)-1].ID != first.ID {
			t.Errorf("last result ID = %d, want oldest %d", got[len(got)-1].ID, first.ID)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		got, err := store.ListLeads(models.StatusNew)
		if err != nil {
			t.Fatalf("ListLeads: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("got %d new leads, want 2", len(got))
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		if _, err := store.ListLeads(models.LeadStatus("bogus")); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestGetLead(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	created, _ := store.CreateLead(models.Lead{Name: "Lead"})

	if _, err := store.GetLead(created.ID); err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if _, err := store.GetLead(99999); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateLead(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)
	created, _ := store.CreateLead(models.Lead{Name: "Old", Status: models.StatusNew})

	t.Run("advances status and UpdatedAt, keeps CreatedAt", func(t *testing.T) {
		clk.advance(time.Hour)
		upd := created
		upd.Status = models.StatusQualified
		upd.Name = "New"
		got, err := store.UpdateLead(upd)
		if err != nil {
			t.Fatalf("UpdateLead: %v", err)
		}
		if got.Status != models.StatusQualified || got.Name != "New" {
			t.Errorf("got %+v", got)
		}
		if !got.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("CreatedAt changed")
		}
		if !got.UpdatedAt.After(created.UpdatedAt) {
			t.Errorf("UpdatedAt did not advance")
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		upd := created
		upd.Status = models.LeadStatus("bogus")
		if _, err := store.UpdateLead(upd); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("unknown id is ErrNotFound", func(t *testing.T) {
		_, err := store.UpdateLead(models.Lead{ID: 99999, Name: "Ghost", Status: models.StatusNew})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestDeleteLead(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	created, _ := store.CreateLead(models.Lead{Name: "Lead"})

	if _, err := store.DeleteLead(created.ID); err != nil {
		t.Fatalf("DeleteLead: %v", err)
	}
	if _, err := store.GetLead(created.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("lead survived delete: %v", err)
	}
	if _, err := store.DeleteLead(99999); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("delete unknown: err = %v, want ErrNotFound", err)
	}
}
