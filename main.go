// Command leadzaar is a local-first, single-user sales CRM.
//
// It runs as one self-contained binary backed by a single embedded bbolt file,
// and exposes the same data through two surfaces selected at launch:
//
//	leadzaar -mode tui   # interactive terminal UI (default)
//	leadzaar -mode mcp   # MCP stdio server for an AI assistant
//
// The two modes may run concurrently as separate processes against the same
// file: the store opens bbolt per operation (connection-per-operation) so no
// process holds the lock while idle. See docs/SPECIFICATIONS.md and
// docs/bbolt-concurrent-access-strategy.md for the full contract.
package main

import (
	"flag"
	"fmt"
	"os"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/server"
	"github.com/techthos/leadzaar/internal/tui"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "leadzaar:", err)
		os.Exit(1)
	}
}

// run parses flags and dispatches to the selected surface. It returns an error
// instead of exiting so it stays testable.
func run(args []string) error {
	fs := flag.NewFlagSet("leadzaar", flag.ContinueOnError)
	mode := fs.String("mode", "tui", "surface to start: tui | mcp")
	dbFlag := fs.String("db", "", "path to the bbolt database file (default $LEADZAAR_DB, else ~/.local/leadzaar/default.db)")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Println("leadzaar", version)
		return nil
	}

	// Resolution order: -db flag > $LEADZAAR_DB > ~/.local/leadzaar/default.db.
	// Both surfaces resolve it the same way, so TUI and MCP land on one file
	// without either having to be told where it is.
	dbPath := *dbFlag
	if dbPath == "" {
		resolved, err := db.DefaultPath()
		if err != nil {
			return err
		}
		dbPath = resolved
	}

	// The TUI and MCP server share one bbolt file. The store opens it per
	// operation, so the two surfaces can run as concurrent processes. Open the
	// store for the chosen surface and close it on exit.
	switch *mode {
	case "tui":
		store, err := db.Open(dbPath)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		return tui.Run(store)
	case "mcp":
		store, err := db.Open(dbPath)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		// stdio transport: the protocol owns stdout, so logs go to stderr only.
		return mcpserver.ServeStdio(server.New(store, version))
	default:
		return fmt.Errorf("unknown mode %q (want tui or mcp)", *mode)
	}
}
