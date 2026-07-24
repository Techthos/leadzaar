# gadget API reference

> Generated from gadget source at commit `2cd09d6` (2026-07-23, pre-release). Regenerate
> from the installed module's `AGENTS.md` when the version changes — see "Updating this
> skill" in SKILL.md.

- **Module**: `github.com/techthos/gadget` (Go >= 1.25), MIT, pre-release (APIs unstable).
- **Spec target**: MCP Apps extension `io.modelcontextprotocol/ui`, version `2026-01-26`.
  Widgets are fully self-contained HTML documents (inline CSS + JS) served as `ui://`
  template resources; hosts render them in a sandboxed iframe.

## Package map

| Package | Import path | Role |
|---|---|---|
| `gadget` | `github.com/techthos/gadget` | `Table`, `Form`, `Action`, columns, fields, `RowsOf` |
| `theme` | `github.com/techthos/gadget/theme` | `Theme` struct → CSS design-token overrides |
| `uispec` | `github.com/techthos/gadget/uispec` | Spec constants and `_meta` types (zero deps) |
| `gosdk` | `github.com/techthos/gadget/gosdk` | Adapter for the official `modelcontextprotocol/go-sdk` — the only package importing an MCP SDK |

## The `Widget` interface

Both `*Table` and `*Form` implement:

```go
type Widget interface {
    Document() (string, error)              // complete self-contained HTML document (calls Validate first)
    Descriptor() uispec.ResourceDescriptor  // registration data for the ui:// template resource
    ToolMeta() map[string]any               // tool _meta linking tool → widget: {"ui": {"resourceUri": ...}}
    Validate() error
}
```

With `gosdk` you rarely call these yourself; they exist for manual wiring.

## Shared types

```go
type Align string        // AlignStart | AlignCenter | AlignEnd
type SortSpec struct {   // default sort order
    Key  string `json:"key"`
    Desc bool   `json:"desc,omitempty"`
}
type EmptyState struct { // no-data message (Table.Empty)
    Title string `json:"title,omitempty"` // defaults to "No data"
    Body  string `json:"body,omitempty"`
}
```

## `Table`

```go
type Table struct {
    URI     string      // REQUIRED. ui:// resource URI, e.g. "ui://myapp/users"
    Title   string      // toolbar heading + document title
    Columns []Column    // REQUIRED, non-empty

    RowsKey string      // structuredContent key holding the rows array. Default "rows"
    RowID   string      // row field uniquely identifying a row. Default "id"

    PageSize    int              // > 0 enables client-side pagination; 0 disables; < 0 invalid
    DefaultSort *SortSpec        // pre-sort on load (Key required when set)
    Filterable  bool             // client-side text filter box
    Selection   *SelectionConfig // row checkboxes + bulk actions
    Empty       EmptyState

    InitialData map[string]any         // optional structuredContent-shaped snapshot baked into the document
    LoadTool    string                 // read tool the runtime calls once on load to re-fetch rows under RowsKey, replacing the baked snapshot (a reloaded widget shows current data, not the render-time snapshot)
    LoadArgs    map[string]any         // optional static args passed to LoadTool
    Theme       *theme.Theme
    UI          *uispec.ResourceUIMeta // overrides resource _meta.ui (CSP, permissions, prefersBorder)
}

type SelectionConfig struct {
    Bulk []Action // toolbar actions while rows are selected; FromSelection resolves across all selected rows
}
```

Rows are string-keyed JSON objects (`[]map[string]any`) delivered at runtime under `RowsKey`.

## `Column`

```go
type Column struct {
    Key      string                  // row field displayed (REQUIRED except ColActions)
    Label    string
    Type     ColumnType              // defaults to ColText
    Sortable *bool                   // overrides default sortability
    Align    Align
    Format   string                  // see formats below
    Badge    map[string]BadgeVariant // ColBadge: cell value -> variant
    Link     *LinkSpec               // ColLink config
    Actions  []Action                // ColActions: per-row buttons
    Width    string                  // CSS width, e.g. "12rem", "20%"
}
```

