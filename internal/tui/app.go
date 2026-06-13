package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// Page names used with the Pages primitive.
const (
	pageDashboard = "dashboard"
	pageLeads     = "leads"
	pageContacts  = "contacts"
	pageDeals     = "deals"
	pageOverlay   = "overlay" // transient forms/modals
)

// tui owns the single Application and all screen primitives. All data is pulled
// through store; the cached slices map a selected table row to its entity.
type tui struct {
	app    *tview.Application
	pages  *tview.Pages
	store  *db.Store
	status *tview.TextView

	dashboard     *tview.TextView
	leadsTable    *tview.Table
	contactsTable *tview.Table
	dealsTable    *tview.Table

	leads    []models.Lead
	contacts []models.Contact
	deals    []models.Deal

	// overlayIsModal is true while the open overlay is a button-only modal
	// (stage picker, delete confirm) rather than a text form. Modals have no
	// text entry, so the global quit keys stay live over them; forms capture
	// 'q' as input and are dismissed with Esc instead.
	overlayIsModal bool
}

// Run builds the TUI over store and blocks until the user quits. It returns the
// Application's run error to the caller (never panics).
func Run(store *db.Store) error {
	t := newTUI(store)
	t.loadSync()
	return t.app.Run()
}

// newTUI constructs the application, screens, layout, and global key handling.
func newTUI(store *db.Store) *tui {
	t := &tui{
		app:    tview.NewApplication(),
		pages:  tview.NewPages(),
		store:  store,
		status: tview.NewTextView().SetDynamicColors(true),
	}

	t.dashboard = t.newDashboard()
	t.leadsTable = t.newLeadsScreen()
	t.contactsTable = t.newContactsScreen()
	t.dealsTable = t.newDealsScreen()

	t.pages.AddPage(pageDashboard, t.dashboard, true, true)
	t.pages.AddPage(pageLeads, t.leadsTable, true, false)
	t.pages.AddPage(pageContacts, t.contactsTable, true, false)
	t.pages.AddPage(pageDeals, t.dealsTable, true, false)

	t.status.SetText(" F1 Dashboard · F2 Leads · F3 Contacts · F4 Deals · n new · enter edit · q quit ")

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.pages, 0, 1, true).
		AddItem(t.status, 1, 0, false)

	t.app.SetInputCapture(t.globalKeys)
	t.app.SetRoot(root, true).EnableMouse(true)
	return t
}

// globalKeys handles page switching and quit. It is only active when no overlay
// (form/modal) is showing, so typing in a form doesn't switch pages.
func (t *tui) globalKeys(event *tcell.EventKey) *tcell.EventKey {
	if t.pages.HasPage(pageOverlay) {
		// A text form needs 'q' as input, so it owns every key (Esc cancels it).
		// A button-only modal has no text entry, so quit stays available there.
		if t.overlayIsModal && (event.Key() == tcell.KeyCtrlC || event.Rune() == 'q') {
			t.app.Stop()
			return nil
		}
		return event // let the overlay handle its own keys
	}
	switch event.Key() {
	case tcell.KeyF1:
		t.show(pageDashboard, t.dashboard)
		return nil
	case tcell.KeyF2:
		t.show(pageLeads, t.leadsTable)
		return nil
	case tcell.KeyF3:
		t.show(pageContacts, t.contactsTable)
		return nil
	case tcell.KeyF4:
		t.show(pageDeals, t.dealsTable)
		return nil
	case tcell.KeyCtrlC:
		t.app.Stop()
		return nil
	}
	if event.Rune() == 'q' {
		t.app.Stop()
		return nil
	}
	return event
}

// show switches to a page and focuses its primitive.
func (t *tui) show(page string, focus tview.Primitive) {
	t.pages.SwitchToPage(page)
	t.app.SetFocus(focus)
}

