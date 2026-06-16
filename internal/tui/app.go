package tui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// defaultPollEvery is how often the TUI checks whether another process (e.g. the
// MCP server) has written to the shared bbolt file, so the view stays fresh
// without a manual reload. See docs/bbolt-concurrent-access-strategy.md.
const defaultPollEvery = 1500 * time.Millisecond

// Body page names. Sections are swapped by SwitchToPage; transient forms/modals
// are layered on top.
const (
	pageDashboard = "dashboard"
	pageLeads     = "leads"
	pageContacts  = "contacts"
	pageDeals     = "deals"
	pageCompanies = "companies"
	pageOffers    = "offers"
	pageForm      = "form"    // full-screen create/edit form
	pageOverlay   = "overlay" // centered modal / help over the body
)

// Layout constants for the shared sidebar·body·status skeleton.
const (
	sidebarWidth         = 20
	minWidth             = 80
	minHeight            = 24
	sidebarCollapseWidth = 100 // auto-collapse the sidebar below this width
)

// overlayKind tracks which transient layer (if any) currently owns input, so
// the global key handler routes events correctly.
type overlayKind int

const (
	ovNone overlayKind = iota
	ovForm
	ovModal
	ovHelp
)

// section is one top-level navigable area (dashboard or an entity list). The tui
// drives the shared chrome (sidebar count, header, status zones) through it.
type section interface {
	primitive() tview.Primitive
	focus()
	rowContext() string // status-bar context zone
	keyHints() string   // status-bar key-hint zone (without "? help")
	total() int         // record count for the sidebar badge / header
	focusables() []tview.Primitive
}

type sectionEntry struct {
	page  string
	title string
	sec   section
}

// tui owns the single Application and the whole widget tree. All data is pulled
// through store; no bbolt access or business logic lives here.
type tui struct {
	app   *tview.Application
	store *db.Store

	layout *appLayout
	middle *tview.Flex
	right  *tview.Flex
	body   *tview.Pages
	header *tview.TextView

	sidebar  *tview.List
	ctxZone  *tview.TextView
	msgZone  *tview.TextView
	hintZone *tview.TextView

	dash      *dashboardScreen
	leads     *listScreen[models.Lead]
	contacts  *listScreen[models.Contact]
	deals     *listScreen[models.Deal]
	companies *listScreen[models.Company]
	offers    *listScreen[models.Offer]

	// companyNames maps CompanyID→name for resolving links in lists/details,
	// refreshed on every applyData. Read by the render closures via companyName.
	companyNames map[uint64]string

	sections []sectionEntry
	active   int

	overlay     overlayKind
	prevOverlay overlayKind
	overlayForm *formView

	sidebarCollapsed bool          // manual Ctrl-B toggle
	autoCollapsed    bool          // narrow-terminal auto-collapse
	inFlight         bool          // a mutation is running off the event loop
	pollEvery        time.Duration // cross-process refresh interval (0 disables)
}

// appLayout is the root primitive. It embeds the normal sidebar·body·status
// Flex but overrides Draw to show a "terminal too small" notice below the hard
// minimum and to auto-collapse the sidebar on narrow terminals.
type appLayout struct {
	*tview.Flex
	t        *tui
	tooSmall *tview.TextView
}

func (l *appLayout) Draw(screen tcell.Screen) {
	_, _, w, h := l.GetRect()
	if w < minWidth || h < minHeight {
		l.tooSmall.SetRect(l.GetRect())
		l.tooSmall.Draw(screen)
		return
	}
	l.t.applyResponsive(w)
	l.Flex.Draw(screen)
}

// Run builds the TUI over store and blocks until the user quits. It returns the
// Application's run error to the caller (never panics). A background goroutine
// watches the shared file for writes by another process and refreshes the view.
func Run(store *db.Store) error {
	t := newTUI(store)
	t.loadSync()
	stop := make(chan struct{})
	go t.watchExternal(stop)
	err := t.app.Run()
	close(stop)
	return err
}

