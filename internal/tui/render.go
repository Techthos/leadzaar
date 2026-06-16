// Package tui is the terminal UI for microapp-crm, built on rivo/tview. It owns
// a single tview.Application and pulls all data through the db.Store — no bbolt
// access or business logic lives here. See docs/SPECIFICATIONS.md (TUI Surface)
// and .claude/rules/tui-rules.md.
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/techthos/microapp-crm/internal/models"
)

// col describes a table column: its header and whether it holds numbers (which
// are right-aligned per the shared design language).
type col struct {
	title string
	right bool
}

// Column layouts for each list screen. Numbers are right-aligned; the trailing
// "Updated" column shows a relative timestamp (absolute lives in the detail).
var (
	leadCols = []col{
		{"ID", true},
		{"Name", false},
		{"Company", false},
		{"Email", false},
		{"Status", false},
		{"Qual", true},
		{"Source", false},
		{"Updated", false},
	}
	contactCols = []col{
		{"ID", true},
		{"Name", false},
		{"Company", false},
		{"Email", false},
		{"Phone", false},
		{"Updated", false},
	}
	dealCols = []col{
		{"ID", true},
		{"Title", false},
		{"Contact", true},
		{"Value", true},
		{"Cur", false},
		{"Stage", false},
		{"Updated", false},
	}
	companyCols = []col{
		{"ID", true},
		{"Name", false},
		{"Industry", false},
		{"Website", false},
		{"Phone", false},
		{"Updated", false},
	}
)

// companyName resolves a CompanyID to its name (via the tui's snapshot map),
// returning "" when unlinked or unknown — callers dash() it for display.
func (t *tui) companyName(id uint64) string {
	if id == 0 {
		return ""
	}
	return t.companyNames[id]
}

// leadCells renders one lead as a table row (column order matches leadCols).
// companyName resolves the lead's linked Company to a display name.
func leadCells(l models.Lead, companyName func(uint64) string) []string {
	return []string{
		strconv.FormatUint(l.ID, 10), dash(l.Name), dash(companyName(l.CompanyID)),
		dash(l.Email), string(l.Status), qualityText(l.Quality), dashSource(l.Source), relTime(l.UpdatedAt),
	}
}

func contactCells(c models.Contact, companyName func(uint64) string) []string {
	return []string{
		strconv.FormatUint(c.ID, 10), dash(c.Name), dash(companyName(c.CompanyID)),
		dash(c.Email), dash(c.Phone), relTime(c.UpdatedAt),
	}
}

func dealCells(d models.Deal) []string {
	return []string{
		strconv.FormatUint(d.ID, 10), dash(d.Title), strconv.FormatUint(d.ContactID, 10),
		formatMoney(d.Value), dash(d.Currency), string(d.Stage), relTime(d.UpdatedAt),
	}
}

func companyCells(c models.Company) []string {
	return []string{
		strconv.FormatUint(c.ID, 10), dash(c.Name), dash(c.Industry),
		dash(c.Website), dash(c.Phone), relTime(c.UpdatedAt),
	}
}

// leadDetail renders the full lead record for the detail pane (absolute
// timestamps, dim em-dash for missing values, field order matching the form).
func leadDetail(l models.Lead, companyName func(uint64) string) string {
	var b strings.Builder
	field(&b, "Name", l.Name)
	field(&b, "Company", companyName(l.CompanyID))
	field(&b, "Email", l.Email)
	field(&b, "Phone", l.Phone)
	field(&b, "Tags", strings.Join(l.Tags, ", "))
	field(&b, "Quality", qualityField(l.Quality))
	field(&b, "Source", string(l.Source))
	field(&b, "Status", string(l.Status))
	field(&b, "Notes", l.Notes)
	if l.Status == models.StatusConverted {
		field(&b, "Contact ID", uintField(l.ContactID))
		field(&b, "Deal ID", uintField(l.DealID))
	}
	b.WriteString("\n")
	field(&b, "Created", absTime(l.CreatedAt))
	field(&b, "Updated", absTime(l.UpdatedAt))
	return b.String()
}

// contactDetail renders a contact plus its deals (UC-12).
func contactDetail(c models.Contact, deals []models.Deal, companyName func(uint64) string) string {
	var b strings.Builder
	field(&b, "Name", c.Name)
	field(&b, "Company", companyName(c.CompanyID))
	field(&b, "Email", c.Email)
	field(&b, "Phone", c.Phone)
	field(&b, "Tags", strings.Join(c.Tags, ", "))
	field(&b, "Notes", c.Notes)
	b.WriteString("\n[::b]Deals[::-]\n")
	if len(deals) == 0 {
		b.WriteString("  [gray]— none —[-]\n")
	}
	for _, d := range deals {
		fmt.Fprintf(&b, "  #%d %s — %s %s [gray](%s)[-]\n",
			d.ID, d.Title, dash(d.Currency), formatMoney(d.Value), d.Stage)
	}
	b.WriteString("\n")
	field(&b, "Created", absTime(c.CreatedAt))
	field(&b, "Updated", absTime(c.UpdatedAt))
	return b.String()
}

