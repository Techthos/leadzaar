package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// runTUI starts a seeded TUI on a simulation screen and returns it, the screen,
// and the channel carrying Run's exit, plus a teardown that stops it cleanly.
func runTUI(t *testing.T) (*tui, tcell.SimulationScreen, <-chan error) {
	t.Helper()
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
	return ti, sim, runErr
}

// waitForGone polls until the screen no longer contains text.
func waitForGone(t *testing.T, ti *tui, sim tcell.SimulationScreen, text string) {
	t.Helper()
	var last string
	for i := 0; i < 200; i++ {
		last = snapshot(ti, sim)
		if !strings.Contains(last, text) {
			return
		}
	}
	t.Fatalf("timed out waiting for %q to disappear; screen:\n%s", text, last)
}

func typeRunes(sim tcell.SimulationScreen, s string) {
	for _, r := range s {
		sim.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
}

func TestCreateLeadThroughForm(t *testing.T) {
	t.Parallel()
	ti, sim, _ := runTUI(t)

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	waitFor(t, ti, sim, "Zara")

	// Open the new-lead form; the Name field is focused first.
	sim.InjectKey(tcell.KeyRune, 'n', tcell.ModNone)
	waitFor(t, ti, sim, "New Lead")
	typeRunes(sim, "Newbie")

	// Ctrl-S saves (no Save button in the new forms).
	sim.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone)

	// The new lead appears in the table and the form is gone.
	waitFor(t, ti, sim, "Newbie")
	waitForGone(t, ti, sim, "New Lead")
}

func TestLeadFormCancel(t *testing.T) {
	t.Parallel()
	ti, sim, _ := runTUI(t)
	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	waitFor(t, ti, sim, "Zara")

	// A pristine form cancels straight back on Esc (no discard prompt).
	sim.InjectKey(tcell.KeyRune, 'n', tcell.ModNone)
	waitFor(t, ti, sim, "New Lead")
	sim.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	waitForGone(t, ti, sim, "New Lead")
}

func TestLeadFormBlocksSaveWhenInvalid(t *testing.T) {
	t.Parallel()
	ti, sim, _ := runTUI(t)
	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	waitFor(t, ti, sim, "Zara")

	// Save with an empty required Name is blocked and surfaces the field error.
	sim.InjectKey(tcell.KeyRune, 'n', tcell.ModNone)
	waitFor(t, ti, sim, "New Lead")
	sim.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone)
	waitFor(t, ti, sim, "Name is required")
	// The form is still open (save was blocked).
	waitFor(t, ti, sim, "New Lead")
}

func TestFilterNarrowsLeads(t *testing.T) {
	t.Parallel()
	ti, sim, _ := runTUI(t)
	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	waitFor(t, ti, sim, "Zara")

	// A filter that matches nothing hides the only lead; Esc clears it back.
	sim.InjectKey(tcell.KeyRune, '/', tcell.ModNone)
	typeRunes(sim, "nomatch")
	waitForGone(t, ti, sim, "Zara")
	sim.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	waitFor(t, ti, sim, "Zara")
}

func TestChangeDealStageThroughPicker(t *testing.T) {
	t.Parallel()
	ti, sim, _ := runTUI(t)

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '4', tcell.ModNone)
	waitFor(t, ti, sim, "Megadeal")
	waitFor(t, ti, sim, "proposal")

	// Open the stage picker and choose the first button (qualification).
	sim.InjectKey(tcell.KeyRune, 's', tcell.ModNone)
	waitFor(t, ti, sim, "which stage")
	sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, ti, sim, "qualification")
}

func TestDeleteContactThroughConfirm(t *testing.T) {
	t.Parallel()
	ti, sim, _ := runTUI(t)

	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '3', tcell.ModNone)
	waitFor(t, ti, sim, "Quentin")

	// `d` raises a confirm that names the cascade; `y` confirms.
	sim.InjectKey(tcell.KeyRune, 'd', tcell.ModNone)
	waitFor(t, ti, sim, "Delete contact")
	sim.InjectKey(tcell.KeyRune, 'y', tcell.ModNone)

	waitForGone(t, ti, sim, "Quentin")
}
