package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// seededStore returns a store with one lead, one contact, and one deal.
func seededStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.CreateLead(models.Lead{Name: "Zara", Source: models.SourceWeb}); err != nil {
		t.Fatalf("seed lead: %v", err)
	}
	c, err := store.CreateContact(models.Contact{Name: "Quentin"})
	if err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	if _, err := store.CreateDeal(models.Deal{Title: "Megadeal", ContactID: c.ID, Stage: models.StageProposal}); err != nil {
		t.Fatalf("seed deal: %v", err)
	}
	return store
}

// screenText flattens the simulation screen into newline-joined rows.
func screenText(sim tcell.SimulationScreen) string {
	cells, w, h := sim.GetContents()
	var b strings.Builder
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			c := cells[row*w+col]
			if len(c.Runes) > 0 && c.Runes[0] != 0 {
				b.WriteRune(c.Runes[0])
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// snapshot reads the rendered screen ON the event loop, so the read is
// serialized with tview's draws (reading from the test goroutine would race the
// draw writing to the same cell buffer).
func snapshot(ti *tui, sim tcell.SimulationScreen) string {
	ch := make(chan string, 1)
	ti.app.QueueUpdateDraw(func() { ch <- screenText(sim) })
	return <-ch
}

// waitFor polls the screen (synchronizing through the event loop) until it
// contains want, failing the test if it never does. No sleeps.
func waitFor(t *testing.T, ti *tui, sim tcell.SimulationScreen, want string) {
	t.Helper()
	var last string
	for i := 0; i < 200; i++ {
		last = snapshot(ti, sim)
		if strings.Contains(last, want) {
			return
		}
	}
	t.Fatalf("timed out waiting for %q; screen was:\n%s", want, last)
}

func TestTUINavigationAndRender(t *testing.T) {
	t.Parallel()
	ti := newTUI(seededStore(t))
	ti.loadSync()

	sim := tcell.NewSimulationScreen("UTF-8")
	ti.app.SetScreen(sim)
	sim.SetSize(120, 40)

	runErr := make(chan error, 1)
	go func() { runErr <- ti.app.Run() }()
	t.Cleanup(func() {
		ti.app.Stop()
		<-runErr
	})

	// Dashboard is the landing screen.
	waitFor(t, ti, sim, "LEADS")
	waitFor(t, ti, sim, "proposal")

	// F2 -> Leads shows the seeded lead.
	sim.InjectKey(tcell.KeyF2, 0, tcell.ModNone)
	waitFor(t, ti, sim, "Zara")

	// F3 -> Contacts.
	sim.InjectKey(tcell.KeyF3, 0, tcell.ModNone)
	waitFor(t, ti, sim, "Quentin")

	// F4 -> Deals.
	sim.InjectKey(tcell.KeyF4, 0, tcell.ModNone)
	waitFor(t, ti, sim, "Megadeal")
}

func TestTUIQuit(t *testing.T) {
	t.Parallel()
	ti := newTUI(seededStore(t))
	ti.loadSync()

	sim := tcell.NewSimulationScreen("UTF-8")
	ti.app.SetScreen(sim)
	sim.SetSize(100, 30)

	runErr := make(chan error, 1)
	go func() { runErr <- ti.app.Run() }()

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)

	if err := <-runErr; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// TestTUIQuitFromModal proves 'q' still quits while a button-only modal (here the
// deal stage picker) is open — these menus have no text entry, so quit stays live.
func TestTUIQuitFromModal(t *testing.T) {
	t.Parallel()
	ti := newTUI(seededStore(t))
	ti.loadSync()

	sim := tcell.NewSimulationScreen("UTF-8")
	ti.app.SetScreen(sim)
	sim.SetSize(120, 40)

	runErr := make(chan error, 1)
	go func() { runErr <- ti.app.Run() }()

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyF4, 0, tcell.ModNone)
	waitFor(t, ti, sim, "Megadeal")
	sim.InjectKey(tcell.KeyRune, 's', tcell.ModNone) // open stage picker modal
	waitFor(t, ti, sim, "which stage")

	sim.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)
	if err := <-runErr; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// TestTUIFormCapturesQuit proves the inverse: inside a text form 'q' is input,
// not quit, and Esc is what backs out — so quitting can't clobber typing.
func TestTUIFormCapturesQuit(t *testing.T) {
	t.Parallel()
	ti := newTUI(seededStore(t))
	ti.loadSync()

	sim := tcell.NewSimulationScreen("UTF-8")
	ti.app.SetScreen(sim)
	sim.SetSize(120, 40)

	runErr := make(chan error, 1)
	go func() { runErr <- ti.app.Run() }()
	t.Cleanup(func() { ti.app.Stop(); <-runErr })

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyF2, 0, tcell.ModNone)
	waitFor(t, ti, sim, "Zara")
	sim.InjectKey(tcell.KeyRune, 'n', tcell.ModNone) // open new-lead form
	waitFor(t, ti, sim, "New Lead")

	// 'q' must not quit while a form owns the keys.
	sim.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)
	select {
	case err := <-runErr:
		t.Fatalf("'q' quit the app from inside a form (err=%v); should be captured as input", err)
	default:
	}

	// Esc is the documented way back out, and it must close the overlay.
	sim.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	for i := 0; i < 200; i++ {
		if !overlayPresent(ti) {
			return
		}
	}
	t.Fatal("Esc did not close the form overlay")
}

// overlayPresent reports whether the transient overlay page exists, reading it
// ON the event loop so the check is serialized with tview's mutations.
func overlayPresent(ti *tui) bool {
	ch := make(chan bool, 1)
	ti.app.QueueUpdate(func() { ch <- ti.pages.HasPage(pageOverlay) })
	return <-ch
}