- **Column types**: `ColText` (default), `ColNumber`, `ColDate`, `ColBadge`, `ColLink`, `ColActions`.
- **Formats** (rendered via `Intl`, host locale/time zone):
  - number: `"int"`, `"decimal:<digits>"`, `"percent"`, `"currency:<code>"` (e.g. `"currency:EUR"`)
  - date: `"date"`, `"datetime"`, `"time"`, `"relative"`
- **Sortability defaults**: text/number/date sortable; badge/link/actions not. Override with `Sortable: &b`.
- **Badge variants**: `BadgeNeutral`, `BadgeInfo`, `BadgeSuccess`, `BadgeWarning`, `BadgeDanger`.

```go
type LinkSpec struct {
    HrefKey string `json:"hrefKey"`           // REQUIRED: row field holding the URL
    TextKey string `json:"textKey,omitempty"` // row field holding link text …
    Text    string `json:"text,omitempty"`    // … or fixed text; else the URL is shown
}
```

**Constructors** (sugar — struct literals also fine):

```go
gadget.Text(key, label)
gadget.Number(key, label, format...)   // right-aligned (AlignEnd)
gadget.Date(key, label, format...)
gadget.Badge(key, label, variants)
gadget.Link(hrefKey, label)            // Key and Link.HrefKey both set to hrefKey
gadget.ActionsColumn(actions...)       // per-row actions column, empty Label
```

## `Form`

```go
type Form struct {
    URI    string      // REQUIRED. ui:// resource URI
    Title  string
    Fields []Field     // REQUIRED, non-empty
    Submit SubmitSpec  // REQUIRED (Submit.Tool must be set)
    Cancel *CancelSpec // when set, adds a reset button

    PrefillKey string  // structuredContent key with {"field": value} prefill. Default "values"
    ErrorsKey  string  // structuredContent key with {"field": "message"} errors. Default "errors"

    InitialData map[string]any // e.g. {"values": {...}} for a pre-filled edit form
    LoadTool    string         // read tool the runtime calls once on load to re-fetch prefill under PrefillKey, replacing the baked snapshot
    LoadArgs    map[string]any // optional static args passed to LoadTool
    Theme       *theme.Theme
    UI          *uispec.ResourceUIMeta
}

type SubmitSpec struct {
    Tool           string         // REQUIRED: MCP tool called with {field: value, ...} merged over StaticArgs
    Label          string         // default "Submit"
    StaticArgs     map[string]any // fixed args merged UNDER field values (field values win)
    SuccessMessage string         // shown after successful submit
}

type CancelSpec struct {
    Label string // default "Cancel"
}
```

## `Field`

```go
type Field struct {
    Name        string      // REQUIRED, unique: the tool-call argument name
    Label       string      // defaults to Name
    Description string      // help text under the control
    Placeholder string
    Type        FieldType   // defaults to FText
    Required    bool
    Default     any         // string-like for most; bool for FCheckbox; []string (or string) for FMultiSelect
    Options     []Option    // REQUIRED for FSelect / FMultiSelect
    Validation  *Validation // client-side constraints
    Rows        int         // textarea height (FTextarea), default 3
}
```

- **Field types**: `FText` (default), `FTextarea`, `FNumber`, `FCheckbox`, `FSelect`,
  `FMultiSelect`, `FDate`, `FTime`, `FHidden`, `FReadonly`.

```go
type Option struct{ Value, Label string }
gadget.Opt("admin") // Option{Value: "admin", Label: "admin"}
```

**Client-side validation** — native HTML attributes, enforced before submit; `Message`
overrides the browser's text:

```go
type Validation struct {
    Pattern string   // HTML pattern-attribute regex
    Min     *float64 // number/date/time constraints
    Max     *float64
    Step    *float64
    MinLen  *int     // text length constraints
    MaxLen  *int
    Message string
}
```

**Submitted value types** (what your submit tool receives):

| Field type | Submitted as |
|---|---|
| `FCheckbox` | `bool` |
| `FNumber` | number (**omitted entirely when empty**) |
| `FMultiSelect` | `[]string` |
| everything else — incl. `FHidden`, `FReadonly` | `string` (parse server-side, e.g. `strconv.Atoi("3")`) |

