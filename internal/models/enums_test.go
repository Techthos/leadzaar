package models_test

import (
	"testing"

	"github.com/techthos/leadzaar/internal/models"
)

func TestSourceValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   models.Source
		want bool
	}{
		{name: "web", in: models.SourceWeb, want: true},
		{name: "referral", in: models.SourceReferral, want: true},
		{name: "event", in: models.SourceEvent, want: true},
		{name: "cold-outreach", in: models.SourceColdOutreach, want: true},
		{name: "other", in: models.SourceOther, want: true},
		{name: "empty is invalid", in: models.Source(""), want: false},
		{name: "unknown is invalid", in: models.Source("linkedin"), want: false},
		{name: "wrong case is invalid", in: models.Source("Web"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.Valid(); got != tc.want {
				t.Errorf("Source(%q).Valid() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLeadStatusValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   models.LeadStatus
		want bool
	}{
		{name: "new", in: models.StatusNew, want: true},
		{name: "contacted", in: models.StatusContacted, want: true},
		{name: "qualified", in: models.StatusQualified, want: true},
		{name: "converted", in: models.StatusConverted, want: true},
		{name: "lost", in: models.StatusLost, want: true},
		{name: "empty is invalid", in: models.LeadStatus(""), want: false},
		{name: "unknown is invalid", in: models.LeadStatus("won"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.Valid(); got != tc.want {
				t.Errorf("LeadStatus(%q).Valid() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestDealStageValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   models.DealStage
		want bool
	}{
		{name: "qualification", in: models.StageQualification, want: true},
		{name: "proposal", in: models.StageProposal, want: true},
		{name: "negotiation", in: models.StageNegotiation, want: true},
		{name: "won", in: models.StageWon, want: true},
		{name: "lost", in: models.StageLost, want: true},
		{name: "empty is invalid", in: models.DealStage(""), want: false},
		{name: "unknown is invalid", in: models.DealStage("closed"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.Valid(); got != tc.want {
				t.Errorf("DealStage(%q).Valid() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestEnumeratorsCoverAllValidValues guards against an enumerator and its Valid()
// switch drifting apart — every listed value must be Valid, and the count is the
// spec's count.
func TestEnumeratorsCoverAllValidValues(t *testing.T) {
	t.Parallel()
	if got := len(models.Sources()); got != 5 {
		t.Errorf("len(Sources()) = %d, want 5", got)
	}
	for _, s := range models.Sources() {
		if !s.Valid() {
			t.Errorf("Sources() returned %q which is not Valid()", s)
		}
	}
	if got := len(models.LeadStatuses()); got != 9 {
		t.Errorf("len(LeadStatuses()) = %d, want 9", got)
	}
	for _, s := range models.LeadStatuses() {
		if !s.Valid() {
			t.Errorf("LeadStatuses() returned %q which is not Valid()", s)
		}
	}
	if got := len(models.DealStages()); got != 5 {
		t.Errorf("len(DealStages()) = %d, want 5", got)
	}
	for _, s := range models.DealStages() {
		if !s.Valid() {
			t.Errorf("DealStages() returned %q which is not Valid()", s)
		}
	}
}
