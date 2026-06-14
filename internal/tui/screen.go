package tui

import (
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// itoa is a terse strconv.Itoa used to compose status-bar context strings.
func itoa(n int) string { return strconv.Itoa(n) }

// screenState drives the mandatory loading / empty / error views — no data
// screen is ever blank.
type screenState int

const (
	stateLoading screenState = iota
	stateLoaded
	stateError
)

// maxCellWidth caps a plain table cell; overflow is truncated with "…" (the
// full value lives in the detail pane).
const maxCellWidth = 28

// screenConfig wires the generic master-detail list screen to a concrete
// entity. Everything entity-specific is a function the entity file supplies.
type screenConfig[T any] struct {
	page      string
	cols      []col
	cells     func(T) []string
	detail    func(T) string
	id        func(T) uint64
	emptyHint string // shown on the empty state, e.g. "No leads yet — press n"
	hints     string // status-bar key hints for this screen (without "? help")
	newForm   func()
	editForm  func(T)
	del       func(targets []T)
	extra     func(ev *tcell.EventKey, sel T, ok bool) *tcell.EventKey
}

// listScreen is a reusable browse screen: a frozen-header table on the left, a
// detail pane on the right, an incremental filter, multi-select, and the three
// mandatory data states. It carries no business logic.
type listScreen[T any] struct {
	t   *tui
	cfg screenConfig[T]

	root      *tview.Flex  // [ data/state | detail ]
	left      *tview.Pages // "data" (table+filter) vs "state" (centered message)
	dataFlex  *tview.Flex  // table, with the filter bar prepended when active
	table     *tview.Table
	detailTV  *tview.TextView
	filterBar *tview.InputField
	stateView *tview.TextView

	items     []T
	view      []T
	selected  map[uint64]bool
	filter    string
	filtering bool
	state     screenState
	err       error
}

func newListScreen[T any](t *tui, cfg screenConfig[T]) *listScreen[T] {
	s := &listScreen[T]{t: t, cfg: cfg, selected: map[uint64]bool{}, state: stateLoading}

	s.table = tview.NewTable().SetBorders(false).SetSelectable(true, false).SetFixed(1, 0)
	s.table.SetSelectionChangedFunc(func(int, int) { s.updateDetail() })
	s.table.SetSelectedFunc(func(row, _ int) {
		if item, ok := s.itemAtRow(row); ok {
			s.cfg.editForm(item)
		}
	})
	s.table.SetInputCapture(s.tableKeys)

	s.detailTV = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
	s.detailTV.SetBorder(true).SetTitle(" Detail ")

	s.filterBar = tview.NewInputField().SetLabel("/ ")
	s.filterBar.SetChangedFunc(func(text string) {
		s.filter = text
		s.applyFilter()
		s.refill()
		s.t.refreshChrome()
	})
	s.filterBar.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			s.clearFilter()
			return nil
		case tcell.KeyEnter:
			s.filtering = false
			s.rebuildData()
			s.t.app.SetFocus(s.table)
			return nil
		}
		return ev
	})

	s.stateView = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)

	s.dataFlex = tview.NewFlex().SetDirection(tview.FlexRow)
	s.rebuildData()

	s.left = tview.NewPages()
	s.left.AddPage("data", s.dataFlex, true, true)
	s.left.AddPage("state", centeredPrimitive(s.stateView), true, false)

	s.root = tview.NewFlex().
		AddItem(s.left, 0, 2, true).
		AddItem(s.detailTV, 0, 1, false)
	return s
}

// rebuildData lays the table out, prepending the filter bar when a filter is
// active or being typed.
func (s *listScreen[T]) rebuildData() {
	s.dataFlex.Clear()
	if s.filtering || s.filter != "" {
		s.dataFlex.AddItem(s.filterBar, 1, 0, false)
	}
	s.dataFlex.AddItem(s.table, 0, 1, true)
}

// setItems replaces the screen's data and recomputes the visible view, state,
// and detail. Runs on the event loop.
func (s *listScreen[T]) setItems(items []T, err error) {
	s.items = items
	s.err = err
	// Drop multi-select marks for rows that no longer exist.
	present := map[uint64]bool{}
	for _, it := range items {
		present[s.cfg.id(it)] = true
	}
	for id := range s.selected {
		if !present[id] {
			delete(s.selected, id)
		}
	}

	switch {
	case err != nil:
		s.state = stateError
		s.view = nil
		s.stateView.SetText("[" + colorError + "]" + err.Error() + "[-]\n\npress r to retry")
		s.left.SwitchToPage("state")
	case len(items) == 0:
		s.state = stateLoaded
		s.view = nil
		s.stateView.SetText("[" + colorDim + "]" + s.cfg.emptyHint + "[-]")
		s.left.SwitchToPage("state")
	default:
		s.state = stateLoaded
		s.applyFilter()
		s.refill()
		s.left.SwitchToPage("data")
	}
	s.updateDetail()
}

// applyFilter recomputes the visible rows: a case-insensitive substring match
// across the rendered columns. An empty filter shows everything.
func (s *listScreen[T]) applyFilter() {
	if s.filter == "" {
		s.view = s.items
		return
	}
	needle := strings.ToLower(s.filter)
	s.view = nil
	for _, it := range s.items {
		if strings.Contains(strings.ToLower(strings.Join(s.cfg.cells(it), " ")), needle) {
			s.view = append(s.view, it)
		}
	}
}

