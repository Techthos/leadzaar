# How an AI chat host renders Leadzaar's MCP Apps widgets

This guide is for implementers of an AI chat application (an MCP **host**) that connects to the
Leadzaar MCP server and wants its interactive widgets to render and behave correctly. Leadzaar is a
single Go binary whose MCP server (`internal/server`) embeds interactive HTML widgets in its tool
results, following the **MCP Apps** extension (`io.modelcontextprotocol/ui`, spec version
`2026-01-26`). This describes the contract from the host's side: what the server sends, how to
render it, and how widget interactions flow back through the standard **App Bridge**.

The protocol here is the one defined by the MCP Apps specification — not a project-specific
invention. See the overview at <https://modelcontextprotocol.io/extensions/apps/overview> and the
specification and `@modelcontextprotocol/ext-apps` SDK at
<https://github.com/modelcontextprotocol/ext-apps>. A host that implements MCP Apps needs no bespoke
message plumbing: attach the standard App Bridge and everything below is handled. Widgets are built
with `github.com/techthos/gadget`, whose runtime speaks the App Bridge for the widget.

## 1. What the server sends

Every CRUD tool result — the list/read tools (`list_leads`, `get_deal`, `pipeline_summary`, …) and
the mutating tools (`create_*`, `update_*`, `delete_*`, `convert_lead`) — carries, in order:

1. a short **status text** content block (and a matching `structuredContent` object) that stands
   alone, and
2. one extra **embedded resource** content block:

```json
{
  "type": "resource",
  "resource": {
    "uri": "ui://leadzaar/leads/1753260000000000000",
    "mimeType": "text/html;profile=mcp-app",
    "text": "<!doctype html>... complete self-contained document ..."
  }
}
```

Properties the host can rely on:

- **`ui://` scheme + `mimeType: "text/html;profile=mcp-app"`** identifies a renderable MCP Apps
  widget. The `profile=mcp-app` token is what the host keys on to attach its App Bridge.
