package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// seededStore returns a store with one company, one lead, one contact (linked to
// the company), and one deal.
func seededStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	globex, err := store.CreateCompany(models.Company{Name: "Globex"})
	if err != nil {
		t.Fatalf("seed company: %v", err)
	}
	if _, err := store.CreateLead(models.Lead{Name: "Zara", Source: models.SourceWeb}); err != nil {
		t.Fatalf("seed lead: %v", err)
	}
	c, err := store.CreateContact(models.Contact{Name: "Quentin", CompanyID: globex.ID})
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

	// Dashboard is the landing section (reached via the sidebar, section 1).
	waitFor(t, ti, sim, "LEADS")
	waitFor(t, ti, sim, "proposal")

	// Numbered sections — no F-keys.
	sim.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	waitFor(t, ti, sim, "Zara")

	sim.InjectKey(tcell.KeyRune, '3', tcell.ModNone)
	waitFor(t, ti, sim, "Quentin")
	waitFor(t, ti, sim, "Globex") // contact's CompanyID resolves to the company name

	sim.InjectKey(tcell.KeyRune, '4', tcell.ModNone)
	waitFor(t, ti, sim, "Megadeal")

	sim.InjectKey(tcell.KeyRune, '5', tcell.ModNone)
	waitFor(t, ti, sim, "Globex") // Companies section lists the company
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
	sim.InjectKey(tcell.KeyRune, '4', tcell.ModNone)
	waitFor(t, ti, sim, "Megadeal")
	sim.InjectKey(tcell.KeyRune, 's', tcell.ModNone) // open stage picker modal
	waitFor(t, ti, sim, "which stage")

	sim.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)
	if err := <-runErr; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// TestTUIFormCapturesQuit proves the inverse: inside a text form 'q' is input,
// not quit, so quitting can't clobber typing.
func TestTUIFormCapturesQuit(t *testing.T) {
	t.Parallel()
	ti, sim, runErr := runTUI(t)

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
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
}

// TestTUIHelpOverlay proves `?` opens the help overlay and Esc closes it.
func TestTUIHelpOverlay(t *testing.T) {
	t.Parallel()
	ti, sim, _ := runTUI(t)

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '?', tcell.ModNone)
	waitFor(t, ti, sim, "microapp-crm — keys")

	sim.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	waitForGone(t, ti, sim, "microapp-crm — keys")
}
