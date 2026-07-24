// Package server implements the Leadzaar MCP server using mark3labs/mcp-go.
// It is transport-agnostic: construction and registration live here, transport
// selection lives in main. Handlers consume the db.Store and never touch bbolt
// directly. See docs/SPECIFICATIONS.md (MCP Surface) and
// .claude/rules/mcp-server.md.
package server

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/gadget/uispec"
	"github.com/techthos/leadzaar/internal/db"
)

// handlers bundles the dependencies shared by every tool/resource/prompt handler.
type handlers struct {
	store *db.Store
}

// New builds the MCP server, enabling exactly the capabilities used (tools,
// resources, prompts) plus panic recovery and logging, and registers the full
// surface.
func New(store *db.Store, version string) *server.MCPServer {
	s := server.NewMCPServer(
		"leadzaar", version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(false, false),
		server.WithPromptCapabilities(false),
		server.WithRecovery(),
		server.WithLogging(),
		// Advertise the MCP Apps extension so hosts know the ui:// template
		// resources are renderable widgets. mark3labs/mcp-go exposes only
		// `capabilities.experimental` (not the spec's `capabilities.extensions`),
		// so we advertise under experimental as the closest available slot; the
		// per-tool `_meta.ui.resourceUri` link is what picker hosts key on.
		server.WithExperimental(map[string]any{
			uispec.ExtensionID: map[string]any{"mimeTypes": []string{uispec.MIMEType}},
		}),
	)
	h := &handlers{store: store}
	h.registerLeadTools(s)
	h.registerContactTools(s)
	h.registerDealTools(s)
	h.registerCompanyTools(s)
	h.registerOfferTools(s)
	h.registerSummaryTool(s)
	h.registerResources(s)
	registerAppResources(s)
	h.registerPrompts(s)
	return s
}

// okResult builds a widget-bearing tool's success result: payload becomes
// structuredContent (what the model reads, and what the gadget runtime keys the
// in-place refresh off of), while status is the short human sentence shown as the
// text/status block. Widget-bearing results must never put raw JSON in the text
// block — an MCP Apps host flashes it before the widget paints over it (see
// .claude/rules/mcp-server.md). NewToolResultStructured cannot fail, so unlike
// the old jsonResult this returns a single value.
func okResult(payload any, status string) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(payload, status)
}

// formErrorResult builds a validation-failure result for a form-bearing tool.
// The field errors ride in structuredContent under the form's ErrorsKey
// ("errors") so an in-place MCP Apps form marks the failed fields, IsError flags
// the failure for the model, and msg is the text block. The caller still embeds
// the retry form (baked values + errors) as a fallback widget.
func formErrorResult(errs map[string]string, msg string) *mcp.CallToolResult {
	res := mcp.NewToolResultStructured(map[string]any{"errors": errs}, msg)
	res.IsError = true
	return res
}

// pageResult wraps a page of list items under a named key alongside the
// pagination metadata every list_* tool returns. key must equal the embedded
// table's RowsKey so its LoadTool re-hydrates from this same shape. items is
// normalized to an empty array for a stable shape (the MCP spec requires
// structuredContent to be a JSON object, so a list tool never returns a bare
// array). status is the human status line for the text block.
func pageResult[T any](key, status string, items []T, page, pageSize, total, totalPages int, hasMore bool) *mcp.CallToolResult {
	if items == nil {
		items = []T{}
	}
	return mcp.NewToolResultStructured(map[string]any{
		key:           items,
		"page":        page,
		"page_size":   pageSize,
		"total":       total,
		"total_pages": totalPages,
		"has_more":    hasMore,
	}, status)
}

// listStatus renders a list tool's status line, e.g. "3 leads." / "1 lead.".
func listStatus(total int, singular, plural string) string {
	noun := plural
	if total == 1 {
		noun = singular
	}
	return fmt.Sprintf("%d %s.", total, noun)
}

// toolErr converts a store/business error into a user-facing tool-error result
// (value, nil error) so the model can react to it rather than the call failing.
func toolErr(err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(err.Error()), nil
}

// setIf overlays a patch pointer onto dst when the caller actually supplied it
// (patch != nil), leaving dst untouched otherwise. It is the primitive behind
// the update_* tools' partial-update (PATCH) semantics: a field the caller omits
// keeps its stored value instead of being reset to the zero value. The DB layer's
// UpdateX does a full-record replace, so the handler must load the existing
// record and setIf every editable field before saving.
func setIf[T any](dst *T, patch *T) {
	if patch != nil {
		*dst = *patch
	}
}

// parseIDFromURI extracts the trailing numeric ID from a resource URI of the
// form prefix + "<id>".
func parseIDFromURI(uri, prefix string) (uint64, error) {
	rest := strings.TrimPrefix(uri, prefix)
	id, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id in %q: %w", uri, err)
	}
	return id, nil
}