## `Action`

Per-row button (`ActionsColumn`), bulk action (`SelectionConfig.Bulk`), or link.

```go
type Action struct {
    Label   string               // REQUIRED
    Kind    ActionKind           // ActionTool (default) | ActionLink
    Tool    string               // MCP tool name (REQUIRED for ActionTool)
    Args    map[string]ArgSource // tool argument name -> value source
    HrefKey string               // row field holding URL (REQUIRED for ActionLink; opens via ui/open-link)
    Confirm string               // inline two-phase confirmation text before firing
    Variant ActionVariant        // VariantDefault ("") | VariantPrimary | VariantDanger
}
```

**Argument sources** (`ArgSource` is opaque — construct ONLY with these):

```go
gadget.Static(v)              // fixed value
gadget.FromRow("field")       // field on the row the action was triggered on
gadget.FromSelection("field") // field across ALL selected rows — bulk actions ONLY
```

`FromSelection` in a per-row action is a validation error; an `ArgSource` built any other
way fails validation/marshaling.

**Behavior contract**:

- `Confirm` renders an inline two-phase button (native `confirm()` is silently disabled
  in sandboxed iframes — never rely on it).
- If a tool called by a table action returns `structuredContent` containing `RowsKey`,
  the table **re-renders with those rows and clears the selection**. Mutating tools
  (delete/archive/…) should therefore return the updated full row list.

## `RowsOf`

```go
func RowsOf(slice any) ([]map[string]any, error)
```

Converts a typed slice (`[]User`, `[]*User`) into row maps via `encoding/json`, honoring
`json` struct tags (column `Key`s must match the tag names). Errors if the value doesn't
marshal to a JSON array of objects.

```go
rows, err := gadget.RowsOf(users)
table.InitialData = map[string]any{"rows": rows}
```

## Validation rules (what `Validate()` / `Document()` reject)

Table: well-formed `ui://` URI with non-empty path; >= 1 column; no duplicate column
`Key`s; `Key` required for text/number/date/badge; `Link.HrefKey` required for links;
actions columns need >= 1 action; `PageSize >= 0`; `DefaultSort.Key` required when set;
action `Label` required, `Tool` for tool kind, `HrefKey` for link kind, all `Args` built
with constructors, `FromSelection` only in bulk; `Theme` must pass `theme.Validate()`.

Form: URI as above; >= 1 field; `Submit.Tool` required; field `Name` required and unique;
`FSelect`/`FMultiSelect` require non-empty `Options`; `Theme` valid.

`Document()` calls `Validate()` first, so with `gosdk` registration you get
configuration errors at startup, not render time.

## The runtime data contract (structuredContent keys)

| Widget | Key (configurable via) | Shape | Meaning |
|---|---|---|---|
| Table | `rows` (`RowsKey`) | `[]object` | rows to render |
| Form | `values` (`PrefillKey`) | `{field: value}` | prefill (edit flows) |
| Form | `errors` (`ErrorsKey`) | `{field: "message"}` | inline server-side field errors; marks the submit failed |

With go-sdk typed handlers, the `Out` struct's JSON form becomes `structuredContent`:

```go
type rowsOut struct { Rows []map[string]any `json:"rows"` }
type editOut struct { Values map[string]any `json:"values"` }
type saveOut struct { Errors map[string]string `json:"errors,omitempty"` }
```

Flows:

- **List**: model calls `list_users` → `{"rows": [...]}` → table renders.
- **Row/bulk mutation**: widget calls `delete_user` (app-only) → return
  `{"rows": [...updated...]}` → table re-renders, selection cleared.
- **Edit form**: model calls `edit_user` (linked to the Form) → `{"values": {...}}` → prefill.
- **Submit**: widget calls `Submit.Tool` with `{field: value, ...}` merged over
  `StaticArgs` → return `{"errors": {...}}` to fail inline, or an errors-free result to
  succeed (shows `SuccessMessage` if set).

## Package `gosdk` — official go-sdk adapter

