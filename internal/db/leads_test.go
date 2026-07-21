package db_test

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
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

func equalIDs(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestQueryLeads(t *testing.T) {
	t.Parallel()
	clk := newClock()
	store := openTestStore(t, clk)

	// Three leads created oldest→newest, with distinct quality scores.
	low, _ := store.CreateLead(models.Lead{Name: "Acme Web", Status: models.StatusNew, Quality: 3, Email: "info@acme.io"})
	clk.advance(time.Hour)
	high, _ := store.CreateLead(models.Lead{Name: "Beta Corp", Status: models.StatusContacted, Quality: 9, Tags: []string{"vip"}})
	clk.advance(time.Hour)
	mid, _ := store.CreateLead(models.Lead{Name: "Acme Labs", Status: models.StatusNew, Quality: 6})

	ids := func(p db.LeadPage) []uint64 {
		out := make([]uint64, len(p.Leads))
		for i, l := range p.Leads {
			out[i] = l.ID
		}
		return out
	}

	t.Run("default is last-updated-first with full-page metadata", func(t *testing.T) {
		// No record has been updated yet, so UpdatedAt == CreatedAt and the
		// default (last-updated-first) coincides with newest-created-first.
		got, err := store.QueryLeads(db.LeadQuery{})
		if err != nil {
			t.Fatalf("QueryLeads: %v", err)
		}
		if want := []uint64{mid.ID, high.ID, low.ID}; !equalIDs(ids(got), want) {
			t.Errorf("order = %v, want %v", ids(got), want)
		}
		if got.Total != 3 || got.TotalPages != 1 || got.Page != 1 || got.PageSize != 50 || got.HasMore {
			t.Errorf("metadata = %+v, want total 3 / 1 page / page 1 / size 50 / no more", got)
		}
	})

	t.Run("status filter", func(t *testing.T) {
		got, err := store.QueryLeads(db.LeadQuery{Status: models.StatusNew})
		if err != nil {
			t.Fatalf("QueryLeads: %v", err)
		}
		if got.Total != 2 {
			t.Errorf("total = %d, want 2", got.Total)
		}
	})

	t.Run("substring search over name/email/tag", func(t *testing.T) {
		byName, _ := store.QueryLeads(db.LeadQuery{Search: "acme"})
		if byName.Total != 2 {
			t.Errorf("name search total = %d, want 2", byName.Total)
		}
		byTag, _ := store.QueryLeads(db.LeadQuery{Search: "VIP"})
		if byTag.Total != 1 || (len(byTag.Leads) == 1 && byTag.Leads[0].ID != high.ID) {
			t.Errorf("tag search = %v, want [%d]", ids(byTag), high.ID)
		}
		byEmail, _ := store.QueryLeads(db.LeadQuery{Search: "@acme.io"})
		if byEmail.Total != 1 || (len(byEmail.Leads) == 1 && byEmail.Leads[0].ID != low.ID) {
			t.Errorf("email search = %v, want [%d]", ids(byEmail), low.ID)
		}
	})

	t.Run("sort by quality", func(t *testing.T) {
		desc, _ := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSortQuality})
		if want := []uint64{high.ID, mid.ID, low.ID}; !equalIDs(ids(desc), want) {
			t.Errorf("quality desc = %v, want %v", ids(desc), want)
		}
		asc, _ := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSortQuality, Asc: true})
		if want := []uint64{low.ID, mid.ID, high.ID}; !equalIDs(ids(asc), want) {
			t.Errorf("quality asc = %v, want %v", ids(asc), want)
		}
	})

	t.Run("sort by updated", func(t *testing.T) {
		// Touch the oldest lead so it becomes the most-recently-updated.
		clk.advance(time.Hour)
		if _, err := store.UpdateLead(low); err != nil {
			t.Fatalf("UpdateLead: %v", err)
		}
		got, _ := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSortUpdated})
		if len(got.Leads) == 0 || got.Leads[0].ID != low.ID {
			t.Errorf("updated desc first = %v, want %d", ids(got), low.ID)
		}
	})

	t.Run("pagination walks the set and reports has_more", func(t *testing.T) {
		p1, _ := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSortQuality, Page: 1, PageSize: 2})
		if want := []uint64{high.ID, mid.ID}; !equalIDs(ids(p1), want) {
			t.Errorf("page 1 = %v, want %v", ids(p1), want)
		}
		if !p1.HasMore || p1.Total != 3 || p1.TotalPages != 2 || p1.PageSize != 2 {
			t.Errorf("page 1 metadata = %+v", p1)
		}
		p2, _ := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSortQuality, Page: 2, PageSize: 2})
		if want := []uint64{low.ID}; !equalIDs(ids(p2), want) {
			t.Errorf("page 2 = %v, want %v", ids(p2), want)
		}
		if p2.HasMore {
			t.Errorf("page 2 should be the last page")
		}
		p3, _ := store.QueryLeads(db.LeadQuery{Page: 3, PageSize: 2})
		if len(p3.Leads) != 0 || p3.HasMore {
			t.Errorf("page past end = %+v, want empty", p3)
		}
	})

	t.Run("page_size clamped to 50", func(t *testing.T) {
		got, _ := store.QueryLeads(db.LeadQuery{PageSize: 1000})
		if got.PageSize != 50 {
			t.Errorf("page_size = %d, want clamped to 50", got.PageSize)
		}
	})

	t.Run("invalid status and sort rejected", func(t *testing.T) {
		if _, err := store.QueryLeads(db.LeadQuery{Status: models.LeadStatus("bogus")}); err == nil {
			t.Error("expected error for bad status")
		}
		if _, err := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSort("bogus")}); err == nil {
			t.Error("expected error for bad sort")
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

func TestLeadUnavailability(t *testing.T) {
	t.Parallel()
	// The injected clock reads 2026-06-13 09:00 UTC throughout this test, so
	// every availability assertion below is deterministic.
	clk := newClock()
	store := openTestStore(t, clk)
	now := clk.now()

	date := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	// away is blocked well into the future; backToday's block expires at the
	// start of today (exclusive, so it is reachable again); expired's block is in
	// the past; open never had one.
	away, _ := store.CreateLead(models.Lead{Name: "Away", UnavailableUntil: date(2026, 6, 30)})
	soon, _ := store.CreateLead(models.Lead{Name: "Soon", UnavailableUntil: date(2026, 6, 20)})
	backToday, _ := store.CreateLead(models.Lead{Name: "Back today", UnavailableUntil: date(2026, 6, 13)})
	expired, _ := store.CreateLead(models.Lead{Name: "Expired", UnavailableUntil: date(2026, 6, 1)})
	open, _ := store.CreateLead(models.Lead{Name: "Open"})

	ids := func(p db.LeadPage) []uint64 {
		out := make([]uint64, len(p.Leads))
		for i, l := range p.Leads {
			out[i] = l.ID
		}
		return out
	}

	t.Run("stored date is normalized to midnight UTC", func(t *testing.T) {
		// A caller may hand in any instant; the store keeps one canonical
		// instant per calendar date so equality and sorting stay well-defined.
		got, err := store.CreateLead(models.Lead{
			Name:             "Sloppy",
			UnavailableUntil: time.Date(2026, 7, 4, 17, 45, 3, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("CreateLead: %v", err)
		}
		if want := date(2026, 7, 4); !got.UnavailableUntil.Equal(want) {
			t.Errorf("UnavailableUntil = %v, want %v", got.UnavailableUntil, want)
		}
		if _, err := store.DeleteLead(got.ID); err != nil {
			t.Fatalf("DeleteLead: %v", err)
		}
	})

	t.Run("Available is exclusive on the until date", func(t *testing.T) {
		cases := []struct {
			name string
			lead models.Lead
			want bool
		}{
			{"future block", away, false},
			{"block ends today", backToday, true},
			{"block elapsed", expired, true},
			{"no block", open, true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := tc.lead.Available(now); got != tc.want {
					t.Errorf("Available(%v) = %v, want %v", now, got, tc.want)
				}
			})
		}
	})

	t.Run("availability filter", func(t *testing.T) {
		gotAway, err := store.QueryLeads(db.LeadQuery{Availability: db.LeadUnavailable})
		if err != nil {
			t.Fatalf("QueryLeads: %v", err)
		}
		if want := []uint64{soon.ID, away.ID}; !equalIDs(sortedIDs(ids(gotAway)), sortedIDs(want)) {
			t.Errorf("unavailable = %v, want %v", ids(gotAway), want)
		}

		gotOpen, err := store.QueryLeads(db.LeadQuery{Availability: db.LeadAvailable})
		if err != nil {
			t.Fatalf("QueryLeads: %v", err)
		}
		want := []uint64{backToday.ID, expired.ID, open.ID}
		if !equalIDs(sortedIDs(ids(gotOpen)), sortedIDs(want)) {
			t.Errorf("available = %v, want %v", ids(gotOpen), want)
		}
	})

	t.Run("invalid availability rejected", func(t *testing.T) {
		if _, err := store.QueryLeads(db.LeadQuery{Availability: db.LeadAvailability("maybe")}); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("ascending sort leads with soonest return, unblocked last", func(t *testing.T) {
		got, err := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSortUnavailableUntil, Asc: true})
		if err != nil {
			t.Fatalf("QueryLeads: %v", err)
		}
		// Dated blocks ascend by date; the lead with no block sorts last because
		// an unset date means "nothing to wait for", not "the zero instant".
		want := []uint64{expired.ID, backToday.ID, soon.ID, away.ID, open.ID}
		if !equalIDs(ids(got), want) {
			t.Errorf("order = %v, want %v", ids(got), want)
		}
	})

	t.Run("descending sort reverses, unblocked first", func(t *testing.T) {
		got, err := store.QueryLeads(db.LeadQuery{SortBy: db.LeadSortUnavailableUntil})
		if err != nil {
			t.Fatalf("QueryLeads: %v", err)
		}
		want := []uint64{open.ID, away.ID, soon.ID, backToday.ID, expired.ID}
		if !equalIDs(ids(got), want) {
			t.Errorf("order = %v, want %v", ids(got), want)
		}
	})

	t.Run("update clears a block", func(t *testing.T) {
		cleared := away
		cleared.UnavailableUntil = time.Time{}
		got, err := store.UpdateLead(cleared)
		if err != nil {
			t.Fatalf("UpdateLead: %v", err)
		}
		if !got.UnavailableUntil.IsZero() {
			t.Errorf("UnavailableUntil = %v, want zero", got.UnavailableUntil)
		}
		if !got.Available(now) {
			t.Error("lead still unavailable after clearing the block")
		}
		// Restore so sibling subtests keep their fixture.
		if _, err := store.UpdateLead(away); err != nil {
			t.Fatalf("UpdateLead restore: %v", err)
		}
	})
}

// sortedIDs returns ids in ascending order so set-equality assertions do not
// depend on the query's ordering.
func sortedIDs(ids []uint64) []uint64 {
	out := append([]uint64(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
