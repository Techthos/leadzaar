package server_test

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// resourceURIFromToolMeta extracts _meta.ui.resourceUri from a tool, or "".
func resourceURIFromToolMeta(tool mcp.Tool) string {
	if tool.Meta == nil {
		return ""
	}
	ui, ok := tool.Meta.AdditionalFields["ui"].(map[string]any)
	if !ok {
		return ""
	}
	uri, _ := ui["resourceUri"].(string)
	return uri
}

// TestToolsAdvertiseAppTemplates verifies the browse tools link a ui:// template
// via _meta.ui.resourceUri so picker-style MCP Apps hosts list them as apps, and
// that non-app tools carry no such link.
func TestToolsAdvertiseAppTemplates(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}

	want := map[string]string{
		"list_leads":       "ui://leadzaar/leads",
		"list_contacts":    "ui://leadzaar/contacts",
		"list_deals":       "ui://leadzaar/deals",
		"list_companies":   "ui://leadzaar/companies",
		"list_offers":      "ui://leadzaar/offers",
		"pipeline_summary": "ui://leadzaar/pipeline-deals",
	}
	apps := 0
	for _, tool := range res.Tools {
		uri := resourceURIFromToolMeta(tool)
		if uri == "" {
			continue
		}
		apps++
		if want[tool.Name] != uri {
			t.Errorf("%s links resourceUri %q, want %q", tool.Name, uri, want[tool.Name])
		}
	}
	if apps != len(want) {
		t.Errorf("%d tools advertise an app template, want %d", apps, len(want))
	}

	// A mutation/detail tool must not be surfaced as an app.
	if uri := resourceURIFromToolMeta(byName["delete_lead"]); uri != "" {
		t.Errorf("delete_lead unexpectedly links resourceUri %q", uri)
	}
}

// TestAppTemplateResourcesReadable verifies each linked ui:// template resolves
// via resources/read and returns an MCP Apps document.
func TestAppTemplateResourcesReadable(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	for _, uri := range []string{
		"ui://leadzaar/leads", "ui://leadzaar/contacts", "ui://leadzaar/deals",
		"ui://leadzaar/companies", "ui://leadzaar/offers", "ui://leadzaar/pipeline-deals",
	} {
		req := mcp.ReadResourceRequest{}
		req.Params.URI = uri
		out, err := c.ReadResource(ctx, req)
		if err != nil {
			t.Fatalf("ReadResource(%s): %v", uri, err)
		}
		trc, ok := out.Contents[0].(mcp.TextResourceContents)
		if !ok {
			t.Fatalf("%s content is %T, want TextResourceContents", uri, out.Contents[0])
		}
		if trc.MIMEType != "text/html;profile=mcp-app" {
			t.Errorf("%s MIME = %q, want text/html;profile=mcp-app", uri, trc.MIMEType)
		}
		if !strings.Contains(strings.ToLower(trc.Text), "<!doctype html>") {
			t.Errorf("%s is not a full HTML document", uri)
		}
	}
}
