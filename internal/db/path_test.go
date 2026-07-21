package db_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/techthos/leadzaar/internal/db"
)

// TestDefaultPath covers the resolution order: LEADZAAR_DB when set, otherwise
// ~/.local/leadzaar/default.db. t.Setenv forbids t.Parallel here.
func TestDefaultPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	tests := []struct {
		name string
		env  string
		want string
	}{
		{
			name: "env override wins",
			env:  "/custom/leadzaar.db",
			want: "/custom/leadzaar.db",
		},
		{
			name: "empty env falls back to home",
			env:  "",
			want: filepath.Join(home, ".local", "leadzaar", "default.db"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(db.EnvDBPath, tc.env)

			got, err := db.DefaultPath()
			if err != nil {
				t.Fatalf("DefaultPath() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("DefaultPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOpenCreatesParentDirectory verifies Open materializes a missing directory
// chain, so the default ~/.local/leadzaar path works on a first run.
func TestOpenCreatesParentDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "deeper", "leadzaar.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := os.Stat(path); err != nil {
		t.Errorf("stat %q: %v", path, err)
	}
}
