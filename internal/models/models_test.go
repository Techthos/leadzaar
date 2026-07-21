package models_test

import (
	"testing"
	"time"

	"github.com/techthos/leadzaar/internal/models"
)

func TestParseDate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		want    time.Time
		wantErr bool
	}{
		{"calendar date", "2026-08-15", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), false},
		{"blank clears", "", time.Time{}, false},
		{"whitespace clears", "   ", time.Time{}, false},
		{"surrounding space tolerated", " 2026-08-15 ", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), false},
		{"wrong order rejected", "15-08-2026", time.Time{}, true},
		{"timestamp rejected", "2026-08-15T09:00:00Z", time.Time{}, true},
		{"prose rejected", "next tuesday", time.Time{}, true},
		{"impossible date rejected", "2026-02-30", time.Time{}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := models.ParseDate(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseDate(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if !got.Equal(tc.want) {
				t.Errorf("ParseDate(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatDateRoundTrips(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"2026-08-15", "", "1999-01-01"} {
		parsed, err := models.ParseDate(in)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", in, err)
		}
		if got := models.FormatDate(parsed); got != in {
			t.Errorf("round trip of %q = %q", in, got)
		}
	}
}

func TestTruncateDate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{
			"drops time of day",
			time.Date(2026, 8, 15, 17, 45, 3, 500, time.UTC),
			time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			"already midnight is unchanged",
			time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			// The calendar date is read in the value's own zone, so a late-evening
			// local time keeps its own date rather than rolling over in UTC.
			"keeps the local calendar date",
			time.Date(2026, 8, 15, 23, 30, 0, 0, time.FixedZone("CEST", 2*60*60)),
			time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		},
		{"zero passes through", time.Time{}, time.Time{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := models.TruncateDate(tc.in)
			if !got.Equal(tc.want) {
				t.Errorf("TruncateDate(%v) = %v, want %v", tc.in, got, tc.want)
			}
			if !got.IsZero() && got.Location() != time.UTC {
				t.Errorf("TruncateDate(%v) location = %v, want UTC", tc.in, got.Location())
			}
		})
	}
}

func TestLeadAvailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		until time.Time
		want  bool
	}{
		{"no block recorded", time.Time{}, true},
		{"block still running", time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), false},
		{"block elapsed", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), true},
		// "until" is exclusive: a lead away until the 13th is reachable on the
		// 13th, which is what an autoresponder's "back on the 13th" means.
		{"block ends at the start of today", time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC), true},
		{"block starts tomorrow", time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := models.Lead{Name: "L", UnavailableUntil: tc.until}
			if got := l.Available(now); got != tc.want {
				t.Errorf("Available() = %v, want %v", got, tc.want)
			}
		})
	}
}
