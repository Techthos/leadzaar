package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

// waitForWithin polls the screen up to attempts times (synchronizing through the
// event loop) until it contains want. Unlike waitFor it allows a larger budget,
// for changes that arrive via the background poll rather than a key injection.
func waitForWithin(t *testing.T, ti *tui, sim tcell.SimulationScreen, want string, attempts int) {
	t.Helper()
	var last string
	for i := 0; i < attempts; i++ {
		last = snapshot(ti, sim)
		if strings.Contains(last, want) {
			return
		}
	}
	t.Fatalf("timed out waiting for %q; screen was:\n%s", want, last)
}

// TestTUIRefreshesOnExternalWrite proves the cross-process refresh: while the TUI
// is running, a second Store (standing in for the MCP process) writes a new lead
// to the same file, and the background txid poll picks it up and repaints the
// list without any manual reload. See docs/bbolt-concurrent-access-strategy.md.
func TestTUIRefreshesOnExternalWrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "crm.db")

	store, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.CreateLead(models.Lead{Name: "Zara", Source: models.SourceWeb}); err != nil {
		t.Fatalf("seed lead: %v", err)
	}

	ti := newTUI(store)
	ti.pollEvery = time.Millisecond // tight loop so the test never waits on wall time
	ti.loadSync()

	sim := tcell.NewSimulationScreen("UTF-8")
	ti.app.SetScreen(sim)
	sim.SetSize(120, 40)

	runErr := make(chan error, 1)
	go func() { runErr <- ti.app.Run() }()
	stop := make(chan struct{})
	go ti.watchExternal(stop)
	t.Cleanup(func() {
		close(stop)
		ti.app.Stop()
		<-runErr
	})

	// Show the leads list so new rows are visible.
	waitFor(t, ti, sim, "LEADS")
	sim.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	waitFor(t, ti, sim, "Zara")

	// A separate process opens the same file and writes a lead.
	other, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open second store: %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	if _, err := other.CreateLead(models.Lead{Name: "Newcomer", Source: models.SourceReferral}); err != nil {
		t.Fatalf("external CreateLead: %v", err)
	}

	// The poll detects the external write and refreshes the list automatically.
	waitForWithin(t, ti, sim, "Newcomer", 4000)
}
