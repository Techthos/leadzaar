---
description: How to build the MCP server in internal/server using github.com/mark3labs/mcp-go — server construction, tool/resource/prompt registration, transports, middleware, conventions, and the interactive UI (gadget widget) requirement for CRUD tools.
paths: internal/server/**
---

# MCP Server — `mark3labs/mcp-go`

Code under `internal/server/` implements this project's [Model Context Protocol](https://modelcontextprotocol.io) server using **`github.com/mark3labs/mcp-go`**. Follow these conventions.

Two import paths only:

```go
import (
    "github.com/mark3labs/mcp-go/mcp"     // protocol types + tool/result builders
    "github.com/mark3labs/mcp-go/server"  // MCPServer, transports, options, middleware
)
```

## Construction

Build the server with `server.NewMCPServer(name, version, opts...)`. It is **transport-agnostic** — construction and registration never reference a transport. Enable only the capabilities the server actually uses, and always enable recovery so a panicking handler can't crash the process:

```go
func New(name, version string) *server.MCPServer {
    return server.NewMCPServer(name, version,
        server.WithToolCapabilities(true),       // we expose tools
        server.WithResourceCapabilities(true, true), // (subscribe, listChanged) — only if used
        server.WithPromptCapabilities(true),     // only if used
        server.WithRecovery(),                   // recover panics in handlers
        server.WithLogging(),
    )
}
```

Keep construction and registration in `internal/server`; keep transport selection and process lifecycle in `cmd/`. `main` stays thin.

## Tools

### Schema-based tools (simple args)

Define the tool with `mcp.NewTool` + `mcp.With*` option builders, then register with `s.AddTool(tool, handler)`. Mark required params with `mcp.Required()`; constrain with `mcp.Enum(...)`, defaults with `mcp.DefaultBool(...)`, etc.

```go
tool := mcp.NewTool("calculate",
    mcp.WithDescription("Perform basic arithmetic"),
    mcp.WithString("operation", mcp.Required(),
        mcp.Enum("add", "subtract", "multiply", "divide"),
        mcp.Description("The operation to perform")),
    mcp.WithNumber("x", mcp.Required(), mcp.Description("First number")),
    mcp.WithNumber("y", mcp.Required(), mcp.Description("Second number")),
)
s.AddTool(tool, handleCalculate)
```

Handler signature is `func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)`. Extract args with the typed accessors — `req.RequireString("operation")` / `req.RequireFloat("x")` / `req.RequireBool(...)` (return an error if missing/wrong type), or `req.GetString("k", default)` for optional values.

### Prefer typed handlers for non-trivial input

When a tool takes more than one or two args, define an input struct with `jsonschema` tags and use `mcp.WithInputSchema[T]()` / `mcp.WithOutputSchema[T]()`, then wrap the handler:

- `mcp.NewStructuredToolHandler(fn)` — `fn(ctx, req, args T) (R, error)`; input is validated and bound, output `R` is auto-serialized.
- `mcp.NewTypedToolHandler(fn)` — `fn(ctx, req, args T) (*mcp.CallToolResult, error)`; build the result yourself.

```go
type SearchRequest struct {
    Query      string   `json:"query" jsonschema:"required,description=Search text"`
    Categories []string `json:"categories"`
    Limit      int      `json:"limit" jsonschema:"description=Max results"`
}

searchTool := mcp.NewTool("search_products",
    mcp.WithDescription("Search the product catalog"),
    mcp.WithInputSchema[SearchRequest](),
    mcp.WithOutputSchema[SearchResponse](),
)
s.AddTool(searchTool, mcp.NewStructuredToolHandler(searchProductsHandler))

func searchProductsHandler(ctx context.Context, req mcp.CallToolRequest, args SearchRequest) (SearchResponse, error) {
    // args is already validated; return a typed value.
}
```

### Results & error semantics — important

Distinguish the two failure modes:

- **Tool-level / user-facing failures** (bad input, business-rule failure): return `mcp.NewToolResultError("message"), nil`. Return value, **`nil` error** — this surfaces to the model as an error result it can react to.
- **Protocol/transport failures** (something the model can't fix): return `nil, err`.

Build success results with: `mcp.NewToolResultText(...)`, `mcp.NewToolResultJSON(v)`, or `mcp.NewToolResultStructured(v, fallbackText)` (structured output + plain-text fallback for older clients).

## Interactive UI for CRUD tools — MCP Apps widgets

Every CRUD tool (create/read/update/delete over a domain entity) also ships an **interactive UI
version** — a widget built with `github.com/techthos/gadget` — not just a text/JSON result.
**Invoke the `gadget-mcp-ui` skill before writing any widget code**; the skill and its
`reference.md` are the source of truth for the gadget API — do not restate or improvise it here.

Widgets follow the **MCP Apps extension** ([`io.modelcontextprotocol/ui`](https://modelcontextprotocol.io/extensions/apps/overview),
spec version `2026-01-26`): a self-contained HTML document tagged with the **MCP Apps HTML profile**
(`text/html;profile=mcp-app`) that the host renders in a sandboxed iframe and drives through the
standard **App Bridge** (`@modelcontextprotocol/ext-apps`) over `postMessage`. Do **not** invent a
custom UI-event channel and do **not** inject chat prompts (no `postMessage`-a-prompt fallback):
interactions flow back only as the standard MCP Apps JSON-RPC methods below.

### Discovery — the tool MUST carry `_meta.ui.resourceUri`

**A host discovers an MCP App by scanning tool definitions for `_meta.ui.resourceUri`, not by
inspecting tool results.** A tool whose *definition* lacks that meta is invisible as an app no matter
what its result embeds — the MCP Apps inspector reports *"No MCP Apps available. Apps are tools that
include a `_meta.ui.resourceUri`"*. A widget that lives only as an embedded per-call resource in the
result is therefore never discovered. Register widgets the **spec-canonical** way:

1. **Register one stable `ui://` template resource per widget**, rendered once and served from
   memory. Use gadget's `Descriptor()` for the URI/title/mimeType and `Document()` for the HTML:
   ```go
   d := w.Descriptor()      // d.URI, d.Title, d.MIMEType == "text/html;profile=mcp-app"
   doc, _ := w.Document()   // rendered once (validates); returned verbatim by resources/read
   s.AddResource(mcp.NewResource(d.URI, d.Title, mcp.WithMIMEType(d.MIMEType)), serveDoc(doc))
   ```
   The URI is **stable and one per widget kind** (`ui://binzaar/catalog`, `ui://binzaar/installed`,
   `ui://binzaar/config`) — **never** a per-render `unixnano` URI, because both the resource and the
   tool link point at it.
2. **Link every tool that renders the widget by setting its `_meta.ui.resourceUri`** to that same
   stable URI. gadget hands you the exact meta via `ToolMeta()` (`{"ui":{"resourceUri": d.URI}}`);
   attach it before registering (mark3labs/mcp-go: `mcp.Tool.Meta` marshals to `_meta`):
   ```go
   tool := mcp.NewTool("list_catalog", mcp.WithDescription("..."))
   tool.Meta = mcp.NewMetaFromMap(w.ToolMeta())
   s.AddTool(tool, h.listCatalog)
   ```
   Advertise the `io.modelcontextprotocol/ui` extension in the server's capabilities where the SDK
   exposes it, so the host negotiates the App Bridge.

The host fetches the template with `resources/read`, renders it in the iframe, then hydrates it from
the tool result: the **document** comes from the resource, the **data** flows through
`structuredContent` — never bake the data into the registered HTML.

- **Table** for list/read output, **Form** for create/update input (prefill via the form's
  `PrefillKey`, inline field errors under `ErrorsKey` keyed by field name).
- **The result carries the data; the resource carries the document.** Build results with
  `mcp.NewToolResultStructured(payload, "Installed owner/name v1.2.3")`: `payload` lands in
  `structuredContent` under the widget's key (a table's `RowsKey`, a form's `PrefillKey`), and the
  short sentence is the human status line the runtime shows as the banner. **Read/list tools use the
  same status-line form** (e.g. `mcp.NewToolResultStructured(catalogOutput{Apps: rows}, "5 apps in
  the catalog.")`) — a raw-JSON text block flashes in the host before the widget paints. Reserve
  `mcp.NewToolResultJSON` for tools with **no** widget (e.g. `app_details`, `list_releases`), where
  the JSON text block is the only output.
- Actions and submits target the **normal model-visible tools**; a widget click/submit dispatches a
  standard **`tools/call`** over the App Bridge that the host runs directly against this server
  (links use **`ui/open-link`**, iframe height is applied via **`ui/notifications/size-changed`**).
- **In-place refresh is the standard tool-result push — not a chat prompt, not a static snapshot.**
  When a widget's `tools/call` completes, the host sends that result back to the same widget via
  **`ui/notifications/tool-result`**, and the gadget runtime re-renders the widget from the result's
  `structuredContent`: a table repaints its rows when `structuredContent` carries that table's
  **`RowsKey`** (the fresh rows array), a form re-applies fields from its **`PrefillKey`** (and
  inline errors from its `ErrorsKey`). So a mutating tool must return the **refreshed collection
  under the target widget's key** (e.g. an install action surfaced on a `RowsKey: "apps"` catalog
  table returns `{"apps": <refreshed rows>}`), or the visible widget goes stale even though the tool
  succeeded.
- **Set a `LoadTool` on every widget so a fresh mount hydrates.** The registered template ships no
  baked data, and the host may reload or remount the iframe (a new turn, a message-list re-render)
  before any result arrives. A widget's `LoadTool` is a read tool the gadget runtime calls once on
  load (after the App Bridge handshake) to fetch current data — the catalog table loads
  `list_catalog`, the installed table `list_installed`, the config form `get_config`. The load tool
  must return its data under the widget's key: a table's `RowsKey` (a plain list tool already fits),
  a form's `PrefillKey` (so the form's read tool must return that key — e.g. `get_config` returns
  `values`, not only `config`).
- **Destructive actions (delete)**: table row actions with `Action.Confirm` — the sandboxed
  iframe has no native `confirm()`/`alert()`.
- **Optional fallback — embed the per-call document too.** For hosts that render a result-embedded
  widget rather than the registered template, you *may also* append the freshly rendered `Document()`
  to the result's `content` **after** the text block
  (`mcp.NewEmbeddedResource(mcp.TextResourceContents{URI: d.URI, MIMEType: "text/html;profile=mcp-app", Text: doc})`).
  This is a supplement, **not** the discovery mechanism — the tool's `_meta.ui.resourceUri` is what
  makes the app visible. The text + `structuredContent` result must always stand alone; widget
  build/render failures are logged to stderr and never fail the tool.
- New or changed widgets and tools are product-surface changes → update `docs/SPECIFICATIONS.md`
  in the **same commit** (`specification-rules.md`).

Configure widget actions declaratively through gadget's `Action`/`Submit` API (which name the
target tool and its argument sources) — the gadget runtime speaks the MCP Apps App Bridge protocol
for you. Never hand-author postMessage payloads or text-sentinel envelopes in widget HTML. The full
host-side contract (method surface, rendering, refresh loop) is documented in
`docs/mcp-apps-host-guide.md`.

## Resources

Register with `s.AddResource(resource, handler)`. Use URI templates for parameterized resources:

```go
s.AddResource(
    mcp.NewResource("file://{path}", "File Content",
        mcp.WithResourceDescription("Read file contents"),
        mcp.WithMIMEType("text/plain")),
    handleFileContent,
)

func handleFileContent(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
    // return []mcp.ResourceContents{ mcp.TextResourceContents{URI, MIMEType, Text} }
}
```

Validate and sanitize any path/URI input (clean the path, reject `..` traversal, confine to an allowed base dir) before touching the filesystem.

## Prompts

Register with `s.AddPrompt(prompt, handler)` using `mcp.NewPrompt(...)` and return `mcp.NewGetPromptResult(...)` from the handler.

## Transports

The server object is shared across transports. Select transport in `cmd/` (e.g. via an `MCP_TRANSPORT` env var), not in `internal/server`:

- **stdio** (default for local/CLI use): `server.ServeStdio(s)`. Never write logs to **stdout** on stdio — that stream is the protocol channel. Log to **stderr**.
- **Streamable HTTP** (preferred network transport): `server.NewStreamableHTTPServer(s, opts...).Start(":8080")`. Options: `server.WithEndpointPath("/mcp")`, `server.WithHeartbeatInterval(30*time.Second)`, `server.WithStateLess(bool)`, `server.WithSessionIdleTTL(...)`.
- **SSE** (legacy): `server.NewSSEServer(s).Start(":8080")`. Prefer Streamable HTTP for new work.
- **In-process** (`client.NewInProcessClient(s)`): use this in tests instead of spawning a subprocess.

## Middleware & context

- Cross-cutting concerns (auth, rate limiting, caching, metrics) go in middleware, not in handlers: `s.AddToolMiddleware(func(next server.ToolHandler) server.ToolHandler { ... })` and `s.AddResourceMiddleware(...)`. Each wraps `next` and may short-circuit by returning early.
- Inside a handler, reach the server via `server.ServerFromContext(ctx)`; push async updates with `mcpServer.SendNotificationToClient(ctx, "event", payload)`.
- Always thread the incoming `ctx` through downstream calls (DB, HTTP) for cancellation.

## Testing

Test handlers through the in-process client (`client.NewInProcessClient(s)`) so the full registration + (de)serialization path is exercised without a transport. Initialize, then `CallTool`, then assert on `result.Content` (extract text via `mcp.AsTextContent(result.Content[0])`).

## Checklist for new functionality

1. One tool/resource/prompt per file (or a small cohesive group) under `internal/server`.
2. Always set `mcp.WithDescription` and describe every parameter — the model relies on these.
3. Use typed handlers + `jsonschema`-tagged structs once input is non-trivial.
4. User/input errors → `NewToolResultError(...), nil`; infrastructure errors → `nil, err`.
5. Enable the matching capability in `NewMCPServer` and confirm `WithRecovery()` is set.
6. Add an in-process client test.
7. CRUD tool? Ship its gadget widget UI via the `gadget-mcp-ui` skill: register a **stable `ui://`
   template resource** and set the rendering tool's **`_meta.ui.resourceUri`** to it (this is what
   makes the app discoverable — a tool without it never appears in an MCP Apps host), with a
   `LoadTool` and actions targeting the model-visible tools; update `docs/SPECIFICATIONS.md` in the
   same commit.
8. Widget interactions flow through the standard MCP Apps App Bridge (`tools/call`, `ui/open-link`,
   `ui/notifications/size-changed`) — configure them via gadget's `Action`/`Submit` API, never a
   hand-authored postMessage payload or text-sentinel envelope.
