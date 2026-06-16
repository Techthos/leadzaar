// Package server implements the microapp-crm MCP server using mark3labs/mcp-go.
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
	"github.com/techthos/microapp-crm/internal/db"
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
		"microapp-crm", version,
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

// listResult wraps a slice under a named key. The MCP spec requires a result's
// structuredContent to be a JSON object, so list tools must not return a bare
// array. A nil slice is normalized to an empty array for a stable shape.
func listResult[T any](key string, items []T) (*mcp.CallToolResult, error) {
	if items == nil {
		items = []T{}
	}
	return mcp.NewToolResultJSON(map[string]any{key: items})
}

// toolErr converts a store/business error into a user-facing tool-error result
// (value, nil error) so the model can react to it rather than the call failing.
func toolErr(err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(err.Error()), nil
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