// loadSync reads all data and fills the screens. Safe to call before Run (off
// the event loop); for post-mutation refreshes use refreshAsync.
func (t *tui) loadSync() {
	leads, _ := t.store.ListLeads("")
	contacts, _ := t.store.SearchContacts("")
	deals, _ := t.store.ListDeals(db.DealFilter{})
	summary, _ := t.store.PipelineSummary()
	t.setLeads(leads)
	t.setContacts(contacts)
	t.setDeals(deals)
	t.setSummary(summary)
}

// mutate runs a store mutation off the event loop. On success it reloads all
// data, dismisses any overlay, and focuses the given page — all on the loop. On
// failure it flashes the error. This is the single write path for every screen,
// keeping DB work off the event loop per the tview rules.
func (t *tui) mutate(page string, focus tview.Primitive, op func() error) {
	go func() {
		if err := op(); err != nil {
			t.app.QueueUpdateDraw(func() { t.flashError(err) })
			return
		}
		leads, _ := t.store.ListLeads("")
		contacts, _ := t.store.SearchContacts("")
		deals, _ := t.store.ListDeals(db.DealFilter{})
		summary, _ := t.store.PipelineSummary()
		t.app.QueueUpdateDraw(func() {
			t.setLeads(leads)
			t.setContacts(contacts)
			t.setDeals(deals)
			t.setSummary(summary)
			if t.pages.HasPage(pageOverlay) {
				t.pages.RemovePage(pageOverlay)
				t.overlayIsModal = false
			}
			t.show(page, focus)
		})
	}()
}

func (t *tui) setLeads(leads []models.Lead) {
	t.leads = leads
	fillTable(t.leadsTable, leadRows(leads))
}

func (t *tui) setContacts(contacts []models.Contact) {
	t.contacts = contacts
	fillTable(t.contactsTable, contactRows(contacts))
}

func (t *tui) setDeals(deals []models.Deal) {
	t.deals = deals
	fillTable(t.dealsTable, dealRows(deals))
}

func (t *tui) setSummary(s models.PipelineSummary) {
	var text string
	for _, line := range summaryLines(s) {
		text += line + "\n"
	}
	t.dashboard.SetText(text)
}

// fillTable replaces a table's contents with rows[0] as a fixed header.
func fillTable(table *tview.Table, rows [][]string) {
	table.Clear()
	for r, row := range rows {
		for c, cell := range row {
			tc := tview.NewTableCell(cell)
			if r == 0 {
				tc.SetSelectable(false).SetAttributes(tcell.AttrBold)
			}
			table.SetCell(r, c, tc)
		}
	}
}

// newListTable builds a selectable table with a frozen header row.
func newListTable(title string) *tview.Table {
	table := tview.NewTable().SetBorders(false).SetSelectable(true, false).SetFixed(1, 0)
	table.SetBorder(true).SetTitle(" " + title + " ")
	table.Select(1, 0)
	return table
}

// closeOverlay removes the transient form/modal and returns focus to page.
func (t *tui) closeOverlay(page string, focus tview.Primitive) {
	t.pages.RemovePage(pageOverlay)
	t.overlayIsModal = false
	t.show(page, focus)
}

// showOverlay layers a transient text form centered over the current page. The
// form captures every key (so 'q' types into fields); Esc cancels it.
func (t *tui) showOverlay(p tview.Primitive, width, height int) {
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
	t.overlayIsModal = false
	t.pages.AddPage(pageOverlay, modal, true, true)
	t.app.SetFocus(p)
}

// showModal layers a button-only modal (a menu with no text entry). Unlike a
// form, the global quit keys ('q' / Ctrl-C) stay live over it.
func (t *tui) showModal(modal *tview.Modal) {
	t.overlayIsModal = true
	t.pages.AddPage(pageOverlay, modal, true, true)
	t.app.SetFocus(modal)
}

// flashError shows an error in the status bar (handlers run on the event loop).
func (t *tui) flashError(err error) {
	if err != nil {
		t.status.SetText(" [red]" + err.Error() + "[-] ")
	}
}
