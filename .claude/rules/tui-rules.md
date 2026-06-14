---
description: Rules and conventions for the rivo/tview terminal UI layer — the Application, primitives, the concurrency model that keeps the screen race-free, and the cross-app product design standards (layout, navigation, tables, CRUD, forms, filters) every micro-app TUI must follow.
paths:
  - internal/tui/**
---

# tview TUI rules (`internal/tui`)

These rules apply when working in `internal/tui` — the terminal user-interface layer built on
`rivo/tview` (which sits on top of `gdamore/tcell/v2`).

## Library

- **Package:** `github.com/rivo/tview` — rich, interactive terminal widgets. Built on
  `github.com/gdamore/tcell/v2` (the screen/event backend).
- **Version:** pin the latest stable `rivo/tview` and `gdamore/tcell/v2` in `go.mod`. tcell **v2**
  is mandatory — never import the v1 module path.
- **Imports:** no alias; use the canonical names:
  ```go
  import (
      "github.com/gdamore/tcell/v2"
      "github.com/rivo/tview"
  )
  ```
- **Docs:** README https://github.com/rivo/tview · GoDoc https://pkg.go.dev/github.com/rivo/tview ·
  Wiki (Concurrency, Primitives, Pages, Form, TextArea) https://github.com/rivo/tview/wiki

## The application & its single goroutine (the rule that matters most)

tview has **one event loop, on one goroutine**. Almost nothing in tview is thread-safe. Everything
below follows from this.

- Create exactly **one** `*tview.Application` (`tview.NewApplication()`), own it in `internal/tui`,
  and start it with `app.SetRoot(root, true).EnableMouse(true).Run()`. `Run()` blocks until
  `app.Stop()`; propagate its returned error to the caller — never `panic` in library code.
- **Event handlers run on the main goroutine.** Callbacks like `SetSelectedFunc`,
  `SetChangedFunc`, `SetDoneFunc`, `SetInputCapture`, and button handlers are safe to mutate
  primitives directly — do **not** wrap them in `QueueUpdate`/`QueueUpdateDraw`.
- **Never call `app.Draw()`, `app.QueueUpdate()`, or `app.QueueUpdateDraw()` from inside an event
  handler or `SetInputCapture`** — the loop already redraws after the handler. Doing so deadlocks.

## Updating the UI from background goroutines

Any goroutine other than the event loop must funnel mutations through the queue:

- **`app.QueueUpdateDraw(func(){ ... })`** — run the closure on the event loop **and** redraw.
  This is the default for "data changed, refresh the screen". It blocks until the closure has run.
- **`app.QueueUpdate(func(){ ... })`** — same synchronization without the implicit redraw; use for
  batched changes (and for read-only access to a primitive another goroutine may mutate).
- Keep queued closures **tiny** — just the primitive mutation. Do the slow work (I/O, DB calls,
  computation) in the goroutine *before* queuing, never inside the closure (it stalls the UI).
- **Never block the event loop**: no network/disk/DB work in handlers or queued closures. Run it in
  a goroutine and `QueueUpdateDraw` the result back.

```go
go func() {
    products, err := repo.List(ctx) // slow work OFF the event loop
    app.QueueUpdateDraw(func() {     // tiny mutation ON the event loop
        if err != nil {
            statusBar.SetText("[red]load failed")
            return
        }
        renderTable(table, products)
    })
}()
```

### TextView is the one exception

`*tview.TextView` implements `io.Writer` and its writes are goroutine-safe. You may
`fmt.Fprintf(textView, ...)` from any goroutine for logs/streaming output. To repaint as it fills,
set `SetChangedFunc(func(){ app.Draw() })`. Note: unlike every other handler, **`SetChangedFunc`
runs on the *writer's* goroutine, not the event loop** — which is exactly why the correct call
there is `app.Draw()` (safe from any goroutine), and you must *not* mutate other primitives inside
it.

## Layout & composition

- Compose the UI as a tree of **layout primitives wrapping leaf widgets**. Prefer:
  - **`Flex`** for proportional row/column layouts: `AddItem(p, fixedSize, proportion, focus)` —
    `fixedSize 0` + `proportion > 0` means "share the remaining space".
  - **`Grid`** for fixed grids with `SetRows`/`SetColumns` (`0` = flexible track).
  - **`Pages`** for stacked screens, modals, and overlays — switch with
    `SwitchToPage` / `ShowPage` / `HidePage`.
- Keep `SetRoot` called **once** with the top-level container; swap *content* via `Pages`, not by
  repeatedly calling `SetRoot`.
- Build each screen/widget as its own constructor (e.g. `newProductList(app, repo) *tview.Flex`)
  that wires its handlers and returns the configured primitive. No package-level mutable UI state.

## Widgets — pick the right one

- **`Table`** — spreadsheet/grid data (the product list). `SetCell(row, col, NewTableCell(...))`,
  `SetFixed(rows, cols)` to freeze headers, `SetSelectable(rows, cols)` for row/cell selection,
  `SetSelectedFunc` (Enter on a row) and `SetDoneFunc` (Escape). Remember header rows shift your
  data indices — guard against selecting row 0.
- **`List`** — single-column menus with optional shortcut runes and secondary text.
- **`TreeView`** — hierarchical data; build with `NewTreeNode`, `AddChild`, `SetReference` to attach
  your domain object, expand/collapse via `SetExpanded`, handle `SetSelectedFunc`.
- **`Form`** — stacked inputs + buttons; add `InputField`, `DropDown`, `Checkbox`, `TextArea`,
  `PasswordField`, then read values with `GetFormItemByLabel("X").(*tview.InputField).GetText()`.
- **`InputField`** — single-line input. Constrain with `SetAcceptanceFunc` (e.g.
  `tview.InputFieldInteger`), suggest with `SetAutocompleteFunc`, mask with `SetMaskCharacter`.
- **`Modal`** — confirmation/alert dialogs; layer over content via `Pages`.

### Large datasets: virtual tables

Do **not** `SetCell` millions of rows — it holds every cell in memory. For large/streamed data,
implement the **`TableContent`** interface (`GetCell`, `GetRowCount`, `GetColumnCount`) and attach
it with `table.SetContent(content)`; tview calls it only for the **visible** rows. Embed
`tview.TableContentReadOnly` for read-only data. Leave `SetEvaluateAllRows(false)` (the default) —
enabling it defeats the point.

## Focus & input

- The Application tracks a single focused primitive. Set it explicitly with `app.SetFocus(p)`;
  after changing pages or rebuilding a view, restore focus deliberately.
- Use **`app.SetInputCapture`** for global keybindings (e.g. Ctrl-C / quit, page switching) and a
  primitive's `SetInputCapture` for local ones. Return `nil` to consume an event, or return the
  `*tcell.EventKey` to let it propagate.
- Compare keys via `event.Key()` (`tcell.KeyEnter`, `tcell.KeyEscape`, `tcell.KeyCtrlC`, …) and
  `event.Rune()` for printable keys. Always provide a visible, documented way to quit.

## Mouse, paste & suspending the UI

- Enable mouse and bracketed paste at startup when the UI benefits:
  `app.SetRoot(root, true).EnableMouse(true).EnablePaste(true)`. Without `EnablePaste`, pasted
  multi-line text arrives as individual keystrokes.
- To run an external program or drop to the shell (e.g. an `$EDITOR`), use **`app.Suspend(fn)`** —
  it restores the normal terminal, runs `fn`, then re-enters the TUI. Never spawn a foreground
  subprocess without it, or it will fight tview for the terminal.

## Testing

- tview is testable headless: create the app, build a `tcell.SimulationScreen`
  (`tcell.NewSimulationScreen("UTF-8")`, then `SetSize`), attach it with **`app.SetScreen(sim)`**,
  drive input, and assert on `sim.GetContents()`. No real terminal required.
- Keep rendering logic in pure helpers (data → cells/strings) that you can unit-test *without* the
  Application at all; reserve screen-level tests for wiring/keybindings.

## Custom primitives

- Build a custom widget by **embedding `*tview.Box`** and implementing only the `Primitive` methods
  you need (`Draw`, `InputHandler`, …); embedding `Box` supplies sane defaults for the rest.
  ```go
  type ProductPanel struct {
      *tview.Box
      // fields...
  }
  func NewProductPanel() *ProductPanel { return &ProductPanel{Box: tview.NewBox()} }
  ```

## Theming & text markup

- Set the global theme **once at startup, before any widget is created**, by assigning
  `tview.Styles = tview.Theme{...}`. Don't hard-code per-widget colors that fight the theme.
- Enable inline color tags per widget with `SetDynamicColors(true)`, then use tcell-style tags:
  `[fg:bg:attrs:url]` (e.g. `[red]`, `[yellow:blue]`, `[::b]bold[::-]`, `[-:-:-:-]` full reset).
  Escape a literal tag with a trailing `[` : `[red[]` prints `[red]`.

# Product design standards (every micro-app TUI)

The rules above are about tview mechanics. The rules below are the **shared product design
language** — the layout, navigation, CRUD flow, and visual conventions that every micro-app built
from this template must follow so they all feel like the same family. Deviate only with a reason,
and when you do, keep the deviation local and documented. These describe **observable behavior**, so
changes here travel with `docs/SPECIFICATIONS.md` (see `specification-rules.md`).

## App skeleton: sidebar · body · status bar

Every app is one `Flex` with three regions, set as the single `SetRoot`:

```
┌──────────┬──────────────────────────────┐
│ SIDEBAR  │ HEADER (screen title · count) │
│ 1 …  ●   │──────────────────────────────│
│ 2 …      │ BODY  (Pages — swappable)     │
│ 3 …      │                               │
├──────────┴──────────────────────────────┤
│ context · 2 of 3   message/spinner   ? help │
└──────────────────────────────────────────┘
```

- **Sidebar** (left, fixed width): the navigation menu and the app's "home" — there is **no
  separate home screen**. It lists the top-level sections, each with a **numeric shortcut** and an
  optional **count + attention badge**. The active section is highlighted.
- **Body** (right, flexible): a **`Pages`** container whose visible page is the current screen.
  Swap *content* by switching pages — never call `SetRoot` again.
- **Status bar** (bottom, one line, three zones, left→right):
  1. **context** — `Catalog · 2 of 3` or `row 2 of 3`;
  2. **message/progress** — transient spinner or colored result (this is where async outcomes land);
  3. **key hints** — the few most relevant keys for the focused screen, always ending in `? help`.

## Navigation

- The **sidebar is the numbered menu.** Pressing **`1`–`9`** anywhere (outside a text input) jumps to
  that section and loads it in the body. `↑/↓` + `Enter` while the sidebar is focused also selects.
- **`Ctrl-B`** toggles the sidebar collapsed/expanded.
- **`Esc` backs out one level** within a section: form → list, detail → list, and from a top-level
  list it does nothing (you're already home). Drilling in is a `Pages` push; `Esc` pops.

## Keybindings (no F-keys — ever)

Context-sensitive: **single letters act while a list/table is focused; Ctrl-chords act while a form
or text input is focused** (so typing never fires an action). Use **numbers, not F-keys**, for
selection and shortcuts. The shared vocabulary — keep it identical across apps:

| Key | Meaning | Active where |
|---|---|---|
| `↑ ↓` / `j k` | move selection | lists/tables |
| `Enter` | open / confirm | everywhere |
| `Esc` | back / cancel | everywhere |
| `1`–`9` | jump to section | global (not in inputs) |
| `Ctrl-B` | toggle sidebar | global |
| `/` | filter | lists/tables |
| `?` | help overlay | everywhere |
| `q` / `Ctrl-C` | quit | top level |
| `n` | new (create) | lists |
| `e` | edit | lists |
| `d` | delete | lists |
| `r` | refresh / reload | lists |
| `Space` | toggle row select | lists |
| `Ctrl-S` | save | forms |

Always surface the active keys in the status bar, and the **full** set via the `?` overlay.

## Tables (the primary browse view)

- **Frozen bold header** (`SetFixed(1, 0)`), **full-row selection** (`SetSelectable(true, false)`),
  text columns left-aligned, **numbers right-aligned**. Guard against selecting the header row 0.
- `Enter` opens the record (see Detail); `n/e/d/r` are the row actions; `/` filters; `Space`
  toggles multi-select.
- Show **`x of y`** in the status-bar context zone.
- **Truncate** overflowing cells with a trailing `…` (full value lives in the detail pane). Never
  horizontal-scroll a table by default.
- For large/streamed data use the virtual `TableContent` interface (see above) — never `SetCell`
  unbounded rows.

### Multi-select & bulk actions

- `Space` toggles a per-row checkmark. An action key then applies to **all checked rows**, with a
  **single batch confirm naming the count** (`Delete 2 items? [y/N]`).
- If **nothing is checked**, the action targets the **highlighted row**.

### Filtering

- `/` focuses an incremental filter bound to the table; typing narrows rows **live**,
  **case-insensitive substring across the visible columns**.
- `Esc` clears the filter and refocuses the table. The status context reads `1 of 3 (filtered)`.
- Filters are **never persisted** — they always start empty.

## CRUD actions & confirmation

- **Direct keys, immediate effect.** `n` → create form, `e` → edit form, `Enter` → detail,
  `d` → delete, `r` → refresh. Non-destructive actions act immediately.
- **Confirm only destructive/irreversible actions.** Use a centered `Modal` over the body whose
  **focus defaults to the safe choice (`No`/Cancel)**, names the exact target, and warns when it
  "cannot be undone". `y`/`Enter`-on-Yes confirms; `n`/`Esc` cancels.
- High-risk, wide-blast actions (reset-all, delete-all) additionally **require typing a confirm
  word** to enable `Yes`. Ordinary single/bulk deletes stay a simple Yes/No.

## Forms (create & edit)

- **Full-screen** in the body, **one field per row** with aligned labels. Reuse **one constructor**
  for create and edit; edit pre-fills the fields.
- **`Ctrl-S` saves; `Esc` cancels** (prompt `Discard changes? [y/N]` if the form is dirty).
- **Validate live, per field**: show the error inline beneath the field in `[red]`, and **disable
  save while any field is invalid**. On a blocked save attempt, focus the first offending field.

## Detail view (master-detail split)

- Selecting a row shows its record in a **right-hand detail pane within the same screen** (list
  left, detail right) — not a separate navigation step. Field order matches the form.
- Record actions (`e` edit, `d` delete) are available from the detail pane.
- Long values **wrap and scroll** (arrow keys when the pane is focused); cells elsewhere truncate.

## States: loading · empty · error (mandatory, no blank screens)

Every data view must handle all three with a **centered message**:

- **Loading** — `Loading…` / spinner.
- **Empty** — a message **plus the action hint** (`No installs yet — press n`).
- **Error** — a `[red]` message **plus a retry hint** (`press r to retry`). Never panic; surface it.

## Async feedback

- Run slow work in a goroutine; show a **spinner / `working…`** in the status-bar message zone,
  then a colored result there (`[green]✓ …` / `[red]✗ …`). `QueueUpdateDraw` the small mutation
  back per the concurrency rules above. Errors land in the status bar, never as a panic.

## Focus & tab order

- On entering a screen, focus the **primary body widget (table)** — not the sidebar.
- `Tab`/`Shift-Tab` cycle the focusable regions in a defined order (sidebar ↔ table ↔ detail pane).
- Returning from a form or detail **restores the prior row selection and focus**.

## Quit

- `q` / `Ctrl-C` quit from a top-level list/section. `Esc` backs out one level first.
- Quitting with a **dirty form or an in-flight operation prompts a confirm**; otherwise exit
  immediately.

## Color & markup semantics

Set one `tview.Theme` at startup; use a **fixed semantic vocabulary** everywhere (don't hard-code
colors that fight the theme):

- `[green]` success / installed / healthy
- `[red]` error / destructive
- `[yellow]` warning / update-available / attention
- gray / dim — disabled, placeholder, empty
- default — normal

## Value formatting

- **Timestamps**: relative in lists (`2h ago`, `3d ago`), absolute on the detail pane.
- **Numbers**: right-aligned.
- **Empty/missing**: a dim **`—`** — never blank, never `null`.

## Responsive layout

- Target a **minimum of 80×24**. On resize, the layout **reflows**.
- Below a width threshold, **auto-collapse the sidebar** (still toggleable with `Ctrl-B`) and stack
  the master-detail split (or push detail to its own screen).
- Below the hard minimum, show a centered **`Terminal too small — need 80×24`** until resized.

## Persisted UI state

- Persist only lightweight view preferences — **last active section** and **sidebar
  collapsed/expanded** — in the app's **`Config` singleton** (never mixed with domain data) so the
  app reopens where you left off.
- **Filters and row selection are never persisted** — they always start fresh.

## Do / Don't

- ✅ Own one `Application`; return `Run()`'s error up the stack.
- ✅ Do slow work in a goroutine, then `QueueUpdateDraw` a **small** mutation back.
- ✅ Mutate freely inside event handlers — they're already on the main goroutine.
- ✅ Keep `internal/tui` a thin view layer; pull data through `internal/db` repositories, never open
  bbolt or run business logic in a draw/handler.
- ❌ Don't call `Draw`/`QueueUpdate`/`QueueUpdateDraw` from within an event handler or input capture
  (deadlock).
- ❌ Don't touch a primitive from a goroutine outside the queue (race) — except writing to a
  `TextView` via `io.Writer`.
- ❌ Don't block the event loop with I/O, DB, or network calls.
- ❌ Don't `panic` on UI errors in library code; surface them to the caller or a status widget.
- ✅ Follow the shared design language: sidebar·body·status skeleton, numbered sections, letters in
  lists / Ctrl in forms, the three mandatory data states, and the semantic color vocabulary.
- ✅ Default destructive-action confirms to the safe choice and name the target.
- ❌ Don't use F-keys — use numbers for selection and shortcuts.
- ❌ Don't render a blank screen — every data view shows loading, empty, or error.
- ❌ Don't invent per-app key meanings that clash with the shared vocabulary (`n/e/d/r`, `/`, `?`,
  `Esc`, `Ctrl-S`).