// newTUI constructs the application, screens, layout, and global key handling.
func newTUI(store *db.Store) *tui {
	applyTheme()
	t := &tui{
		app:       tview.NewApplication(),
		store:     store,
		body:      tview.NewPages(),
		pollEvery: defaultPollEvery,
	}

	t.header = tview.NewTextView().SetDynamicColors(true)
	t.ctxZone = tview.NewTextView().SetDynamicColors(true)
	t.msgZone = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	t.hintZone = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight)

	t.sidebar = tview.NewList().ShowSecondaryText(false)
	t.sidebar.SetHighlightFullLine(true).SetBorder(true).SetTitle(" microapp-crm ")
	t.sidebar.SetSelectedFunc(func(i int, _, _ string, _ rune) { t.switchTo(i) })

	t.companyNames = map[uint64]string{}
	t.dash = newDashboard(t)
	t.leads = newLeadsScreen(t)
	t.contacts = newContactsScreen(t)
	t.deals = newDealsScreen(t)
	t.companies = newCompaniesScreen(t)
	t.offers = newOffersScreen(t)

	t.sections = []sectionEntry{
		{pageDashboard, "Dashboard", t.dash},
		{pageLeads, "Leads", t.leads},
		{pageContacts, "Contacts", t.contacts},
		{pageDeals, "Deals", t.deals},
		{pageCompanies, "Companies", t.companies},
		{pageOffers, "Offers", t.offers},
	}
	for i, e := range t.sections {
		t.body.AddPage(e.page, e.sec.primitive(), true, i == 0)
		t.sidebar.AddItem(fmt.Sprintf("%d  %s", i+1, e.title), "", rune('1'+i), nil)
	}

	// Right area: header above the swappable body.
	t.right = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.header, 1, 0, false).
		AddItem(t.body, 0, 1, true)

	t.middle = tview.NewFlex()
	t.rebuildMiddle()

	statusBar := tview.NewFlex().
		AddItem(t.ctxZone, 0, 2, false).
		AddItem(t.msgZone, 0, 3, false).
		AddItem(t.hintZone, 0, 3, false)

	normal := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.middle, 0, 1, true).
		AddItem(statusBar, 1, 0, false)

	tooSmall := tview.NewTextView().SetTextAlign(tview.AlignCenter).
		SetText(fmt.Sprintf("\nTerminal too small — need %d×%d\n", minWidth, minHeight))
	t.layout = &appLayout{Flex: normal, t: t, tooSmall: tooSmall}

	t.app.SetInputCapture(t.globalKeys)
	t.app.SetRoot(t.layout, true).EnableMouse(true).EnablePaste(true)
	t.active = 0
	t.sidebar.SetCurrentItem(0)
	t.sections[0].sec.focus()
	t.refreshChrome()
	return t
}

// rebuildMiddle lays out sidebar + right area, honoring the collapse state.
func (t *tui) rebuildMiddle() {
	t.middle.Clear()
	if !t.collapsed() {
		t.middle.AddItem(t.sidebar, sidebarWidth, 0, false)
	}
	t.middle.AddItem(t.right, 0, 1, true)
}

func (t *tui) collapsed() bool { return t.sidebarCollapsed || t.autoCollapsed }

// applyResponsive auto-collapses the sidebar on narrow terminals. Called from
// the layout's Draw, so it only rebuilds on a width-threshold transition.
func (t *tui) applyResponsive(w int) {
	auto := w < sidebarCollapseWidth
	if auto != t.autoCollapsed {
		t.autoCollapsed = auto
		t.rebuildMiddle()
	}
}

func (t *tui) toggleSidebar() {
	t.sidebarCollapsed = !t.sidebarCollapsed
	t.rebuildMiddle()
	if t.collapsed() {
		t.sections[t.active].sec.focus()
	} else {
		t.app.SetFocus(t.sidebar)
	}
}

// sectionIndex returns the index of the section with the given page name, or -1.
func (t *tui) sectionIndex(page string) int {
	for i, e := range t.sections {
		if e.page == page {
			return i
		}
	}
	return -1
}

// gotoLead jumps to the Leads section and highlights the lead with leadID,
// clearing any active filter so the target is visible. Used by the Offers
// section's `l` action to follow an offer back to its lead.
func (t *tui) gotoLead(leadID uint64) {
	i := t.sectionIndex(pageLeads)
	if i < 0 {
		return
	}
	t.switchTo(i)
	t.leads.focusByID(leadID)
}

