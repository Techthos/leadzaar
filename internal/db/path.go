package db

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvDBPath is the environment variable that overrides the default database
// location. When set to a non-empty value it wins over the fallback path.
const EnvDBPath = "LEADZAAR_DB"

// DefaultPath returns the canonical Leadzaar database path: the value of
// LEADZAAR_DB when set, otherwise ~/.local/leadzaar/default.db. The directory is
// not created here — Open does that for whatever path it is handed.
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvDBPath); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "leadzaar", "default.db"), nil
}
