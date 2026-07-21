# Leadzaar

A local-first, single-user sales CRM in one self-contained Go binary. Capture **Leads**, convert the
good ones into **Contacts**, and track opportunities as **Deals** through a pipeline — all stored in
a single embedded [bbolt](https://github.com/etcd-io/bbolt) file, no server and no network.

The same data is reachable through two surfaces, selected at launch:

- a **tview** terminal UI for the human operator, and
- an **MCP stdio server** so an AI assistant can read and update the CRM.

The two modes can run concurrently as separate local processes against the same file: persistence
opens bbolt [per operation](docs/bbolt-concurrent-access-strategy.md) so no process holds the lock
while idle, and the TUI auto-refreshes when the MCP process writes. The full design lives in
[`docs/SPECIFICATIONS.md`](docs/SPECIFICATIONS.md) — the source of truth for what this app is.

## Getting Started

Requires Go 1.26+. Two dev tools are used for formatting and linting:

```sh
go install mvdan.cc/gofumpt@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

### Build, run, test, lint

```sh
make build        # go build ./...
make run          # go run . (defaults to -mode tui)
go run . -mode mcp  # start the MCP stdio server
make test         # go test ./... -race -cover
make fmt          # gofumpt -w .
make lint         # golangci-lint run
make check        # fmt + tidy + lint + test
```

> The surfaces are not implemented yet — this is the initial scaffold. `make build` / `make test`
> are green from day one; running a mode currently reports "not yet implemented".

`make check` (fmt + tidy + lint + test) mirrors CI and is the gate to run before pushing.

### Database location

Both surfaces resolve the bbolt file the same way, so the TUI and the MCP server share one database
no matter which directory you start them from:

1. the `-db` flag — `go run . -db /path/to/leadzaar.db`
2. the `LEADZAAR_DB` environment variable
3. the default `~/.local/leadzaar/default.db`

The parent directory is created on first run.

## Project layout

```
main.go            flag parsing; dispatches to the selected surface
internal/models    plain domain structs (Lead, Contact, Deal) + enums — no persistence imports
internal/db        the Store — the only bbolt-aware package; all CRUD, validation, indexes,
                   and cross-entity use-cases (lead conversion, contact cascade-delete)
internal/server    (planned) MCP stdio server, consuming internal/db
internal/tui       (planned) tview TUI, consuming internal/db
docs/              SPECIFICATIONS.md — the source of truth
.claude/rules/     layer conventions (bbolt, MCP, tview, testing, spec) read by Claude Code
```

The layering rule: only `internal/db` touches bbolt; every other layer works with plain models.
Both surfaces consume the same `Store`, so business logic lives in one place. See
[`CLAUDE.md`](CLAUDE.md) for the full architecture and contributor guidance.
