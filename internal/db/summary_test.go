package db_test

import (
	"testing"

	"github.com/techthos/leadzaar/internal/models"
)

func TestPipelineSummaryEmpty(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())

	got, err := store.PipelineSummary()
	if err != nil {
		t.Fatalf("PipelineSummary: %v", err)
	}
	if len(got.DealsByStage) != 5 {
		t.Errorf("DealsByStage = %d groups, want 5", len(got.DealsByStage))
	}
	if len(got.LeadsByStatus) != 9 {
		t.Errorf("LeadsByStatus = %d groups, want 9", len(got.LeadsByStatus))
	}
	for _, ss := range got.DealsByStage {
		if ss.Count != 0 || len(ss.Totals) != 0 {
			t.Errorf("stage %q not zeroed: %+v", ss.Stage, ss)
		}
	}
}

func TestPipelineSummaryGroupsByCurrency(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())
	c := mustContact(t, store, "Acme")

	seed := []models.Deal{
		{Title: "q-eur-1", ContactID: c.ID, Value: 1000, Currency: "EUR", Stage: models.StageQualification},
		{Title: "q-eur-2", ContactID: c.ID, Value: 1500, Currency: "EUR", Stage: models.StageQualification},
		{Title: "q-usd-1", ContactID: c.ID, Value: 3000, Currency: "USD", Stage: models.StageQualification},
		{Title: "won-eur", ContactID: c.ID, Value: 40000, Currency: "EUR", Stage: models.StageWon},
	}
	for _, d := range seed {
		if _, err := store.CreateDeal(d); err != nil {
			t.Fatalf("CreateDeal: %v", err)
		}
	}
	_, _ = store.CreateLead(models.Lead{Name: "L1", Status: models.StatusNew})
	_, _ = store.CreateLead(models.Lead{Name: "L2", Status: models.StatusNew})

	got, err := store.PipelineSummary()
	if err != nil {
		t.Fatalf("PipelineSummary: %v", err)
	}

	stage := stageByName(t, got, models.StageQualification)
	if stage.Count != 3 {
		t.Errorf("qualification count = %d, want 3", stage.Count)
	}
	// EUR and USD must be separate, sorted (EUR before USD), and not summed together.
	if len(stage.Totals) != 2 {
		t.Fatalf("qualification currencies = %d, want 2", len(stage.Totals))
	}
	if stage.Totals[0].Currency != "EUR" || stage.Totals[1].Currency != "USD" {
		t.Errorf("currencies not sorted: %+v", stage.Totals)
	}
	if !floatEq(stage.Totals[0].Total, 2500) {
		t.Errorf("EUR total = %v, want 2500", stage.Totals[0].Total)
	}
	if !floatEq(stage.Totals[1].Total, 3000) {
		t.Errorf("USD total = %v, want 3000", stage.Totals[1].Total)
	}

	newCount := statusCount(t, got, models.StatusNew)
	if newCount != 2 {
		t.Errorf("new lead count = %d, want 2", newCount)
	}
}

func stageByName(t *testing.T, s models.PipelineSummary, stage models.DealStage) models.StageSummary {
	t.Helper()
	for _, ss := range s.DealsByStage {
		if ss.Stage == stage {
			return ss
		}
	}
	t.Fatalf("stage %q not found in summary", stage)
	return models.StageSummary{}
}

func statusCount(t *testing.T, s models.PipelineSummary, status models.LeadStatus) int {
	t.Helper()
	for _, sc := range s.LeadsByStatus {
		if sc.Status == status {
			return sc.Count
		}
	}
	t.Fatalf("status %q not found in summary", status)
	return 0
}

func floatEq(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}