// switchTo activates section i: shows its page, highlights the sidebar, focuses
// its primary widget, and refreshes the chrome.
func (t *tui) switchTo(i int) {
	if i < 0 || i >= len(t.sections) {
		return
	}
	t.active = i
	t.body.SwitchToPage(t.sections[i].page)
	t.sidebar.SetCurrentItem(i)
	t.sections[i].sec.focus()
	t.refreshChrome()
}

// globalKeys is the single app-wide input capture. It routes by overlay state,
// so typing in a form or filter never triggers a navigation action.
func (t *tui) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch t.overlay {
	case ovHelp:
		if ev.Key() == tcell.KeyEscape || ev.Rune() == '?' || ev.Rune() == 'q' {
			t.closeOverlay()
		}
		return nil
	case ovForm:
		switch ev.Key() {
		case tcell.KeyCtrlC:
			t.requestQuit()
			return nil
		case tcell.KeyCtrlS:
			t.overlayForm.trySave()
			return nil
		case tcell.KeyTab:
			t.overlayForm.focusNext()
			return nil
		case tcell.KeyBacktab:
			t.overlayForm.focusPrev()
			return nil
		case tcell.KeyEscape:
			t.overlayForm.cancel()
			return nil
		}
		return ev
	case ovModal:
		if ev.Key() == tcell.KeyCtrlC || ev.Rune() == 'q' {
			t.requestQuit()
			return nil
		}
		return ev
	}

	// No overlay. While a filter input is focused, only the global chords act;
	// everything else is text input.
	if t.inTextEntry() {
		switch ev.Key() {
		case tcell.KeyCtrlB:
			t.toggleSidebar()
			return nil
		case tcell.KeyCtrlC:
			t.requestQuit()
			return nil
		}
		return ev
	}

	switch ev.Key() {
	case tcell.KeyCtrlB:
		t.toggleSidebar()
		return nil
	case tcell.KeyCtrlC:
		t.requestQuit()
		return nil
	case tcell.KeyTab:
		t.cycleFocus(1)
		return nil
	case tcell.KeyBacktab:
		t.cycleFocus(-1)
		return nil
	}
	switch ev.Rune() {
	case '?':
		t.showHelp()
		return nil
	case 'q':
		t.requestQuit()
		return nil
	}
	if r := ev.Rune(); r >= '1' && r <= '9' {
		t.switchTo(int(r - '1'))
		return nil
	}
	return ev
}

// inTextEntry reports whether a text input (a filter bar) currently has focus.
func (t *tui) inTextEntry() bool {
	_, ok := t.app.GetFocus().(*tview.InputField)
	return ok
}

