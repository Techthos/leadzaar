package server_test

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// embeddedHTML returns the URI and document of the first embedded MCP Apps
// resource in a tool result, failing the test when none is present. The status
// text block must always come first — widgets ride after it.
func embeddedHTML(t *testing.T, res *mcp.CallToolResult) (uri, doc string) {
	t.Helper()
	if len(res.Content) < 2 {
		t.Fatalf("result has %d content blocks, want status text + embedded widget", len(res.Content))
	}
	if _, ok := mcp.AsTextContent(res.Content[0]); !ok {
		t.Fatalf("first content block is %T, want text (status line must stand alone)", res.Content[0])
	}
	for _, c := range res.Content[1:] {
		er, ok := mcp.AsEmbeddedResource(c)
		if !ok {
			continue
		}
		trc, ok := er.Resource.(mcp.TextResourceContents)
		if !ok {
			t.Fatalf("embedded resource contents are %T, want TextResourceContents", er.Resource)
		}
		if trc.MIMEType != "text/html;profile=mcp-app" {
			t.Fatalf("embedded resource MIME type = %q, want text/html;profile=mcp-app", trc.MIMEType)
		}
		return trc.URI, trc.Text
	}
	t.Fatal("no embedded resource in result content")
	return "", ""
}

// TestListToolsEmbedTableWidget verifies every list_* result carries an
// embedded per-call table widget after the standalone JSON block, each render
// with a unique ui:// URI.
func TestListToolsEmbedTableWidget(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	callTool(t, c, ctx, "create_lead", map[string]any{"name": "Widget Lead", "source": "web"})

	seen := map[string]bool{}
	for _, tool := range []string{"list_leads", "list_contacts", "list_deals", "list_companies", "list_offers"} {
		res := callTool(t, c, ctx, tool, map[string]any{})
		if res.IsError {
			t.Fatalf("%s: %s", tool, resultText(t, res))
		}
		uri, doc := embeddedHTML(t, res)
		if !strings.HasPrefix(uri, "ui://leadzaar/") {
			t.Errorf("%s widget URI = %q, want ui://leadzaar/ prefix", tool, uri)
		}
		if seen[uri] {
			t.Errorf("%s widget URI %q reused across renders", tool, uri)
		}
		seen[uri] = true
		if !strings.Contains(doc, "<!doctype html>") && !strings.Contains(doc, "<!DOCTYPE html>") {
			t.Errorf("%s widget document is not a full HTML document", tool)
		}
	}

	// The same list re-queried renders a fresh document under a fresh URI.
	res := callTool(t, c, ctx, "list_leads", map[string]any{})
	if uri, _ := embeddedHTML(t, res); seen[uri] {
		t.Errorf("second list_leads render reused URI %q", uri)
	}
}

// TestMutationWidgets covers the create/update form embeds (values baked in),
// the get_* one-row table, and the refreshed table on delete.
func TestMutationWidgets(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	res := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Form Lead", "source": "referral"})
	if res.IsError {
		t.Fatalf("create_lead: %s", resultText(t, res))
	}
	_, doc := embeddedHTML(t, res)
	if !strings.Contains(doc, "update_lead") {
		t.Error("create_lead widget form does not target update_lead")
	}
	if !strings.Contains(doc, "Form Lead") {
		t.Error("create_lead widget form is not prefilled with the saved record")
	}

	var lead struct {
		ID uint64 `json:"id"`
	}
	decodeStructured(t, res, &lead)

	res = callTool(t, c, ctx, "get_lead", map[string]any{"id": lead.ID})
	if _, doc = embeddedHTML(t, res); !strings.Contains(doc, "Form Lead") {
		t.Error("get_lead widget table does not carry the record row")
	}

	res = callTool(t, c, ctx, "update_lead", map[string]any{"id": lead.ID, "name": "Form Lead 2"})
	if _, doc = embeddedHTML(t, res); !strings.Contains(doc, "Form Lead 2") {
		t.Error("update_lead widget form is not prefilled with the updated record")
	}

	res = callTool(t, c, ctx, "delete_lead", map[string]any{"id": lead.ID})
	if res.IsError {
		t.Fatalf("delete_lead: %s", resultText(t, res))
	}
	if _, doc = embeddedHTML(t, res); strings.Contains(doc, "Form Lead 2") {
		t.Error("delete_lead refreshed table still shows the deleted row")
	}
}