- **The document is fully self-contained** (inline CSS and JS, the call's data baked in). It needs
  no network access, no `resources/read` round-trip, and no CSP allowances beyond a locked-down
  default.
- **The URI is unique per render** (`ui://leadzaar/<kind>/<unixnano>`). Key renders by URI: two
  calls to the same tool produce two distinct widgets, each painting the data of its own call. Never
  dedupe or cache by URI prefix.
- **The text block is a human status line, not raw JSON** (`"3 leads."`, `"Lead #7 created."`,
  `"Deal #4 deleted."`). The machine-readable payload lives in `structuredContent`; keep that in the
  model's context (it is what the model reasons over), and show the text line to the user. A host
  that renders no UI can ignore the resource block and lose nothing.

Widgets come in two kinds: **tables** (list/read output; filterable, paginated, sortable, with
per-row action buttons — **Delete** on every entity, plus **Convert** on leads) and **forms**
(create/update input; prefilled values, inline per-field server-side errors). The host does not need
to distinguish them; both are self-contained documents obeying the same protocol.

**App discovery (picker-style hosts).** A host that presents an app launcher lists an app when a
**tool declares `_meta.ui.resourceUri`** pointing at a registered `ui://` resource. Leadzaar links
its six browse tools — `list_leads`, `list_contacts`, `list_deals`, `list_companies`,
`list_offers`, and `pipeline_summary` — to stable `ui://leadzaar/<entity>` templates (also served
with `mimeType: "text/html;profile=mcp-app"` via `resources/read`), so those surface as apps.
Selecting an app runs its tool and renders the template from the result's `structuredContent`
(rows under the table's `RowsKey`). The other tools (create/update/get/delete/convert) carry no
`_meta.ui.resourceUri` and are not separate apps — they deliver their widget via the embedded
resource block, and their mutations refresh an open browse widget in place (section 4). The MCP Apps
extension is advertised in the server's `experimental` capabilities (the `mark3labs/mcp-go` SDK
exposes no `extensions` slot); a host that keys on the per-tool `_meta.ui.resourceUri` link needs
nothing from the capability object.

## 2. Rendering the widget and attaching the App Bridge

- Render the document in a **sandboxed iframe**. The MCP Apps **App Bridge** from
  `@modelcontextprotocol/ext-apps` owns the iframe lifecycle, the `ui/initialize` capability
  handshake, and message passing over `postMessage`; the
  [`basic-host`](https://github.com/modelcontextprotocol/ext-apps/tree/main/examples/basic-host)
  example shows the integration, and `@mcp-ui/client` provides ready-made React components. Do not
  grant the iframe `allow-same-origin`, popups, or downloads.
- The widgets never call `alert()`/`confirm()`/`prompt()` — destructive actions (Delete, Convert)
  use an inline two-phase confirm button — so the sandbox can stay dialog-free.
- **Auto-resize.** The widget reports its content height and the bridge applies it via the standard
  **`ui/notifications/size-changed`** mechanism. Set the iframe height from it; let the iframe fill
  the available width and the widget's responsive CSS adapt. A host that honors this never shows an
  internally scrolled or clipped widget.
- **Theming.** Widgets use a `--gadget-*` design-token system with built-in light/dark fallbacks;
  they look correct with zero configuration. A host may inject `hostContext.styles.variables`
  (delivered via `ui/notifications/host-context-changed`) to align them with its theme, but this is
  optional.
- **Re-hydration on load.** Each table declares a `loadTool` (its `list_*` tool, or
  `pipeline_summary` for the two summary tables). After the `ui/initialize` handshake the runtime
  calls that tool once and replaces the baked snapshot with current data, so a widget that is
  remounted (a new turn, a message-list re-render) shows live data rather than the render-time
  snapshot. Create/update **forms carry no `loadTool`**: their baked snapshot is the edit buffer and
  must survive a remount unchanged.

## 3. How interactions flow back (MCP Apps JSON-RPC)

Widget buttons, form submits, and links do not mutate anything inside the iframe. Each interaction
is a standard MCP Apps request the widget sends to the host over the `postMessage` channel, and the
App Bridge routes it. Every message is **JSON-RPC 2.0** — there is no text sentinel, prompt
injection, or bespoke envelope.

- **Tool actions** (row buttons, form submit) → **`tools/call`** with the target tool name and
  arguments. The App Bridge runs it **directly** against the Leadzaar MCP server and returns the
  result to the widget. The targets are the normal model-visible tools (`delete_lead`,
  `convert_lead`, `update_contact`, `create_deal`, …).
- **Links** → **`ui/open-link`** with the target URL. Open it in a new browser tab; never navigate
  the iframe or the chat page.
- **Sizing** ← the host applies `ui/notifications/size-changed` (see section 2).

The full method surface, for a host implementing the bridge directly rather than using the SDK:

| Direction | Method | Purpose |
|---|---|---|
| widget → host | `ui/initialize` | capability handshake |
| widget → host | `tools/call` | run a tool on the MCP server |
| widget → host | `resources/read` | read a resource |
| widget → host | `ui/open-link` | open an external URL |
| widget → host | `ui/message` | add a message to the conversation |
| widget → host | `ui/request-display-mode` | request a display-mode change |
| widget → host | `ui/update-model-context` | update the model's context |
| host → widget | `ui/notifications/tool-input` / `…-partial` | (streaming) tool input |
| host → widget | `ui/notifications/tool-result` | tool result pushed to the widget |
| host → widget | `ui/notifications/tool-cancelled` | tool execution cancelled |
| host → widget | `ui/notifications/size-changed` | iframe size update |
| host → widget | `ui/notifications/host-context-changed` | host state (theme, etc.) |

## 4. The refresh loop (in-place, plus an embedded fallback)

When a widget's `tools/call` completes, the host pushes the result back to the **same** widget via
**`ui/notifications/tool-result`**, and the gadget runtime re-renders in place from the result's
`structuredContent`:

- a **table** repaints its rows when `structuredContent` carries that table's **`RowsKey`** (the
  fresh rows array) and clears any selection;
- a **form** re-applies fields from its **`PrefillKey`** and inline field errors from its
  **`ErrorsKey`**.

So every Leadzaar mutating tool returns the **refreshed collection under the target table's
`RowsKey`**. The keys are the entity names — a leads table reads its rows from `structuredContent.leads`,
so `delete_lead` and `convert_lead` return `{"leads": [...refreshed...], ...}`; likewise `contacts`,
`deals`, `companies`, `offers`, and the summary tables' `dealRows` / `statusRows`. Illustrated with
leads:

1. User: "show me my leads" → model calls `list_leads` → host renders the leads table (rows under
   `leads`).
2. User clicks **Delete** on a row → the widget sends
   `tools/call {"name":"delete_lead","arguments":{"id":7}}` → the App Bridge runs it against the
   server.
3. The `delete_lead` result carries a status line, the deletion record, and the refreshed
   `leads` array in `structuredContent`.
4. The host pushes that result to the widget via `ui/notifications/tool-result`; the table repaints
   without that row.

The same result **also embeds a freshly rendered table** (a new `ui://` URI) as a fallback for hosts
that render result widgets rather than patching in place. A host that supports the in-place push can
render the embedded widget or ignore it; either way the user sees current data. Form submit failures
work the same way: the result carries the field errors under `errors` in `structuredContent` and
embeds the retry form with those errors baked in.

## 5. Host checklist

- [ ] Render `type: "resource"` blocks with a `ui://` URI and
      `mimeType: "text/html;profile=mcp-app"` in a sandboxed iframe (no `allow-same-origin`), keyed
      by URI, via the App Bridge.
- [ ] Honor `ui/notifications/size-changed`; never fix the iframe height.
- [ ] Route widget `tools/call` to the MCP server and push the result back via
      `ui/notifications/tool-result` (in-place refresh); or render the embedded fallback widget.
- [ ] Call each table's `loadTool` once on load to re-hydrate; leave forms on their baked snapshot.
- [ ] Handle `ui/open-link` by opening the URL externally.
- [ ] Keep the status text / `structuredContent` in the model's context; the widget is presentation
      only.
- [ ] Ignore unknown methods and unknown content blocks.

## 6. Security notes for the host

- The document executes arbitrary JS from the MCP server: the sandbox (no same-origin, no
  top-navigation, no popups) is the boundary. Treat every message as untrusted input; validate its
  shape and enforce the bridge's capability policy (which tools a widget may call, whether
  `ui/open-link` is permitted).
- A `tools/call` from a widget can have side effects — Leadzaar's `delete_lead`, `delete_contact`
  (cascade-deletes its deals), `delete_company` (unlinks references), and `convert_lead` all mutate
  the store. The host's capability policy and user-consent model govern which calls are permitted;
  the widgets gate destructive actions behind an inline confirm, but the host is the real boundary.
- Widgets never need network egress; a CSP that blocks all external fetches from the iframe is
  correct and expected.