// cycleFocus moves focus across the regions: sidebar ↔ table ↔ detail pane.
func (t *tui) cycleFocus(delta int) {
	order := []tview.Primitive{}
	if !t.collapsed() {
		order = append(order, t.sidebar)
	}
	order = append(order, t.sections[t.active].sec.focusables()...)
	if len(order) == 0 {
		return
	}
	cur := t.app.GetFocus()
	idx := 0
	for i, p := range order {
		if p == cur {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(order)) % len(order)
	t.app.SetFocus(order[idx])
}

// requestQuit exits, prompting first if a form is dirty or a write is mid-flight.
func (t *tui) requestQuit() {
	switch {
	case t.overlay == ovForm && t.overlayForm.dirty:
		t.confirm("Quit", "Discard changes and quit? [y/N]", false, t.app.Stop)
	case t.inFlight:
		t.confirm("Quit", "An operation is in progress. Quit anyway? [y/N]", false, t.app.Stop)
	default:
		t.app.Stop()
	}
}

// --- chrome refresh -------------------------------------------------------

// refreshChrome repaints the sidebar badges, the header, and the status context
// and hint zones for the active section. The message zone is left untouched.
func (t *tui) refreshChrome() {
	for i, e := range t.sections {
		label := fmt.Sprintf("%d  %s", i+1, e.title)
		if n := e.sec.total(); n > 0 {
			label = fmt.Sprintf("%d  %-9s %d", i+1, e.title, n)
		}
		t.sidebar.SetItemText(i, label, "")
	}
	active := t.sections[t.active]
	t.header.SetText(fmt.Sprintf(" [::b]%s[::-]   %d records", active.title, active.sec.total()))
	t.ctxZone.SetText(" " + active.sec.rowContext())
	t.hintZone.SetText(active.sec.keyHints() + " · ? help ")
}

func (t *tui) setMessage(s string) {
	if s == "" {
		t.msgZone.SetText("")
		return
	}
	t.msgZone.SetText(s)
}

// --- data loading & mutation ---------------------------------------------

// dataSnapshot is a full read of every screen's data, gathered off the event loop.
type dataSnapshot struct {
	leads        []models.Lead
	leadsErr     error
	contacts     []models.Contact
	contactsErr  error
	deals        []models.Deal
	dealsErr     error
	companies    []models.Company
	companiesErr error
	offers       []models.Offer
	offersErr    error
	summary      models.PipelineSummary
	summaryErr   error
}

// fetchAll reads everything from the store. Safe to call off the event loop.
func (t *tui) fetchAll() dataSnapshot {
	var s dataSnapshot
	s.leads, s.leadsErr = t.store.ListLeads("")
	s.contacts, s.contactsErr = t.store.SearchContacts("")
	s.deals, s.dealsErr = t.store.ListDeals(db.DealFilter{})
	s.companies, s.companiesErr = t.store.ListCompanies()
	s.offers, s.offersErr = t.store.ListOffers(db.OfferFilter{})
	s.summary, s.summaryErr = t.store.PipelineSummary()
	return s
}

// applyData pushes a dataSnapshot into the screens. Runs on the event loop. The
// company-name resolver map is rebuilt first so the lead/contact/deal renderers
// resolve their links against the freshest company set.
func (t *tui) applyData(s dataSnapshot) {
	names := make(map[uint64]string, len(s.companies))
	for _, c := range s.companies {
		names[c.ID] = c.Name
	}
	t.companyNames = names

	t.leads.setItems(s.leads, s.leadsErr)
	t.contacts.setItems(s.contacts, s.contactsErr)
	t.deals.setItems(s.deals, s.dealsErr)
	t.companies.setItems(s.companies, s.companiesErr)
	t.offers.setItems(s.offers, s.offersErr)
	t.dash.set(s.summary, s.summaryErr)
	t.refreshChrome()
}

// loadSync does the initial synchronous load before Run (off the event loop).
func (t *tui) loadSync() { t.applyData(t.fetchAll()) }

// mutate runs a store write off the event loop, then reloads and reports the
// outcome in the status bar. On success it closes an open form. This is the
// single write path for every screen, keeping DB work off the event loop.
func (t *tui) mutate(op func() error) {
	t.setMessage("[" + colorWarn + "]working…[-]")
	t.inFlight = true
	go func() {
		if err := op(); err != nil {
			t.app.QueueUpdateDraw(func() {
				t.inFlight = false
				t.setMessage("[" + colorError + "]✗ " + err.Error() + "[-]")
			})
			return
		}
		data := t.fetchAll()
		t.app.QueueUpdateDraw(func() {
			t.inFlight = false
			t.applyData(data)
			if t.overlay == ovForm {
				t.closeForm()
			}
			t.setMessage("[" + colorSuccess + "]✓ saved[-]")
		})
	}()
}

// watchExternal polls the store's transaction ID and, when another process has
// committed a write to the shared file, re-reads the data and refreshes the
// screen — the connection-per-operation companion to a manual `r` reload (see
// docs/bbolt-concurrent-access-strategy.md). lastApplied is owned solely by this
// goroutine; the overlay/in-flight checks read event-loop state and so run only
// inside the queued closure. It returns when stop is closed (on app exit).
func (t *tui) watchExternal(stop <-chan struct{}) {
	if t.pollEvery <= 0 {
		return
	}
	ticker := time.NewTicker(t.pollEvery)
	defer ticker.Stop()
	lastApplied, _ := t.store.TxID() // baseline; a transient error just defers detection
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			id, err := t.store.TxID()
			if err != nil || id == lastApplied {
				continue
			}
			data := t.fetchAll() // slow read off the event loop
			applied := make(chan bool, 1)
			t.app.QueueUpdateDraw(func() {
				// Don't disturb an open form or an in-flight local mutation;
				// leave lastApplied so the refresh retries once they clear.
				if t.overlay == ovForm || t.inFlight {
					applied <- false
					return
				}
				t.applyData(data)
				t.setMessage("[" + colorWarn + "]↻ updated from another process[-]")
				applied <- true
			})
			if <-applied {
				lastApplied = id
			}
		}
	}
}

