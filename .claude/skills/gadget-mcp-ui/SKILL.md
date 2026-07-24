---
name: gadget-mcp-ui
description: Add an interactive HTML UI (table or form widget) to an MCP tool using github.com/techthos/gadget — MCP Apps (io.modelcontextprotocol/ui) widgets served as ui:// resources from a Go MCP server. Use when the user wants to render a tool's output as a table, add a form UI to an MCP tool, mentions "gadget", "MCP Apps", or ui:// widgets.
allowed-tools: Bash(go *) Bash(ls *) Bash(cat *) Bash(find *) Bash(grep *) Read Write Edit
---

# gadget — interactive MCP Apps widgets for Go MCP servers

`github.com/techthos/gadget` gives this project prebuilt, parameterized **Table** and
**Form** widgets that render inside the chat of MCP Apps hosts (Claude, ChatGPT, VS Code,
Cursor, …). You never write HTML/CSS/JS: declare a widget struct in Go, link tools to it,
and return `structuredContent`-shaped data from tool handlers.

> Generated from gadget source at commit `2cd09d6` (2026-07-23, pre-release — no tagged
> version yet). If `go list -m github.com/techthos/gadget` reports a different version
> than the one recorded here, run **Updating this skill** below before relying on API
> details.

**`reference.md` in this skill directory holds the full API detail** — every struct
field, the `gosdk` adapter functions, theming, `uispec` constants, validation rules, and
the manual (SDK-agnostic) wiring steps. Open it whenever you are about to write code
using gadget; this file only carries the mental model, install steps, and gotchas.

## Mental model

The MCP Apps spec uses a **template model**: the widget's HTML document is registered
once as a `ui://` resource and can contain no per-call data. Data arrives at runtime:

1. **Go renders structure** at registration time — table chrome, form fields, config.
2. **The embedded runtime renders data** inside the host's sandboxed iframe — from every
   tool result's `structuredContent`, matched by key.

The entire widget ↔ server contract is *which keys appear in `structuredContent`*:

| Widget | Key (default) | Shape | Meaning |
|---|---|---|---|
| Table | `rows` (`RowsKey`) | `[]object` | rows to render |
| Form | `values` (`PrefillKey`) | `{field: value}` | prefill for edit flows |
| Form | `errors` (`ErrorsKey`) | `{field: "message"}` | inline server-side errors; marks submit failed |

Sorting, filtering, pagination, and selection are **client-side** over delivered rows.

## Two delivery modes — pick by host

1. **MCP Apps template mode** (register-once hosts): register the widget once as a `ui://` template
   resource, link tools via `_meta.ui.resourceUri`, deliver data through `structuredContent` as
   above. Row/submit actions may target **app-only** tools (hidden from the model, callable from
   the widget).
