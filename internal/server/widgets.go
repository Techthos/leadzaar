package server

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/techthos/gadget"
	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

// Interactive widget UI for the CRUD tools (see .claude/rules/mcp-server.md and
// docs/SPECIFICATIONS.md "Interactive widget UI"). Widgets are gadget Tables and
// Forms following the MCP Apps extension (io.modelcontextprotocol/ui), delivered
// in **embedded per-call mode**: each render is a fresh self-contained HTML
// document tagged with the MCP Apps HTML profile (text/html;profile=mcp-app),
// with the call's data baked in (InitialData) and a unique ui:// URI, appended to
// the tool result's content after the text/status block. Interactions flow back
// through the standard App Bridge — a widget action dispatches a tools/call that
// the host runs directly against this server; a mutating tool returns the
// refreshed collection under the table's RowsKey in structuredContent so the open
// widget repaints in place. Every table also sets a LoadTool (its list tool) so a
// remounted iframe re-hydrates from current data instead of the frozen snapshot.
// The non-UI result (status text + structuredContent) always stands alone: widget
// build/render failures are logged to stderr and never fail the tool.

// widgetPageSize is the client-side page size inside a table widget. The server
// already bounds a list result to 50 rows; this only keeps the iframe short.
const widgetPageSize = 10

// cardListPageSize is the client-side page size inside a card-list widget. Cards
// are taller than table rows, so a browse-as-cards view paginates sooner.
const cardListPageSize = 5

// Each entity table's RowsKey — the structuredContent key its rows ride under.
// A list tool returns the page under this key, the table's LoadTool re-fetches
// under it, and a mutating tool returns the refreshed collection under it to
// drive the standard MCP Apps in-place refresh (ui/notifications/tool-result).
const (
	leadsRowsKey     = "leads"
	contactsRowsKey  = "contacts"
	dealsRowsKey     = "deals"
	companiesRowsKey = "companies"
	offersRowsKey    = "offers"
)

var (
	// uiRenderEpoch namespaces this process's render URIs so a restart never
	// reuses one — mcp-ui hosts key renders by URI and would show a stale one.
	uiRenderEpoch = time.Now().UnixNano()
	uiRenderSeq   atomic.Uint64
)

// uiURI returns a URI unique to one render of one widget kind.
func uiURI(kind string) string {
	return fmt.Sprintf("ui://leadzaar/%s/%d-%d", kind, uiRenderEpoch, uiRenderSeq.Add(1))
}

// embedWidget renders w and appends the document to res.Content as an embedded
// text/html resource. Never fails the tool: errors go to stderr and res is
// returned as-is.
func embedWidget(res *mcp.CallToolResult, w gadget.Widget) {
	if res == nil || w == nil {
		return
	}
	doc, err := w.Document()
	if err != nil {
		log.Printf("widget %s: render: %v", w.Descriptor().URI, err)
		return
	}
	res.Content = append(res.Content, mcp.NewEmbeddedResource(mcp.TextResourceContents{
		URI:      w.Descriptor().URI,
		MIMEType: w.Descriptor().MIMEType, // "text/html;profile=mcp-app" (MCP Apps HTML profile)
		Text:     doc,
	}))
}

// tableRows converts a typed slice into gadget row maps; a conversion failure
// is logged and yields nil so the caller simply skips the widget.
func tableRows(items any) []map[string]any {
	rows, err := gadget.RowsOf(items)
	if err != nil {
		log.Printf("widget rows: %v", err)
		return nil
	}
	return rows
}

// embedTable builds-and-embeds in one step, skipping the widget when the rows
// could not be converted.
func embedTable(res *mcp.CallToolResult, build func([]map[string]any) *gadget.Table, items any) {
	if rows := tableRows(items); rows != nil {
		embedWidget(res, build(rows))
	}
}

// embedCardList is embedTable for a CardList collection widget.
func embedCardList(res *mcp.CallToolResult, build func([]map[string]any) *gadget.CardList, items any) {
	if rows := tableRows(items); rows != nil {
		embedWidget(res, build(rows))
	}
}

// embedCard is embedTable for a single-record Card widget; the card renders
// rows[0], so items is a one-element slice.
func embedCard(res *mcp.CallToolResult, build func([]map[string]any) *gadget.Card, items any) {
	if rows := tableRows(items); rows != nil {
		embedWidget(res, build(rows))
	}
}

