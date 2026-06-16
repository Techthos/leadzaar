package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/techthos/microapp-crm/internal/models"
)

// testCompanyName is a fixed CompanyID→name resolver for render tests.
func testCompanyName(id uint64) string {
	return map[uint64]string{7: "Acme"}[id]
}

func TestLeadCells(t *testing.T) {
	t.Parallel()
	row := leadCells(models.Lead{
		ID: 1, Name: "Jane", CompanyID: 7, Email: "j@x.io",
		Status: models.StatusNew, Quality: 8, Source: models.SourceWeb,
	}, testCompanyName)
	if row[0] != "1" || row[1] != "Jane" || row[2] != "Acme" || row[4] != "new" || row[5] != "8" || row[6] != "web" {
		t.Errorf("unexpected lead cells: %v", row)
	}
}

func TestContactAndDealCells(t *testing.T) {
	t.Parallel()
	crow := contactCells(models.Contact{ID: 2, Name: "Ada", CompanyID: 7, Phone: "555"}, testCompanyName)
	if crow[0] != "2" || crow[1] != "Ada" || crow[2] != "Acme" || crow[4] != "555" {
		t.Errorf("contact cells: %v", crow)
	}
	drow := dealCells(models.Deal{ID: 3, Title: "Big", ContactID: 2, Value: 1500, Currency: "EUR", Stage: models.StageWon})
	if drow[0] != "3" || drow[3] != "1500.00" || drow[5] != "won" {
		t.Errorf("deal cells: %v", drow)
	}
}

func TestCompanyCellsAndDetail(t *testing.T) {
	t.Parallel()
	row := companyCells(models.Company{ID: 7, Name: "Acme", Industry: "Tech", Website: "acme.io", Phone: "555"})
	if row[0] != "7" || row[1] != "Acme" || row[2] != "Tech" || row[3] != "acme.io" {
		t.Errorf("company cells: %v", row)
	}
	out := companyDetail(models.Company{Name: "Acme", Industry: "Tech"})
	for _, want := range []string{"Name:", "Acme", "Industry:", "Tech", "Created:"} {
		if !strings.Contains(out, want) {
			t.Errorf("company detail missing %q in:\n%s", want, out)
		}
	}
}

func TestDashFormatsMissingValues(t *testing.T) {
	t.Parallel()
	if got := dash(""); !strings.Contains(got, "—") {
		t.Errorf("dash(empty) = %q, want an em-dash placeholder", got)
	}
	if got := dash("x"); got != "x" {
		t.Errorf("dash(x) = %q, want passthrough", got)
	}
}

func TestRelSince(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"seconds", 5 * time.Second, "just now"},
		{"minutes", 3 * time.Minute, "3m ago"},
		{"hours", 2 * time.Hour, "2h ago"},
		{"days", 49 * time.Hour, "2d ago"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := relSince(tc.in); got != tc.want {
				t.Errorf("relSince(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLeadDetailShowsFields(t *testing.T) {
	t.Parallel()
	offers := []models.Offer{{ID: 3, LeadID: 1, Title: "Pilot", Subject: "Intro"}}
	out := leadDetail(models.Lead{Name: "Jane", CompanyID: 7, Tags: []string{"vip"}, Quality: 8, Status: models.StatusNew}, testCompanyName, offers)
	for _, want := range []string{"Name:", "Jane", "Company:", "Acme", "Tags:", "vip", "Quality:", "8", "Status:", "new", "Offers", "Pilot", "Created:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lead detail missing %q in:\n%s", want, out)
		}
	}
}

func TestSummaryLinesGroupsByCurrency(t *testing.T) {
	t.Parallel()
	s := models.PipelineSummary{
		DealsByStage: []models.StageSummary{
			{Stage: models.StageQualification, Count: 2, Totals: []models.CurrencyTotal{
				{Currency: "EUR", Total: 2500}, {Currency: "USD", Total: 3000},
			}},
			{Stage: models.StageWon, Count: 0},
		},
		LeadsByStatus: []models.StatusCount{
			{Status: models.StatusNew, Count: 4}, {Status: models.StatusLost, Count: 1},
		},
	}
	out := strings.Join(summaryLines(s), "\n")
	for _, want := range []string{"LEADS", "new:4", "lost:1", "qualification", "EUR 2500.00", "USD 3000.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q in:\n%s", want, out)
		}
	}
	// EUR and USD must appear separately, not summed.
	if strings.Contains(out, "5500") {
		t.Errorf("currencies were summed across:\n%s", out)
	}
}

func TestOptionHelpers(t *testing.T) {
	t.Parallel()
	if got := sourceOptions(); got[0] != "" || len(got) != 6 {
		t.Errorf("sourceOptions = %v, want blank + 5", got)
	}
	if got := statusOptions(); len(got) != 5 {
		t.Errorf("statusOptions = %d, want 5", len(got))
	}
	if got := indexOf(stageOptions(), "won"); got != 3 {
		t.Errorf("indexOf(won) = %d, want 3", got)
	}
	if got := indexOf([]string{"a", "b"}, "missing"); got != 0 {
		t.Errorf("indexOf(missing) = %d, want 0", got)
	}
}

func TestFormatMoneyAndParse(t *testing.T) {
	t.Parallel()
	if got := formatMoney(1500); got != "1500.00" {
		t.Errorf("formatMoney = %q", got)
	}
	if got := parseFloat(" 12.5 "); got != 12.5 {
		t.Errorf("parseFloat = %v", got)
	}
	if got := parseFloat("nope"); got != 0 {
		t.Errorf("parseFloat(invalid) = %v, want 0", got)
	}
	if got := parseUint("42"); got != 42 {
		t.Errorf("parseUint = %v", got)
	}
}