// TestTableWidgetsSetLoadTool verifies every embedded table declares a LoadTool
// so a remounted iframe re-hydrates from current data rather than the frozen
// baked snapshot.
func TestTableWidgetsSetLoadTool(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	callTool(t, c, ctx, "create_lead", map[string]any{"name": "Load Lead"})

	for _, tc := range []struct{ tool, loadTool string }{
		{"list_leads", "list_leads"},
		{"list_contacts", "list_contacts"},
		{"list_deals", "list_deals"},
		{"list_companies", "list_companies"},
		{"list_offers", "list_offers"},
		{"pipeline_summary", "pipeline_summary"},
	} {
		res := callTool(t, c, ctx, tc.tool, map[string]any{})
		if res.IsError {
			t.Fatalf("%s: %s", tc.tool, resultText(t, res))
		}
		_, doc := embeddedHTML(t, res)
		if !strings.Contains(doc, `"loadTool"`) || !strings.Contains(doc, tc.loadTool) {
			t.Errorf("%s widget does not declare loadTool %q", tc.tool, tc.loadTool)
		}
	}
}

// TestMutatingToolsRefreshRowsInPlace verifies a mutating tool returns the
// refreshed collection under the target table's RowsKey in structuredContent, so
// an MCP Apps host repaints the open widget in place (not just via the embedded
// fallback). The text block is a human status line, not raw JSON.
func TestMutatingToolsRefreshRowsInPlace(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	keep := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Keep"})
	var kept struct {
		ID uint64 `json:"id"`
	}
	decodeStructured(t, keep, &kept)
	drop := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Drop"})
	var dropped struct {
		ID uint64 `json:"id"`
	}
	decodeStructured(t, drop, &dropped)

	del := callTool(t, c, ctx, "delete_lead", map[string]any{"id": dropped.ID})
	if del.IsError {
		t.Fatalf("delete_lead: %s", resultText(t, del))
	}
	if txt := resultText(t, del); strings.Contains(txt, "{") {
		t.Errorf("delete_lead status line looks like JSON: %q", txt)
	}
	var payload struct {
		Leads []struct {
			ID   uint64 `json:"id"`
			Name string `json:"name"`
		} `json:"leads"`
	}
	decodeStructured(t, del, &payload)
	if len(payload.Leads) != 1 || payload.Leads[0].ID != kept.ID {
		t.Errorf("delete_lead refreshed rows = %+v, want only lead #%d under \"leads\"", payload.Leads, kept.ID)
	}
}

// TestValidationErrorEmbedsFormWithFieldError verifies a failed create embeds
// the form with the submitted values and the error mapped onto a field, while
// the plain tool error still stands alone.
func TestValidationErrorEmbedsFormWithFieldError(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	res := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Bad Source", "source": "carrier-pigeon"})
	if !res.IsError {
		t.Fatal("create_lead with invalid source should be a tool error")
	}
	_, doc := embeddedHTML(t, res)
	if !strings.Contains(doc, "invalid lead source") {
		t.Error("widget form does not carry the field error message")
	}
	if !strings.Contains(doc, "Bad Source") {
		t.Error("widget form does not carry the submitted values")
	}
	if !strings.Contains(doc, "create_lead") {
		t.Error("retry form does not target create_lead")
	}
}