// widgetFieldErrors maps a store validation error onto the form field its
// message names — best-effort substring rules, first match wins; an unmatched
// message lands on fallback so it is always visible in the form. The plain JSON
// tool error is unaffected.
func widgetFieldErrors(err error, fallback string, rules ...[2]string) map[string]string {
	msg := err.Error()
	for _, r := range rules {
		if strings.Contains(msg, r[0]) {
			return map[string]string{r[1]: msg}
		}
	}
	return map[string]string{fallback: msg}
}

// Create/update forms deliberately set no LoadTool (unlike the tables, which
// re-hydrate via their list tool). An embedded form is an edit buffer, not a
// live view: its baked snapshot (the values just submitted or saved) is exactly
// what should repaint if the host remounts the iframe, and re-fetching from the
// server would discard in-progress input. A create form has no record to load at
// all. In-place submit feedback still flows through the standard App Bridge —
// the handler returns inline field errors under the form's ErrorsKey ("errors").

// formData assembles the InitialData snapshot a form bakes: prefill under
// "values", inline field errors under "errors".
func formData(values map[string]any, errs map[string]string) map[string]any {
	data := map[string]any{}
	if values != nil {
		data["values"] = values
	}
	if errs != nil {
		data["errors"] = errs
	}
	return data
}

func fp(v float64) *float64 { return &v }

// enumOptions renders an enum slice as select options, prepended with an empty
// "unset" choice when optional.
func enumOptions[T ~string](vals []T, optional bool) []gadget.Option {
	opts := make([]gadget.Option, 0, len(vals)+1)
	if optional {
		opts = append(opts, gadget.Option{Value: "", Label: "(none)"})
	}
	for _, v := range vals {
		opts = append(opts, gadget.Opt(string(v)))
	}
	return opts
}

// --- Leads ---------------------------------------------------------------

func leadStatusBadges() map[string]gadget.BadgeVariant {
	return map[string]gadget.BadgeVariant{
		string(models.StatusNew):                 gadget.BadgeInfo,
		string(models.StatusContacted):           gadget.BadgeNeutral,
		string(models.StatusContactedFirstTouch): gadget.BadgeNeutral,
		string(models.StatusContactedFollowup1):  gadget.BadgeNeutral,
		string(models.StatusContactedFollowup2):  gadget.BadgeNeutral,
		string(models.StatusContactedFollowup3):  gadget.BadgeNeutral,
		string(models.StatusQualified):           gadget.BadgeWarning,
		string(models.StatusConverted):           gadget.BadgeSuccess,
		string(models.StatusLost):                gadget.BadgeDanger,
	}
}

func leadsTable(title string, rows []map[string]any) *gadget.Table {
	return &gadget.Table{
		URI:      uiURI("leads"),
		Title:    title,
		RowsKey:  leadsRowsKey,
		LoadTool: "list_leads",
		Columns: []gadget.Column{
			gadget.Number("id", "ID", "int"),
			gadget.Text("name", "Name"),
			gadget.Text("email", "Email"),
			gadget.Badge("status", "Status", leadStatusBadges()),
			gadget.Text("source", "Source"),
			gadget.Number("quality", "Quality", "int"),
			gadget.Text("unavailableUntil", "Away until"),
			gadget.Date("updatedAt", "Updated", "relative"),
			gadget.ActionsColumn(
				gadget.Action{
					Label: "Convert", Tool: "convert_lead",
					Args:    map[string]gadget.ArgSource{"id": gadget.FromRow("id")},
					Confirm: "Convert this lead into a contact?",
					Variant: gadget.VariantPrimary,
				},
				gadget.Action{
					Label: "Delete", Tool: "delete_lead",
					Args:    map[string]gadget.ArgSource{"id": gadget.FromRow("id")},
					Confirm: "Delete this lead and all of its offers?",
					Variant: gadget.VariantDanger,
				},
			),
		},
		Filterable:  true,
		PageSize:    widgetPageSize,
		Empty:       gadget.EmptyState{Title: "No leads"},
		InitialData: map[string]any{leadsRowsKey: rows},
	}
}

