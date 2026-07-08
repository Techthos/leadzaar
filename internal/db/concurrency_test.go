package db_test

import (
	"path/filepath"
	"testing"

	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

// TestTwoStoresShareOneFile proves the connection-per-operation strategy: two
// independent Stores opened on the same file (standing in for the TUI and MCP
// processes) can both read and write it, and a write through one is visible to
// the other. The old single-handle model would have timed out on the second
// Open. See docs/bbolt-concurrent-access-strategy.md.
func TestTwoStoresShareOneFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "crm.db")

	a, err := db.Open(path)
	if err != nil {
		t.Fatalf("open store A: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	// Second Open on the same live file must succeed — no lock is held between
	// operations, so this no longer times out.
	b, err := db.Open(path)
	if err != nil {
		t.Fatalf("open store B on the same file: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	// Write through A, read through B.
	lead, err := a.CreateLead(models.Lead{Name: "Ada", Source: models.SourceWeb})
	if err != nil {
		t.Fatalf("A.CreateLead: %v", err)
	}
	got, err := b.GetLead(lead.ID)
	if err != nil {
		t.Fatalf("B.GetLead: %v", err)
	}
	if got.Name != "Ada" {
		t.Errorf("B.GetLead name = %q, want %q", got.Name, "Ada")
	}

	// And the reverse: write through B, read through A.
	c, err := b.CreateContact(models.Contact{Name: "Bao"})
	if err != nil {
		t.Fatalf("B.CreateContact: %v", err)
	}
	if _, err := a.GetContact(c.ID); err != nil {
		t.Fatalf("A.GetContact after B wrote: %v", err)
	}
}

// TestTxIDAdvancesOnWrite proves the change-detection probe used by the TUI
// poll: TxID is stable across reads and strictly increases after a committed
// write, so a long-lived reader can detect another process's writes cheaply.
func TestTxIDAdvancesOnWrite(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, newClock())

	before, err := store.TxID()
	if err != nil {
		t.Fatalf("TxID before: %v", err)
	}

	// A pure read must not advance the transaction ID.
	if _, err := store.ListLeads(""); err != nil {
		t.Fatalf("ListLeads: %v", err)
	}
	steady, err := store.TxID()
	if err != nil {
		t.Fatalf("TxID after read: %v", err)
	}
	if steady != before {
		t.Errorf("TxID advanced on a read: before=%d after=%d", before, steady)
	}

	// A committed write must advance it.
	if _, err := store.CreateLead(models.Lead{Name: "Cy", Source: models.SourceReferral}); err != nil {
		t.Fatalf("CreateLead: %v", err)
	}
	after, err := store.TxID()
	if err != nil {
		t.Fatalf("TxID after write: %v", err)
	}
	if after <= before {
		t.Errorf("TxID did not advance after write: before=%d after=%d", before, after)
	}
}