// refill repaints the table from the visible view, with a frozen bold header
// and a leading multi-select marker column.
func (s *listScreen[T]) refill() {
	s.table.Clear()
	s.table.SetCell(0, 0, headerCell(" "))
	for c, col := range s.cfg.cols {
		hc := headerCell(col.title)
		if col.right {
			hc.SetAlign(tview.AlignRight)
		}
		s.table.SetCell(0, c+1, hc)
	}
	for r, item := range s.view {
		mark := " "
		if s.selected[s.cfg.id(item)] {
			mark = "[" + colorSuccess + "]✓[-]"
		}
		s.table.SetCell(r+1, 0, tview.NewTableCell(mark).SetSelectable(true))
		for c, val := range s.cfg.cells(item) {
			tc := tview.NewTableCell(truncateCell(val))
			if s.cfg.cols[c].right {
				tc.SetAlign(tview.AlignRight)
			}
			s.table.SetCell(r+1, c+1, tc)
		}
	}
	// Keep the selection on a real data row (row 0 is the header).
	row, _ := s.table.GetSelection()
	if n := len(s.view); n == 0 {
		s.table.Select(0, 0)
	} else if row < 1 || row > n {
		s.table.Select(1, 0)
	}
}

// updateDetail refreshes the detail pane for the highlighted row.
func (s *listScreen[T]) updateDetail() {
	if item, ok := s.selectedItem(); ok {
		s.detailTV.SetText(s.cfg.detail(item))
		s.detailTV.ScrollToBeginning()
		return
	}
	s.detailTV.SetText("")
}

func (s *listScreen[T]) itemAtRow(row int) (T, bool) {
	idx := row - 1
	if idx < 0 || idx >= len(s.view) {
		var zero T
		return zero, false
	}
	return s.view[idx], true
}

func (s *listScreen[T]) selectedItem() (T, bool) {
	row, _ := s.table.GetSelection()
	return s.itemAtRow(row)
}

// targets returns the rows an action applies to: every checked row, or — when
// nothing is checked — the highlighted row.
func (s *listScreen[T]) targets() []T {
	var checked []T
	for _, it := range s.view {
		if s.selected[s.cfg.id(it)] {
			checked = append(checked, it)
		}
	}
	if len(checked) > 0 {
		return checked
	}
	if it, ok := s.selectedItem(); ok {
		return []T{it}
	}
	return nil
}

func (s *listScreen[T]) beginFilter() {
	s.filtering = true
	s.rebuildData()
	s.filterBar.SetText(s.filter)
	s.t.app.SetFocus(s.filterBar)
}

func (s *listScreen[T]) clearFilter() {
	s.filter = ""
	s.filtering = false
	s.applyFilter()
	s.rebuildData()
	s.refill()
	s.t.app.SetFocus(s.table)
	s.t.refreshChrome()
}

// tableKeys handles the shared list keybindings while the table is focused.
func (s *listScreen[T]) tableKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEscape:
		// Esc clears an active filter; at a bare top-level list it does nothing.
		if s.filter != "" {
			s.clearFilter()
		}
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		case 'n':
			s.cfg.newForm()
			return nil
		case 'e':
			if it, ok := s.selectedItem(); ok {
				s.cfg.editForm(it)
			}
			return nil
		case 'd':
			if t := s.targets(); len(t) > 0 {
				s.cfg.del(t)
			}
			return nil
		case 'r':
			s.t.reload()
			return nil
		case '/':
			s.beginFilter()
			return nil
		case ' ':
			if it, ok := s.selectedItem(); ok {
				id := s.cfg.id(it)
				s.selected[id] = !s.selected[id]
				if !s.selected[id] {
					delete(s.selected, id)
				}
				s.refill()
				s.t.refreshChrome()
			}
			return nil
		}
	}
	if s.cfg.extra != nil {
		it, ok := s.selectedItem()
		return s.cfg.extra(ev, it, ok)
	}
	return ev
}

// --- chrome helpers (used by tui to render the header / status / sidebar) ---

func (s *listScreen[T]) primitive() tview.Primitive { return s.root }

func (s *listScreen[T]) focus() { s.t.app.SetFocus(s.table) }

func (s *listScreen[T]) total() int { return len(s.items) }

func (s *listScreen[T]) isFiltered() bool { return s.filter != "" }

// rowContext renders the status-bar context zone ("row 2 of 7", "(filtered)").
func (s *listScreen[T]) rowContext() string {
	if s.state == stateError {
		return "error"
	}
	if len(s.view) == 0 {
		if s.isFiltered() {
			return "0 of " + itoa(s.total()) + " (filtered)"
		}
		return "empty"
	}
	row, _ := s.table.GetSelection()
	ctx := "row " + itoa(row) + " of " + itoa(len(s.view))
	if s.isFiltered() {
		ctx += " (filtered)"
	}
	if n := len(s.selected); n > 0 {
		ctx += " · " + itoa(n) + " selected"
	}
	return ctx
}

func (s *listScreen[T]) keyHints() string { return s.cfg.hints }

// focusables lists the regions this screen contributes to the Tab cycle.
func (s *listScreen[T]) focusables() []tview.Primitive {
	out := []tview.Primitive{s.table}
	if s.filtering || s.filter != "" {
		out = append(out, tview.Primitive(s.filterBar))
	}
	return append(out, s.detailTV)
}

// --- small shared widget helpers ---

func headerCell(text string) *tview.TableCell {
	return tview.NewTableCell(text).SetSelectable(false).SetAttributes(tcell.AttrBold)
}

func truncateCell(s string) string {
	if strings.Contains(s, "[") { // color-tagged placeholder; leave as-is
		return s
	}
	r := []rune(s)
	if len(r) <= maxCellWidth {
		return s
	}
	return string(r[:maxCellWidth-1]) + "…"
}

// centeredPrimitive wraps p so it floats in the middle of its region (used for
// the loading / empty / error state views).
func centeredPrimitive(p tview.Primitive) tview.Primitive {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(p, 0, 2, true).
			AddItem(nil, 0, 1, false), 3, 0, true).
		AddItem(nil, 0, 1, false)
}