// leadForm is the create/update input widget. submitTool selects the flow:
// "create_lead" (no id/status fields — status defaults to new) or
// "update_lead" (readonly id, status select).
func leadForm(submitTool string, values map[string]any, errs map[string]string) *gadget.Form {
	edit := submitTool == "update_lead"
	fields := make([]gadget.Field, 0, 11)
	if edit {
		fields = append(fields, gadget.Field{Name: "id", Label: "ID", Type: gadget.FReadonly, Required: true})
	}
	fields = append(
		fields,
		gadget.Field{Name: "name", Label: "Name", Required: true},
		gadget.Field{Name: "company_id", Label: "Company ID", Type: gadget.FNumber, Description: "0 = none"},
		gadget.Field{Name: "email", Label: "Email"},
		gadget.Field{Name: "phone", Label: "Phone"},
		gadget.Field{Name: "tags", Label: "Tags", Description: "Comma-separated"},
		gadget.Field{
			Name: "quality", Label: "Quality", Type: gadget.FNumber,
			Description: "1-10, 0 = unscored",
			Validation:  &gadget.Validation{Min: fp(0), Max: fp(10)},
		},
		gadget.Field{Name: "source", Label: "Source", Type: gadget.FSelect, Options: enumOptions(models.Sources(), true)},
	)
	if edit {
		fields = append(fields, gadget.Field{
			Name: "status", Label: "Status", Type: gadget.FSelect,
			Options: enumOptions(models.LeadStatuses(), false),
		})
	}
	fields = append(
		fields,
		gadget.Field{
			Name: "unavailable_until", Label: "Unavailable until", Type: gadget.FDate,
			Description: "Out-of-office block; clear to mark reachable",
		},
		gadget.Field{Name: "notes", Label: "Notes", Type: gadget.FTextarea, Rows: 4},
	)
	title := "New lead"
	if edit {
		title = "Edit lead"
	}
	return &gadget.Form{
		URI:         uiURI("lead-form"),
		Title:       title,
		Fields:      fields,
		Submit:      gadget.SubmitSpec{Tool: submitTool, Label: "Save", SuccessMessage: "Lead saved."},
		InitialData: formData(values, errs),
	}
}

func leadValues(l models.Lead) map[string]any {
	return map[string]any{
		"id":                strconv.FormatUint(l.ID, 10),
		"name":              l.Name,
		"company_id":        l.CompanyID,
		"email":             l.Email,
		"phone":             l.Phone,
		"tags":              strings.Join(l.Tags, ", "),
		"quality":           l.Quality,
		"source":            string(l.Source),
		"status":            string(l.Status),
		"unavailable_until": models.FormatDate(l.UnavailableUntil),
		"notes":             l.Notes,
	}
}

func createLeadValues(a createLeadArgs) map[string]any {
	return map[string]any{
		"name":              a.Name,
		"company_id":        a.CompanyID,
		"email":             a.Email,
		"phone":             a.Phone,
		"tags":              strings.Join(a.Tags, ", "),
		"quality":           a.Quality,
		"source":            a.Source,
		"unavailable_until": a.UnavailableUntil,
		"notes":             a.Notes,
	}
}

func leadFieldErrors(err error) map[string]string {
	return widgetFieldErrors(
		err, "name",
		[2]string{"source", "source"},
		[2]string{"status", "status"},
		[2]string{"quality", "quality"},
		[2]string{"date", "unavailable_until"},
		[2]string{"company", "company_id"},
		[2]string{"name", "name"},
	)
}

// latestLeads returns the default first page of leads for a mutating tool to
// return under leadsRowsKey (driving the in-place table refresh) and embed as a
// fallback widget. A query failure is logged and yields nil so the tool result
// simply omits the refreshed rows rather than failing.
func (h *handlers) latestLeads() []leadListItem {
	page, err := h.store.QueryLeads(db.LeadQuery{})
	if err != nil {
		log.Printf("widget refresh leads: %v", err)
		return nil
	}
	return toLeadListItems(page.Leads)
}

// --- Contacts ------------------------------------------------------------