// TestBrowseCardListWidgets verifies list_companies and list_contacts embed a
// CardList (not a table), paginated at the smaller card page size, and that the
// dropped fields (notes always; company industry) are absent from the card.
func TestBrowseCardListWidgets(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	callTool(t, c, ctx, "create_company", map[string]any{
		"name": "Acme", "website": "acme.io", "industry": "Aerospace", "phone": "555-1", "notes": "secret memo",
	})
	callTool(t, c, ctx, "create_contact", map[string]any{
		"name": "Ada", "email": "ada@acme.io", "phone": "555-2", "notes": "private note",
	})

	for _, tc := range []struct{ tool, name string }{
		{"list_companies", "Acme"},
		{"list_contacts", "Ada"},
	} {
		res := callTool(t, c, ctx, tc.tool, map[string]any{})
		if res.IsError {
			t.Fatalf("%s: %s", tc.tool, resultText(t, res))
		}
		_, doc := embeddedHTML(t, res)
		if !strings.Contains(doc, `"widget":"cardlist"`) {
			t.Errorf("%s widget is not a cardlist", tc.tool)
		}
		if !strings.Contains(doc, `"pageSize":5`) {
			t.Errorf("%s cardlist does not default to page size 5", tc.tool)
		}
		if !strings.Contains(doc, tc.name) {
			t.Errorf("%s cardlist does not carry the record %q", tc.tool, tc.name)
		}
	}

	// The card must declare no industry field, and the long freeform notes
	// (dropped by the list projection) must never reach the browse card.
	res := callTool(t, c, ctx, "list_companies", map[string]any{})
	if _, doc := embeddedHTML(t, res); strings.Contains(doc, `"key":"industry"`) || strings.Contains(doc, "secret memo") {
		t.Error("company card declares an industry field or leaks notes")
	}
	res = callTool(t, c, ctx, "list_contacts", map[string]any{})
	if _, doc := embeddedHTML(t, res); strings.Contains(doc, "private note") {
		t.Error("contact card leaks notes")
	}
}

// TestDetailCardWidgets verifies get_company / get_contact embed a single-record
// Card, and get_offer embeds a Card carrying the full body so an offer reads in
// one view (the list projection drops the body).
func TestDetailCardWidgets(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	comp := callTool(t, c, ctx, "create_company", map[string]any{"name": "Globex", "website": "globex.io"})
	var company struct {
		ID uint64 `json:"id"`
	}
	decodeStructured(t, comp, &company)
	res := callTool(t, c, ctx, "get_company", map[string]any{"id": company.ID})
	if _, doc := embeddedHTML(t, res); !strings.Contains(doc, `"widget":"card"`) || !strings.Contains(doc, "Globex") {
		t.Error("get_company does not embed a card carrying the record")
	}

	con := callTool(t, c, ctx, "create_contact", map[string]any{"name": "Hank"})
	var contact struct {
		ID uint64 `json:"id"`
	}
	decodeStructured(t, con, &contact)
	res = callTool(t, c, ctx, "get_contact", map[string]any{"id": contact.ID})
	if _, doc := embeddedHTML(t, res); !strings.Contains(doc, `"widget":"card"`) || !strings.Contains(doc, "Hank") {
		t.Error("get_contact does not embed a card carrying the record")
	}

	lead := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Offer Lead"})
	var l struct {
		ID uint64 `json:"id"`
	}
	decodeStructured(t, lead, &l)
	body := "Dear customer,\nHere is our detailed proposal body."
	off := callTool(t, c, ctx, "create_offer", map[string]any{
		"lead_id": l.ID, "title": "Q3 Proposal", "subject": "Proposal", "body": body,
	})
	var o struct {
		ID uint64 `json:"id"`
	}
	decodeStructured(t, off, &o)
	res = callTool(t, c, ctx, "get_offer", map[string]any{"id": o.ID})
	_, doc := embeddedHTML(t, res)
	if !strings.Contains(doc, `"widget":"card"`) {
		t.Error("get_offer does not embed a card")
	}
	if !strings.Contains(doc, "detailed proposal body") {
		t.Error("get_offer card does not carry the full offer body")
	}
}

// TestPipelineSummaryEmbedsTables verifies the summary result carries the two
// read-only tables.
func TestPipelineSummaryEmbedsTables(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	callTool(t, c, ctx, "create_lead", map[string]any{"name": "Summary Lead"})
	res := callTool(t, c, ctx, "pipeline_summary", map[string]any{})
	if res.IsError {
		t.Fatalf("pipeline_summary: %s", resultText(t, res))
	}
	var widgets int
	for _, c := range res.Content[1:] {
		if _, ok := mcp.AsEmbeddedResource(c); ok {
			widgets++
		}
	}
	if widgets != 2 {
		t.Errorf("pipeline_summary embeds %d widgets, want 2 (deals by stage, leads by status)", widgets)
	}
}
