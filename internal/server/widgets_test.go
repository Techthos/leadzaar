package server_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// embeddedHTML returns the URI and document of the first embedded text/html
// resource in a tool result, failing the test when none is present. The JSON
// text block must always come first — widgets ride after it.
func embeddedHTML(t *testing.T, res *mcp.CallToolResult) (uri, doc string) {
	t.Helper()
	if len(res.Content) < 2 {
		t.Fatalf("result has %d content blocks, want JSON text + embedded widget", len(res.Content))
	}
	if _, ok := mcp.AsTextContent(res.Content[0]); !ok {
		t.Fatalf("first content block is %T, want text (JSON must stand alone)", res.Content[0])
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
		if trc.MIMEType != "text/html" {
			t.Fatalf("embedded resource MIME type = %q, want text/html", trc.MIMEType)
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
	if err := json.Unmarshal([]byte(resultText(t, res)), &lead); err != nil {
		t.Fatalf("create_lead JSON does not stand alone: %v", err)
	}

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