// contactCardTemplate is the shared card layout for the contact browse list and
// the single-contact detail card. Like the company template it drops the long
// freeform `notes` (the list projection already omits it) and includes the
// per-card Delete action only in the collection.
func contactCardTemplate(withActions bool) gadget.CardTemplate {
	t := gadget.CardTemplate{
		TitleKey:    "name",
		SubtitleKey: "email",
		Fields: []gadget.Column{
			gadget.Text("phone", "Phone"),
			gadget.Number("companyId", "Company", "int"),
			gadget.Date("updatedAt", "Updated", "relative"),
		},
	}
	if withActions {
		t.Actions = []gadget.Action{{
			Label: "Delete", Tool: "delete_contact",
			Args:    map[string]gadget.ArgSource{"id": gadget.FromRow("id")},
			Confirm: "Delete this contact and all of its deals?",
			Variant: gadget.VariantDanger,
		}}
	}
	return t
}

// contactsCardList is the browse/search widget for contacts; see
// companiesCardList for the shape and the app-template role.
func contactsCardList(title string, rows []map[string]any) *gadget.CardList {
	return &gadget.CardList{
		URI:         uiURI("contacts"),
		Title:       title,
		Template:    contactCardTemplate(true),
		RowsKey:     contactsRowsKey,
		LoadTool:    "list_contacts",
		PageSize:    cardListPageSize,
		Filterable:  true,
		Empty:       gadget.EmptyState{Title: "No contacts"},
		InitialData: map[string]any{contactsRowsKey: rows},
	}
}

// contactCard is the single-record detail widget embedded by get_contact; see
// companyCard for the no-LoadTool rationale.
func contactCard(title string, rows []map[string]any) *gadget.Card {
	return &gadget.Card{
		URI:         uiURI("contact"),
		Title:       title,
		Template:    contactCardTemplate(false),
		RowsKey:     contactsRowsKey,
		Empty:       gadget.EmptyState{Title: "No contact"},
		InitialData: map[string]any{contactsRowsKey: rows},
	}
}

func contactForm(submitTool string, values map[string]any, errs map[string]string) *gadget.Form {
	edit := submitTool == "update_contact"
	fields := make([]gadget.Field, 0, 7)
	if edit {
		fields = append(fields, gadget.Field{Name: "id", Label: "ID", Type: gadget.FReadonly, Required: true})
	}
	fields = append(
		fields,
		gadget.Field{Name: "name", Label: "Name", Required: true},
		gadget.Field{Name: "company_id", Label: "Company ID", Type: gadget.FNumber, Description: "0 = none"},
		gadget.Field{Name: "email", Label: "Email"},
		gadget.Field{Name: "phone", Label: "Phone"},
		gadget.Field{Name: "tags", Label: "Tags", Description: "Comma-separated"},
		gadget.Field{Name: "notes", Label: "Notes", Type: gadget.FTextarea, Rows: 4},
	)
	title := "New contact"
	if edit {
		title = "Edit contact"
	}
	return &gadget.Form{
		URI:         uiURI("contact-form"),
		Title:       title,
		Fields:      fields,
		Submit:      gadget.SubmitSpec{Tool: submitTool, Label: "Save", SuccessMessage: "Contact saved."},
		InitialData: formData(values, errs),
	}
}

func contactValues(c models.Contact) map[string]any {
	return map[string]any{
		"id":         strconv.FormatUint(c.ID, 10),
		"name":       c.Name,
		"company_id": c.CompanyID,
		"email":      c.Email,
		"phone":      c.Phone,
		"tags":       strings.Join(c.Tags, ", "),
		"notes":      c.Notes,
	}
}

func createContactValues(a createContactArgs) map[string]any {
	return map[string]any{
		"name":       a.Name,
		"company_id": a.CompanyID,
		"email":      a.Email,
		"phone":      a.Phone,
		"tags":       strings.Join(a.Tags, ", "),
		"notes":      a.Notes,
	}
}

func contactFieldErrors(err error) map[string]string {
	return widgetFieldErrors(
		err, "name",
		[2]string{"company", "company_id"},
		[2]string{"name", "name"},
	)
}

func (h *handlers) latestContacts() []contactListItem {
	page, err := h.store.QueryContacts(db.ContactQuery{})
	if err != nil {
		log.Printf("widget refresh contacts: %v", err)
		return nil
	}
	return toContactListItems(page.Contacts)
}

// --- Deals ---------------------------------------------------------------