// reload re-reads all data off the event loop (the `r` action).
func (t *tui) reload() {
	t.setMessage("[" + colorWarn + "]reloading…[-]")
	go func() {
		data := t.fetchAll()
		t.app.QueueUpdateDraw(func() {
			t.applyData(data)
			t.setMessage("[" + colorSuccess + "]✓ reloaded[-]")
		})
	}()
}

// --- overlays: forms, modals, help ---------------------------------------

// openForm shows a full-screen form in the body (the section is hidden).
func (t *tui) openForm(f *formView) {
	t.overlay = ovForm
	t.overlayForm = f
	t.body.AddPage(pageForm, f.root, true, false)
	t.body.SwitchToPage(pageForm)
	t.app.SetFocus(f.order[0])
	t.ctxZone.SetText(" form")
	t.hintZone.SetText("Ctrl-S save · Esc cancel ")
	t.setMessage("")
}

// closeForm tears the form down and returns to the active section.
func (t *tui) closeForm() {
	t.body.SwitchToPage(t.sections[t.active].page)
	t.body.RemovePage(pageForm)
	t.overlay = ovNone
	t.overlayForm = nil
	t.sections[t.active].sec.focus()
	t.refreshChrome()
}

// openModal layers a centered modal over the body, remembering the prior overlay
// so a confirm raised from a form returns to that form on cancel.
func (t *tui) openModal(modal tview.Primitive) {
	t.prevOverlay = t.overlay
	t.overlay = ovModal
	t.body.AddPage(pageOverlay, modal, true, true)
	t.app.SetFocus(modal)
}

// closeOverlay removes the modal/help layer and restores focus.
func (t *tui) closeOverlay() {
	t.body.RemovePage(pageOverlay)
	t.overlay = t.prevOverlay
	t.prevOverlay = ovNone
	if t.overlay == ovForm && t.overlayForm != nil {
		t.app.SetFocus(t.overlayForm.order[0])
	} else {
		t.sections[t.active].sec.focus()
		t.refreshChrome()
	}
}

// confirm shows a Yes/No modal whose focus defaults to the safe choice (Cancel).
// y / Enter-on-Yes confirms; n / Esc cancels.
func (t *tui) confirm(_ /*title*/, message string, _ /*danger*/ bool, onYes func()) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"Cancel", "Yes"})
	modal.SetDoneFunc(func(_ int, label string) {
		t.closeOverlay()
		if label == "Yes" {
			onYes()
		}
	})
	modal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Rune() {
		case 'y', 'Y':
			t.closeOverlay()
			onYes()
			return nil
		case 'n', 'N':
			t.closeOverlay()
			return nil
		}
		return ev
	})
	t.openModal(modal)
}

// showModal layers an arbitrary button-only modal (e.g. the stage picker).
func (t *tui) showModal(modal *tview.Modal) { t.openModal(modal) }

// newModal builds a button-only modal with a done handler.
func newModal(text string, buttons []string, done func(int, string)) *tview.Modal {
	return tview.NewModal().SetText(text).AddButtons(buttons).SetDoneFunc(done)
}

// confirmDeleteText composes a delete-confirm message that names the target and
// warns the action cannot be undone. A single target names it; a batch counts it.
func confirmDeleteText(noun string, count int, name string) string {
	if count == 1 {
		return fmt.Sprintf("Delete %s %q?\nThis cannot be undone.", noun, name)
	}
	return fmt.Sprintf("Delete %d %ss?\nThis cannot be undone.", count, noun)
}