Requires `go get github.com/modelcontextprotocol/go-sdk`.

```go
import "github.com/techthos/gadget/gosdk"

// Declares the MCP Apps extension in server capabilities. Mutates and returns opts
// (nil allocates fresh) so it composes with mcp.NewServer. NOTE: explicitly setting
// Capabilities disables the SDK's historical default of advertising {"logging":{}}.
func EnableUI(opts *mcp.ServerOptions) *mcp.ServerOptions

// Registers w's template as a ui:// resource. Rendered ONCE, served from memory.
// Idempotent per (server, URI). Returns render/validation errors.
func AddWidget(s *mcp.Server, w gadget.Widget) error

// Registers tool t linked to w via _meta (registers w's resource first if needed).
func AddWidgetTool(s *mcp.Server, w gadget.Widget, t *mcp.Tool, h mcp.ToolHandler) error

// Typed variant: In/Out JSON schemas inferred; Out's JSON form becomes structuredContent.
func AddWidgetToolFor[In, Out any](s *mcp.Server, w gadget.Widget, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) error

// Marks t app-only (_meta.ui.visibility: ["app"]): callable from the widget UI, hidden
// from the model. Call BEFORE registering the tool. Use for row-action and submit tools.
func AppOnly(t *mcp.Tool, w gadget.Widget)

// Merges data into the result's _meta — delivered to the widget, hidden from the model.
func WithAppData(res *mcp.CallToolResult, data map[string]any)

// Whether the session's client declared the MCP Apps extension. Branching is optional —
// attaching _meta.ui unconditionally is spec-legal (hosts ignore unknown metadata).
func ClientSupportsUI(ss *mcp.ServerSession) bool
```

**Canonical wiring pattern**:

```go
server := mcp.NewServer(&mcp.Implementation{Name: "myapp"}, gosdk.EnableUI(nil))

// Model-visible tool rendered by the table:
gosdk.AddWidgetToolFor(server, table,
    &mcp.Tool{Name: "list_users", Description: "List users."}, listUsers)

// App-only tool (fired by a row action, hidden from the model):
del := &mcp.Tool{Name: "delete_user", Description: "Delete a user."}
gosdk.AppOnly(del, table)
gosdk.AddWidgetToolFor(server, table, del, deleteUser)
```

Multiple tools may link to one widget; `AddWidget` runs implicitly and is idempotent.
Serve via `mcp.NewStreamableHTTPHandler` (HTTP) or `server.Run(ctx, &mcp.StdioTransport{})`
(stdio).

**Quickstart** (complete minimal program):

```go
table := &gadget.Table{
    URI:   "ui://myapp/users",
    Title: "Users",
    Columns: []gadget.Column{
        gadget.Text("name", "Name"),
        gadget.Number("balance", "Balance", "currency:EUR"),
        gadget.Badge("status", "Status", map[string]gadget.BadgeVariant{
            "active": gadget.BadgeSuccess,
        }),
    },
    Filterable: true,
    PageSize:   10,
}

server := mcp.NewServer(&mcp.Implementation{Name: "myapp"}, gosdk.EnableUI(nil))

type in struct{}
type out struct {
    Rows []map[string]any `json:"rows"` // must match Table.RowsKey (default "rows")
}
gosdk.AddWidgetToolFor(server, table,
    &mcp.Tool{Name: "list_users", Description: "List users in a table."},
    func(context.Context, *mcp.CallToolRequest, in) (*mcp.CallToolResult, out, error) {
        rows, _ := gadget.RowsOf(loadUsers())
        return nil, out{Rows: rows}, nil
    })

h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
http.ListenAndServe(":8080", h)
```

## Embedded per-call mode (result-embedded MCP Apps widgets)

Instead of registering a `ui://` template resource once and linking tools via
`_meta.ui.resourceUri`, deliver a fresh self-contained document in **each tool result**. This is
what this template's binzaar server uses; interactions still flow through the standard MCP Apps
**App Bridge**.

- Build the widget **per call**: bake the call's data via `InitialData` (the runtime paints it at
  first render, before any host handshake) and give every render a **unique URI**
  (e.g. `ui://app/kind/<unixnano>` — hosts key renders by URI).