func dealStageBadges() map[string]gadget.BadgeVariant {
	return map[string]gadget.BadgeVariant{
		string(models.StageQualification): gadget.BadgeInfo,
		string(models.StageProposal):      gadget.BadgeNeutral,
		string(models.StageNegotiation):   gadget.BadgeWarning,
		string(models.StageWon):           gadget.BadgeSuccess,
		string(models.StageLost):          gadget.BadgeDanger,
	}
}

func dealsTable(title string, rows []map[string]any) *gadget.Table {
	return &gadget.Table{
		URI:      uiURI("deals"),
		Title:    title,
		RowsKey:  dealsRowsKey,
		LoadTool: "list_deals",
		Columns: []gadget.Column{
			gadget.Number("id", "ID", "int"),
			gadget.Text("title", "Title"),
			gadget.Number("contactId", "Contact", "int"),
			gadget.Number("value", "Value", "decimal:2"),
			gadget.Text("currency", "Currency"),
			gadget.Badge("stage", "Stage", dealStageBadges()),
			gadget.Date("updatedAt", "Updated", "relative"),
			gadget.ActionsColumn(
				gadget.Action{
					Label: "Delete", Tool: "delete_deal",
					Args:    map[string]gadget.ArgSource{"id": gadget.FromRow("id")},
					Confirm: "Delete this deal?",
					Variant: gadget.VariantDanger,
				},
			),
		},
		Filterable:  true,
		PageSize:    widgetPageSize,
		Empty:       gadget.EmptyState{Title: "No deals"},
		InitialData: map[string]any{dealsRowsKey: rows},
	}
}

func dealForm(submitTool string, values map[string]any, errs map[string]string) *gadget.Form {
	edit := submitTool == "update_deal"
	fields := make([]gadget.Field, 0, 8)
	if edit {
		fields = append(fields, gadget.Field{Name: "id", Label: "ID", Type: gadget.FReadonly, Required: true})
	}
	fields = append(
		fields,
		gadget.Field{Name: "title", Label: "Title", Required: true},
		gadget.Field{Name: "contact_id", Label: "Contact ID", Type: gadget.FNumber, Required: true},
		gadget.Field{Name: "company_id", Label: "Company ID", Type: gadget.FNumber, Description: "0 = none"},
		gadget.Field{Name: "value", Label: "Value", Type: gadget.FNumber},
		gadget.Field{Name: "currency", Label: "Currency", Description: "3-letter code, required for non-zero value"},
		gadget.Field{Name: "stage", Label: "Stage", Type: gadget.FSelect, Options: enumOptions(models.DealStages(), false)},
		gadget.Field{Name: "notes", Label: "Notes", Type: gadget.FTextarea, Rows: 4},
	)
	title := "New deal"
	if edit {
		title = "Edit deal"
	}
	return &gadget.Form{
		URI:         uiURI("deal-form"),
		Title:       title,
		Fields:      fields,
		Submit:      gadget.SubmitSpec{Tool: submitTool, Label: "Save", SuccessMessage: "Deal saved."},
		InitialData: formData(values, errs),
	}
}

func dealValues(d models.Deal) map[string]any {
	return map[string]any{
		"id":         strconv.FormatUint(d.ID, 10),
		"title":      d.Title,
		"contact_id": d.ContactID,
		"company_id": d.CompanyID,
		"value":      d.Value,
		"currency":   d.Currency,
		"stage":      string(d.Stage),
		"notes":      d.Notes,
	}
}

func createDealValues(a createDealArgs) map[string]any {
	return map[string]any{
		"title":      a.Title,
		"contact_id": a.ContactID,
		"company_id": a.CompanyID,
		"value":      a.Value,
		"currency":   a.Currency,
		"stage":      a.Stage,
		"notes":      a.Notes,
	}
}

func dealFieldErrors(err error) map[string]string {
	return widgetFieldErrors(
		err, "title",
		[2]string{"stage", "stage"},
		[2]string{"currency", "currency"},
		[2]string{"contact", "contact_id"},
		[2]string{"company", "company_id"},
		[2]string{"name", "title"}, // the shared errEmptyName sentinel guards Title here
	)
}

func (h *handlers) latestDeals() []dealListItem {
	page, err := h.store.QueryDeals(db.DealQuery{})
	if err != nil {
		log.Printf("widget refresh deals: %v", err)
		return nil
	}
	return toDealListItems(page.Deals)
}