2. **Embedded per-call mode** (what this template's binzaar server uses): build the widget **per
   call** with the data baked in (`InitialData` — the runtime paints it at first render) and a
   **unique URI per render**, and append the rendered `Document()` to the tool result's `content`
   as an embedded resource (`{type:"resource", resource:{uri, mimeType:"text/html;profile=mcp-app",
   text: doc}}`). The MCP Apps profile mimeType makes the host attach its **App Bridge**, which
   drives every interaction through the standard MCP Apps JSON-RPC (`io.modelcontextprotocol/ui`):
   a widget action becomes a **`tools/call`** the bridge runs directly against the server, a link
   becomes **`ui/open-link`**, and the iframe height is applied via
   **`ui/notifications/size-changed`**. Point actions/submits at **model-visible** tools and embed
   the **refreshed dataset's widget** in each mutating tool's result (there is no separate
   rows-refresh round-trip). See reference.md §Embedded per-call mode.

## Install

```sh
go get github.com/techthos/gadget@latest
```

The core is **SDK-agnostic**. Pick the wiring path by what MCP SDK this project uses
(check `go.mod`):

- **`github.com/modelcontextprotocol/go-sdk`** (official): also `go get` it, then use the
  `github.com/techthos/gadget/gosdk` adapter — `gosdk.EnableUI` (capability),
  `gosdk.AddWidgetToolFor` (register widget + typed tool), `gosdk.AppOnly`
  (widget-only tools). See reference.md §gosdk.
- **Any other Go MCP implementation** — including `mark3labs/mcp-go`, the default in
  projects scaffolded by this template — wire manually via the `Widget` interface
  (`Document()` / `Descriptor()` / `ToolMeta()`). Four steps: advertise the extension
  capability, register the `ui://` resource serving the rendered document, merge
  `ToolMeta()` into each linked tool's `_meta`, and put widget data in tool results'
  `structuredContent`. See reference.md §Manual wiring for the exact steps and shapes.

## Typical flow when asked to "add a UI to tool X"

1. Read reference.md, then read the existing tool handler and its output type.
2. Choose Table (list/collection output) or Form (input/edit flow); define the widget
   struct with a `ui://<app>/<name>` URI.
3. Make the tool's output match the widget's key contract (e.g. wrap the slice as
   `{"rows": [...]}` — `gadget.RowsOf` converts typed slices honoring `json` tags).
4. Pick the delivery mode (see "Two delivery modes"): template mode → register widget + tool
   via `gosdk` or manually per reference.md; embedded mode → build per call with `InitialData`
   + unique URI and append the document to the tool result per reference.md §Embedded.
5. Row actions / form submit targets: in template mode add **app-only** tools (hidden from the
   model, callable from the widget); in embedded mode target the **model-visible** tools.
6. Update the project spec (`docs/SPECIFICATIONS.md`) — a new/changed MCP tool or
   resource is a product change.

## Gotchas (the bugs you will otherwise write)

1. **Match keys exactly.** Handler output JSON key must equal `RowsKey`/`PrefillKey`/
   `ErrorsKey` (defaults `rows`/`values`/`errors`). Mismatch renders nothing, errors nothing.
2. Every row should carry the `RowID` field (default `"id"`) — selection and
   `FromRow`/`FromSelection` depend on it.
3. **Mutating table tools must return the updated full row list** under `RowsKey`, or the
   UI keeps showing stale rows.
4. Hidden/readonly/text form fields submit **strings** (a hidden numeric ID arrives as
   `"3"` — parse server-side). Empty `FNumber` fields are omitted entirely.
5. Mark widget-only tools **app-only** (submit targets, row actions) — and do it *before*
   registering the tool.
6. `FromSelection` only in bulk actions; `FromRow` in per-row actions.
7. **No native dialogs**: `confirm()`/`alert()` don't work in the sandboxed iframe. Use
   `Action.Confirm` for destructive confirmation.
8. Documents must stay **self-contained** — no external URLs/CDNs/fonts, also not via `Theme`.
9. In **template mode**, widgets are registered **once, immutably**; per-call variation belongs
   in tool-result data, not the template. In **embedded mode** it is the opposite: every render
   is a fresh document with a fresh unique URI — reusing a URI makes hosts show a stale render.
10. In **embedded mode**, actions target the **normal model-visible tools** — the per-call document
    is not a registered template with linked app-only tools, so don't register `ui_*` app-only
    helpers or rely on `_meta.ui.visibility` there.
11. Column `Key`s must match the **JSON tag names** of your row structs (`RowsOf` honors
    `json` tags), not Go field names.
12. Sort/filter/pagination are client-side — for big datasets, bound the list server-side.
13. `AddWidget*` returns validation errors at startup (bad URI, duplicate keys, unsafe
    theme values) — check them.
14. If this server runs over **stdio**, all logging goes to stderr; stdout is the
    protocol channel.

## Updating this skill

gadget is pre-release; its API moves. The module ships its own LLM reference
(`AGENTS.md`), so this skill can regenerate itself from the installed version:

1. In the consuming project, find the installed version:
   `go list -m github.com/techthos/gadget`.
2. Compare against the version recorded at the top of this file and of `reference.md`.
3. If they differ, or the user asks to refresh:
   - Locate the module source: `go list -m -f '{{.Dir}}' github.com/techthos/gadget`
     (run `go mod download github.com/techthos/gadget` first if the dir is empty).
   - Read `<dir>/AGENTS.md` fully.
   - Regenerate `reference.md` from it (same structure: mental model → data contract →
     core API → gosdk → theme → uispec → manual wiring → examples), and update any part
     of this SKILL.md that AGENTS.md contradicts (install, key contract, gotchas).
   - Update the recorded version line in **both** files to the newly installed version
     (e.g. `Generated from gadget v0.3.1`).
4. Report what changed between the two versions so the user can review affected code.