- **Set `LoadTool` so a reloaded snapshot re-hydrates.** The baked `InitialData` is frozen at render
  time; if the host reloads or remounts the iframe the widget would repaint that stale snapshot. Give
  each widget a `LoadTool` (a read tool returning the data under the widget's `RowsKey`/`PrefillKey`)
  and the runtime calls it once after the handshake to replace the snapshot with current data.
- Append the rendered `Document()` to the tool result's `content` **after** the JSON text block, as
  an embedded resource carrying the **MCP Apps HTML profile** `mimeType: "text/html;profile=mcp-app"`.
  With mark3labs/mcp-go: `res.Content = append(res.Content, mcp.NewEmbeddedResource(
  mcp.TextResourceContents{URI: uri, MIMEType: "text/html;profile=mcp-app", Text: doc}))`.
- **Interactions use the standard App Bridge JSON-RPC** (gadget runtime): once the host attaches its
  App Bridge (`ui/initialize` handshake), a widget action calls **`tools/call`** (the bridge runs
  the named tool directly against the server and returns the result to the widget), and a link calls
  **`ui/open-link`**. If a tool the widget called returns `structuredContent` with `RowsKey`, the
  table re-renders with those rows and clears the selection; otherwise the call resolves and the
  refreshed widget arrives as the next embedded result. Do not hand-author postMessage payloads or
  any text-sentinel envelope — configure actions via gadget's `Action`/`Submit` API.
- **The iframe auto-resizes** (gadget runtime): size reporting starts at first paint; the widget
  reports its content height and the bridge applies it via **`ui/notifications/size-changed`**, so
  hosts grow the iframe and the widget is never internally scrolled (`width` omitted so the
  responsive CSS width wins; the document resets `body{margin:0;padding:8px}` so `body.scrollHeight`
  measures true content height).
- Consequences: point actions/submits at **model-visible** tools (the per-call document is not a
  registered template with linked app-only tools, so don't register app-only `ui_*` helpers or rely
  on `_meta.ui.visibility` here), and there is no separate rows-refresh round-trip — embed a
  **freshly rendered widget with the refreshed dataset** in each mutating tool's result. The JSON
  result must stand alone: log widget build/render failures to stderr and never fail the tool over
  UI.

## Manual wiring (any other Go MCP SDK, e.g. mark3labs/mcp-go)

The core emits plain spec-shaped values; adapt them to your SDK:

```go
w := table // or form; any gadget.Widget

doc, err := w.Document() // render once (validates); serve from memory
d := w.Descriptor()      // d.URI, d.Name (derived: "ui://demo/users" -> "demo-users"),
                         // d.Title, d.MIMEType ("text/html;profile=mcp-app"), d.MetaMap()
```

1. **Advertise the extension** in server capabilities:
   `capabilities.extensions["io.modelcontextprotocol/ui"] = {"mimeTypes": ["text/html;profile=mcp-app"]}`.
2. **Register a resource** at `d.URI` with MIME type `d.MIMEType` (plus `d.MetaMap()` as
   `_meta` when non-nil); `resources/read` returns `doc` as text contents.
3. **On each linked tool**, merge `w.ToolMeta()` into the tool's `_meta`. For app-only
   tools, also set `_meta.ui.visibility = ["app"]` (`uispec.ToolUIMeta{ResourceURI: d.URI,
   Visibility: []string{uispec.VisibilityApp}}.MetaMap()`; merge with `uispec.MergeMeta`).
4. **Tool results** carry widget data in `structuredContent` per the data contract above.

Check the installed SDK's API for where capabilities extensions, resource `_meta`, tool
`_meta`, and `structuredContent` are set — read its source under
`go list -m -f '{{.Dir}}' <sdk-module>` rather than guessing. In stdio servers, keep all
logging on stderr.

## Package `theme` — styling overrides

Widgets ship a `--gadget-*` design-token system scoped under `.gadget-root`. Every token
defaults to the host-injected MCP Apps CSS variable (from `hostContext.styles.variables`)
with a built-in fallback — widgets match Claude/ChatGPT theming and dark mode with zero
configuration. Only use `Theme` to deliberately override; nil / zero value overrides nothing.

```go
type Theme struct {
    ColorBackground  string // page/widget background
    ColorSurface     string // cards, table header, inputs
    ColorText        string
    ColorTextMuted   string
    ColorBorder      string
    ColorPrimary     string // accent: primary buttons, focused controls, links
    ColorPrimaryText string // text on primary background
    ColorDanger      string
    ColorSuccess     string
    ColorWarning     string

    FontFamily     string
    FontFamilyMono string

    RadiusS, RadiusM, RadiusL string

    SpaceUnit string // base spacing unit (default 0.25rem)

    Extra map[string]string // raw custom properties; keys MUST start with "--gadget-"
}

func (t *Theme) CSS() string      // ".gadget-root{...}", "" when nothing set; skips invalid entries
func (t *Theme) Validate() error  // surfaces what CSS() would silently skip
```

Fields hold raw CSS values (`"#0f62fe"`, `"0.5rem"`, `"Inter, sans-serif"`). Values must
not contain `{`, `}`, `;`, `</`, or `<!--` (breakout guard); `Extra` keys use only
`[A-Za-z0-9_-]` after the `--gadget-` prefix.

## Package `uispec` — spec constants and `_meta` types

Dependency-free; for manual wiring or advanced `_meta` control.

```go
const (
    ExtensionID = "io.modelcontextprotocol/ui"
    SpecVersion = "2026-01-26"
    MIMEType    = "text/html;profile=mcp-app"
    MetaKey     = "ui"
    URIScheme   = "ui"
)

const (
    VisibilityModel = "model" // tool callable by the model
    VisibilityApp   = "app"   // tool callable from the app UI only
)

const ( // ResourceUIMeta.Permissions values
    PermissionCamera, PermissionMicrophone           = "camera", "microphone"
    PermissionGeolocation, PermissionClipboardWrite  = "geolocation", "clipboardWrite"
)

type CSP struct { // external origins a UI resource needs (hosts default locked-down)
    ConnectDomains, ResourceDomains, FrameDomains, BaseURIDomains []string
}

type ResourceUIMeta struct { // _meta.ui on a ui:// resource (set via Table.UI / Form.UI)
    CSP           *CSP
    Permissions   []string
    Domain        string
    PrefersBorder *bool
}

type ToolUIMeta struct { // _meta.ui on a tool, linking it to its template resource
    ResourceURI string   `json:"resourceUri"`
    Visibility  []string `json:"visibility,omitempty"`
}

type ResourceDescriptor struct { // everything needed to register a widget resource
    URI, Name, Title, Description string
    MIMEType                      string // always uispec.MIMEType for gadget widgets
    UI                            *ResourceUIMeta
}

func (m ResourceUIMeta) MetaMap() map[string]any     // {"ui": {...}}
func (m ToolUIMeta) MetaMap() map[string]any         // {"ui": {"resourceUri": ..., ...}}
func (d ResourceDescriptor) MetaMap() map[string]any // nil when d.UI == nil
func MergeMeta(dst, src map[string]any) map[string]any // recursive merge; nil dst allocated
func ValidateURI(uri string) error                     // well-formed ui:// URI check
```

gadget widgets need no `CSP` declarations — documents are self-contained and satisfy the
spec's default locked-down policy.

## Examples in the gadget module (for study)

Locate the module: `go list -m -f '{{.Dir}}' github.com/techthos/gadget`.

- `examples/demo` — complete runnable MCP server (users table with row/bulk actions +
  edit form with server-side validation, prefill, string-ID parsing).
  `go run ./examples/demo -addr :8080` (streamable HTTP at `/mcp`) or `-stdio`.
- `examples/harness` — a fake MCP Apps host in one HTML page: sandboxed iframe,
  `ui/initialize` handshake, JSON-RPC logging, simulated tool results/themes.
  `go run ./examples/harness`, open `http://localhost:8090`. Verifies widget behavior
  without any real MCP client.