// --- Companies -----------------------------------------------------------

// companyCardTemplate is the shared card layout for the company browse list and
// the single-company detail card. It deliberately drops the long freeform
// `notes` and the `industry` line, keeping only the identifying fields. The
// per-card Delete action is included only in the collection (withActions), never
// on the standalone detail card (a detail view is read-only, and its refresh
// would return a collection the single card cannot represent).
func companyCardTemplate(withActions bool) gadget.CardTemplate {
	t := gadget.CardTemplate{
		TitleKey:    "name",
		SubtitleKey: "website",
		Fields: []gadget.Column{
			gadget.Text("phone", "Phone"),
			gadget.Date("updatedAt", "Updated", "relative"),
		},
	}
	if withActions {
		t.Actions = []gadget.Action{{
			Label: "Delete", Tool: "delete_company",
			Args:    map[string]gadget.ArgSource{"id": gadget.FromRow("id")},
			Confirm: "Delete this company? Linked records are unlinked, not deleted.",
			Variant: gadget.VariantDanger,
		}}
	}
	return t
}

// companiesCardList is the browse/search widget for companies: a filterable,
// paginated grid of company cards. list_companies (and the delete refresh) embed
// it, and it is the stable app template linked from list_companies.
func companiesCardList(title string, rows []map[string]any) *gadget.CardList {
	return &gadget.CardList{
		URI:         uiURI("companies"),
		Title:       title,
		Template:    companyCardTemplate(true),
		RowsKey:     companiesRowsKey,
		LoadTool:    "list_companies",
		PageSize:    cardListPageSize,
		Filterable:  true,
		Empty:       gadget.EmptyState{Title: "No companies"},
		InitialData: map[string]any{companiesRowsKey: rows},
	}
}

// companyCard is the single-record detail widget embedded by get_company. Like
// the create/update forms it sets no LoadTool: get_company returns the flat
// record (not a rows array under companiesRowsKey), so the baked snapshot is what
// should repaint on remount.
func companyCard(title string, rows []map[string]any) *gadget.Card {
	return &gadget.Card{
		URI:         uiURI("company"),
		Title:       title,
		Template:    companyCardTemplate(false),
		RowsKey:     companiesRowsKey,
		Empty:       gadget.EmptyState{Title: "No company"},
		InitialData: map[string]any{companiesRowsKey: rows},
	}
}

func companyForm(submitTool string, values map[string]any, errs map[string]string) *gadget.Form {
	edit := submitTool == "update_company"
	fields := make([]gadget.Field, 0, 6)
	if edit {
		fields = append(fields, gadget.Field{Name: "id", Label: "ID", Type: gadget.FReadonly, Required: true})
	}
	fields = append(
		fields,
		gadget.Field{Name: "name", Label: "Name", Required: true},
		gadget.Field{Name: "website", Label: "Website"},
		gadget.Field{Name: "industry", Label: "Industry"},
		gadget.Field{Name: "phone", Label: "Phone"},
		gadget.Field{Name: "notes", Label: "Notes", Type: gadget.FTextarea, Rows: 4},
	)
	title := "New company"
	if edit {
		title = "Edit company"
	}
	return &gadget.Form{
		URI:         uiURI("company-form"),
		Title:       title,
		Fields:      fields,
		Submit:      gadget.SubmitSpec{Tool: submitTool, Label: "Save", SuccessMessage: "Company saved."},
		InitialData: formData(values, errs),
	}
}

func companyValues(c models.Company) map[string]any {
	return map[string]any{
		"id":       strconv.FormatUint(c.ID, 10),
		"name":     c.Name,
		"website":  c.Website,
		"industry": c.Industry,
		"phone":    c.Phone,
		"notes":    c.Notes,
	}
}

func createCompanyValues(a createCompanyArgs) map[string]any {
	return map[string]any{
		"name":     a.Name,
		"website":  a.Website,
		"industry": a.Industry,
		"phone":    a.Phone,
		"notes":    a.Notes,
	}
}

func companyFieldErrors(err error) map[string]string {
	return widgetFieldErrors(err, "name", [2]string{"name", "name"})
}

func (h *handlers) latestCompanies() []companyListItem {
	page, err := h.store.QueryCompanies(db.CompanyQuery{})
	if err != nil {
		log.Printf("widget refresh companies: %v", err)
		return nil
	}
	return toCompanyListItems(page.Companies)
}

