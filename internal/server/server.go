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
	)
	h := &handlers{store: store}
	h.registerLeadTools(s)
	h.registerContactTools(s)
	h.registerDealTools(s)
	h.registerCompanyTools(s)
	h.registerOfferTools(s)
	h.registerSummaryTool(s)
	h.registerResources(s)
	h.registerPrompts(s)
	return s
}

// jsonResult wraps a value as a tool success result (JSON text + structured
// content). A marshal failure is a protocol error (nil result, error).
func jsonResult(v any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultJSON(v)
}

// pageResult wraps a page of list items under a named key alongside the
// pagination metadata every list_* tool returns. items is normalized to an empty
// array for a stable shape (the MCP spec requires structuredContent to be a JSON
// object, so a list tool never returns a bare array).
func pageResult[T any](key string, items []T, page, pageSize, total, totalPages int, hasMore bool) (*mcp.CallToolResult, error) {
	if items == nil {
		items = []T{}
	}
	return mcp.NewToolResultJSON(map[string]any{
		key:           items,
		"page":        page,
		"page_size":   pageSize,
		"total":       total,
		"total_pages": totalPages,
		"has_more":    hasMore,
	})
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