func dealDetail(d models.Deal, companyName func(uint64) string) string {
	var b strings.Builder
	field(&b, "Title", d.Title)
	field(&b, "Contact ID", uintField(d.ContactID))
	field(&b, "Company", companyName(d.CompanyID))
	field(&b, "Value", formatMoney(d.Value))
	field(&b, "Currency", d.Currency)
	field(&b, "Stage", string(d.Stage))
	field(&b, "Notes", d.Notes)
	b.WriteString("\n")
	field(&b, "Created", absTime(d.CreatedAt))
	field(&b, "Updated", absTime(d.UpdatedAt))
	return b.String()
}

// companyDetail renders a company record for the detail pane (field order
// matches the company form).
func companyDetail(c models.Company) string {
	var b strings.Builder
	field(&b, "Name", c.Name)
	field(&b, "Website", c.Website)
	field(&b, "Industry", c.Industry)
	field(&b, "Phone", c.Phone)
	field(&b, "Notes", c.Notes)
	b.WriteString("\n")
	field(&b, "Created", absTime(c.CreatedAt))
	field(&b, "Updated", absTime(c.UpdatedAt))
	return b.String()
}

// splitTags parses a comma-separated tag input into a trimmed, non-empty slice.
func splitTags(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// field writes a "Label: value" detail line, dimming missing values to em-dash.
func field(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "[::b]%-11s[::-] %s\n", label+":", dash(value))
}

// dash returns s, or a dim em-dash when s is empty — never blank, never null.
func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "[gray]—[-]"
	}
	return s
}

func dashSource(s models.Source) string { return dash(string(s)) }

func uintField(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

// qualityText renders a lead's 1–10 quality score for a list cell, dimming an
// unscored (0) lead to an em-dash.
func qualityText(q int) string {
	if q == 0 {
		return "[gray]—[-]"
	}
	return strconv.Itoa(q)
}

// qualityField renders a quality score for the detail pane: "" when unscored so
// the shared field() helper dims it to an em-dash like every other empty value.
func qualityField(q int) string {
	if q == 0 {
		return ""
	}
	return strconv.Itoa(q)
}

// formatMoney renders a monetary value with two decimals.
func formatMoney(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// absTime renders an absolute timestamp for detail panes.
func absTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// relTime renders a timestamp relative to now for list cells.
func relTime(t time.Time) string {
	if t.IsZero() {
		return "[gray]—[-]"
	}
	return relSince(time.Since(t))
}

// relSince renders a duration as a compact relative label (pure, for testing).
func relSince(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// sourceOptions lists lead-source dropdown choices (blank = unset, allowed).
func sourceOptions() []string {
	opts := []string{""}
	for _, s := range models.Sources() {
		opts = append(opts, string(s))
	}
	return opts
}

// statusOptions lists lead-status dropdown choices.
func statusOptions() []string {
	opts := make([]string, 0, len(models.LeadStatuses()))
	for _, s := range models.LeadStatuses() {
		opts = append(opts, string(s))
	}
	return opts
}

// stageOptions lists deal-stage dropdown choices.
func stageOptions() []string {
	opts := make([]string, 0, len(models.DealStages()))
	for _, s := range models.DealStages() {
		opts = append(opts, string(s))
	}
	return opts
}

// indexOf returns the position of val in opts, or 0 if absent.
func indexOf(opts []string, val string) int {
	for i, o := range opts {
		if o == val {
			return i
		}
	}
	return 0
}

// summaryLines renders the pipeline summary as display lines (UC-18). Deal
// values are shown grouped by currency, never summed across currencies.
func summaryLines(s models.PipelineSummary) []string {
	var lines []string

	var funnel strings.Builder
	funnel.WriteString("LEADS  ")
	for i, sc := range s.LeadsByStatus {
		if i > 0 {
			funnel.WriteString("  ")
		}
		fmt.Fprintf(&funnel, "%s:%d", sc.Status, sc.Count)
	}
	lines = append(lines, funnel.String(), "")

	lines = append(lines, "DEALS")
	for _, ss := range s.DealsByStage {
		totals := "—"
		if len(ss.Totals) > 0 {
			parts := make([]string, 0, len(ss.Totals))
			for _, ct := range ss.Totals {
				parts = append(parts, fmt.Sprintf("%s %s", ct.Currency, formatMoney(ct.Total)))
			}
			totals = strings.Join(parts, " / ")
		}
		lines = append(lines, fmt.Sprintf("  %-14s %3d deals   %s", ss.Stage, ss.Count, totals))
	}
	return lines
}