// --- Offers --------------------------------------------------------------

func offersTable(title string, rows []map[string]any) *gadget.Table {
	return &gadget.Table{
		URI:      uiURI("offers"),
		Title:    title,
		RowsKey:  offersRowsKey,
		LoadTool: "list_offers",
		Columns: []gadget.Column{
			gadget.Number("id", "ID", "int"),
			gadget.Number("leadId", "Lead", "int"),
			gadget.Text("title", "Title"),
			gadget.Text("subject", "Subject"),
			gadget.Date("updatedAt", "Updated", "relative"),
			gadget.ActionsColumn(
				gadget.Action{
					Label: "Delete", Tool: "delete_offer",
					Args:    map[string]gadget.ArgSource{"id": gadget.FromRow("id")},
					Confirm: "Delete this offer?",
					Variant: gadget.VariantDanger,
				},
			),
		},
		Filterable:  true,
		PageSize:    widgetPageSize,
		Empty:       gadget.EmptyState{Title: "No offers"},
		InitialData: map[string]any{offersRowsKey: rows},
	}
}

// offerCard is the single-record detail widget embedded by get_offer. Unlike the
// offer list projection (which drops the long freeform body), the detail card
// carries the full body and description so an offer can be read in one view. It
// sets no LoadTool for the same reason as the other detail cards (get_offer
// returns the flat record, not a rows array under offersRowsKey).
func offerCard(title string, rows []map[string]any) *gadget.Card {
	return &gadget.Card{
		URI:     uiURI("offer"),
		Title:   title,
		RowsKey: offersRowsKey,
		Template: gadget.CardTemplate{
			TitleKey:    "title",
			SubtitleKey: "subject",
			Fields: []gadget.Column{
				gadget.Number("leadId", "Lead", "int"),
				gadget.Text("description", "Description"),
				gadget.Text("body", "Body"),
				gadget.Date("updatedAt", "Updated", "relative"),
			},
		},
		Empty:       gadget.EmptyState{Title: "No offer"},
		InitialData: map[string]any{offersRowsKey: rows},
	}
}

// offerDetailRow projects a full offer (including the body dropped from the list
// item) into the card's row shape; keys match offerCard's template fields.
func offerDetailRow(o models.Offer) map[string]any {
	return map[string]any{
		"id":          o.ID,
		"leadId":      o.LeadID,
		"title":       o.Title,
		"subject":     o.Subject,
		"description": o.Description,
		"body":        o.Body,
		"updatedAt":   o.UpdatedAt,
	}
}

func offerForm(submitTool string, values map[string]any, errs map[string]string) *gadget.Form {
	edit := submitTool == "update_offer"
	fields := make([]gadget.Field, 0, 6)
	if edit {
		fields = append(fields, gadget.Field{Name: "id", Label: "ID", Type: gadget.FReadonly, Required: true})
	}
	fields = append(
		fields,
		gadget.Field{Name: "lead_id", Label: "Lead ID", Type: gadget.FNumber, Required: true},
		gadget.Field{Name: "title", Label: "Title", Required: true},
		gadget.Field{Name: "description", Label: "Description", Type: gadget.FTextarea, Rows: 3},
		gadget.Field{Name: "subject", Label: "Email subject"},
		gadget.Field{Name: "body", Label: "Email body", Type: gadget.FTextarea, Rows: 8},
	)
	title := "New offer"
	if edit {
		title = "Edit offer"
	}
	return &gadget.Form{
		URI:         uiURI("offer-form"),
		Title:       title,
		Fields:      fields,
		Submit:      gadget.SubmitSpec{Tool: submitTool, Label: "Save", SuccessMessage: "Offer saved."},
		InitialData: formData(values, errs),
	}
}

func offerValues(o models.Offer) map[string]any {
	return map[string]any{
		"id":          strconv.FormatUint(o.ID, 10),
		"lead_id":     o.LeadID,
		"title":       o.Title,
		"description": o.Description,
		"subject":     o.Subject,
		"body":        o.Body,
	}
}

func createOfferValues(a createOfferArgs) map[string]any {
	return map[string]any{
		"lead_id":     a.LeadID,
		"title":       a.Title,
		"description": a.Description,
		"subject":     a.Subject,
		"body":        a.Body,
	}
}

