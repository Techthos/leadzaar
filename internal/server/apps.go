package server

import (
	"context"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/gadget"
	"github.com/techthos/gadget/uispec"
)

// Spec-canonical linked-template mode (see .claude/rules/mcp-server.md and
// docs/SPECIFICATIONS.md "Interactive widget UI"). Alongside the per-call
// embedded documents (widgets.go) that inline-rendering hosts display, this
// registers a stable `ui://` template resource per browse view and links the
// list tools (and pipeline_summary) to it via `_meta.ui.resourceUri`. A
// picker-style MCP Apps host discovers apps by that field, reads the template
// with `resources/read`, and renders it from the tool result's structuredContent
// (rows under the table's RowsKey) — which those tools already return. Row
// actions then flow back through the App Bridge exactly as in embedded mode.
//
// Only the list tools and pipeline_summary carry the link: they are the natural
// "get started" entry points and their results render standalone. The
// create/update/get/delete/convert tools keep embedded-per-call widgets (forms,
// detail rows, refreshed tables) and are not surfaced as separate apps.

// Stable app-template URIs. Distinct from the per-call render URIs
// (`ui://leadzaar/<kind>/<unixnano>`): these have no trailing render segment.
const (
	appLeads     = "ui://leadzaar/leads"
	appContacts  = "ui://leadzaar/contacts"
	appDeals     = "ui://leadzaar/deals"
	appCompanies = "ui://leadzaar/companies"
	appOffers    = "ui://leadzaar/offers"
	appPipeline  = "ui://leadzaar/pipeline-deals"
)

// appTemplates builds the stable-URI, data-less template widgets to register.
// Each reuses its per-call widget's config (columns/card template, actions,
// LoadTool) so the linked and embedded renders stay identical; only the URI is
// fixed and the baked snapshot dropped (canonical hosts feed data via
// structuredContent). Companies and Contacts browse as card lists; the rest
// remain tables.
func appTemplates() []gadget.Widget {
	stableTable := func(t *gadget.Table, uri string) *gadget.Table {
		t.URI = uri
		t.InitialData = nil
		return t
	}
	stableCards := func(l *gadget.CardList, uri string) *gadget.CardList {
		l.URI = uri
		l.InitialData = nil
		return l
	}
	return []gadget.Widget{
		stableTable(leadsTable("Leads", nil), appLeads),
		stableCards(contactsCardList("Contacts", nil), appContacts),
		stableTable(dealsTable("Deals", nil), appDeals),
		stableCards(companiesCardList("Companies", nil), appCompanies),
		stableTable(offersTable("Offers", nil), appOffers),
		stableTable(summaryDealsTable(nil), appPipeline),
	}
}

// registerAppResources renders each template once and serves it as a `ui://`
// resource. A render failure is logged to stderr and skipped — it never blocks
// startup, and the embedded-per-call path is unaffected.
func registerAppResources(s *server.MCPServer) {
	for _, w := range appTemplates() {
		d := w.Descriptor()
		doc, err := w.Document()
		if err != nil {
			log.Printf("app template %s: render: %v", d.URI, err)
			continue
		}
		contents := []mcp.ResourceContents{mcp.TextResourceContents{
			URI: d.URI, MIMEType: d.MIMEType, Text: doc,
		}}
		s.AddResource(
			mcp.NewResource(d.URI, d.Name, mcp.WithMIMEType(d.MIMEType)),
			func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return contents, nil
			},
		)
	}
}

// uiToolMeta builds a tool's `_meta` linking it to its `ui://` template resource
// (`{"ui": {"resourceUri": uri}}`), so hosts surface the tool as an MCP App.
func uiToolMeta(resourceURI string) *mcp.Meta {
	return mcp.NewMetaFromMap(uispec.ToolUIMeta{ResourceURI: resourceURI}.MetaMap())
}