func offerFieldErrors(err error) map[string]string {
	return widgetFieldErrors(
		err, "title",
		[2]string{"lead", "lead_id"},
		[2]string{"name", "title"}, // the shared errEmptyName sentinel guards Title here
	)
}

func (h *handlers) latestOffers() []offerListItem {
	page, err := h.store.QueryOffers(db.OfferQuery{})
	if err != nil {
		log.Printf("widget refresh offers: %v", err)
		return nil
	}
	return toOfferListItems(page.Offers)
}

// --- Pipeline summary ----------------------------------------------------

// stageSummaryRow flattens a StageSummary to one row per stage-currency pair
// (or a single zero row for a stage with no valued deals). The synthetic ID
// keys the table rows.
type stageSummaryRow struct {
	ID       string  `json:"id"`
	Stage    string  `json:"stage"`
	Count    int     `json:"count"`
	Currency string  `json:"currency,omitempty"`
	Total    float64 `json:"total"`
}

type statusCountRow struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// Summary-table RowsKeys. pipeline_summary returns its two flattened row sets
// under these keys so each read-only table's LoadTool ("pipeline_summary")
// re-hydrates on remount.
const (
	summaryDealRowsKey   = "dealRows"
	summaryStatusRowsKey = "statusRows"
)

func summaryDealsTable(rows []map[string]any) *gadget.Table {
	return &gadget.Table{
		URI:      uiURI("pipeline-deals"),
		Title:    "Deals by stage",
		RowsKey:  summaryDealRowsKey,
		LoadTool: "pipeline_summary",
		Columns: []gadget.Column{
			gadget.Badge("stage", "Stage", dealStageBadges()),
			gadget.Number("count", "Deals", "int"),
			gadget.Text("currency", "Currency"),
			gadget.Number("total", "Total", "decimal:2"),
		},
		Empty:       gadget.EmptyState{Title: "No deals"},
		InitialData: map[string]any{summaryDealRowsKey: rows},
	}
}

func summaryLeadsTable(rows []map[string]any) *gadget.Table {
	return &gadget.Table{
		URI:      uiURI("pipeline-leads"),
		Title:    "Leads by status",
		RowsKey:  summaryStatusRowsKey,
		LoadTool: "pipeline_summary",
		Columns: []gadget.Column{
			gadget.Badge("status", "Status", leadStatusBadges()),
			gadget.Number("count", "Leads", "int"),
		},
		Empty:       gadget.EmptyState{Title: "No leads"},
		InitialData: map[string]any{summaryStatusRowsKey: rows},
	}
}

// pipelineSummaryRows flattens a PipelineSummary into the two table row sets:
// deals by stage (one row per stage-currency pair, or a single zero row for a
// stage with no valued deals — totals are never summed across currencies) and
// leads by status.
func pipelineSummaryRows(s models.PipelineSummary) (dealRows []stageSummaryRow, statusRows []statusCountRow) {
	dealRows = make([]stageSummaryRow, 0, len(s.DealsByStage))
	for _, st := range s.DealsByStage {
		if len(st.Totals) == 0 {
			dealRows = append(dealRows, stageSummaryRow{ID: string(st.Stage), Stage: string(st.Stage), Count: st.Count})
			continue
		}
		for _, tot := range st.Totals {
			dealRows = append(dealRows, stageSummaryRow{
				ID: string(st.Stage) + "/" + tot.Currency, Stage: string(st.Stage),
				Count: st.Count, Currency: tot.Currency, Total: tot.Total,
			})
		}
	}
	statusRows = make([]statusCountRow, 0, len(s.LeadsByStatus))
	for _, sc := range s.LeadsByStatus {
		statusRows = append(statusRows, statusCountRow{ID: string(sc.Status), Status: string(sc.Status), Count: sc.Count})
	}
	return dealRows, statusRows
}

// embedPipelineSummary embeds the two read-only summary tables from
// pre-flattened rows.
func embedPipelineSummary(res *mcp.CallToolResult, dealRows []stageSummaryRow, statusRows []statusCountRow) {
	embedTable(res, summaryDealsTable, dealRows)
	embedTable(res, summaryLeadsTable, statusRows)
}
