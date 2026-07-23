---
description: Conventions for the Makefile. Applies when creating or editing Makefile build targets.
paths:
  - Makefile
---

# Makefile rules

## Build output always goes under `./bin/`

- The `build` target must **fully build the binary** and place it at **`./bin/<binary-name>`**,
  where `<binary-name>` is the project's binary name (normally the repo/module directory name).
  This is always the `go build` output location:

  ```make
  build:
  	$(GO) build -o bin/<binary-name> .
  ```

  (`go build -o` creates `bin/` itself; no `mkdir` needed.)

- Never use a bare `go build ./...` as the `build` target — it only compiles and drops nothing
  (or, for a root `main` package, drops the binary in the repo root). The placed binary under
  `./bin/` is the contract.
- Never let any target write a binary to the repo root; every target that produces a binary
  (cross-compile helpers included) writes under `./bin/`.
- Keep `./bin/` gitignored (`/bin/` in `.gitignore`) — it is build output, never committed.

## Binaries are static (`CGO_ENABLED=0`)

- Every target that produces a binary sets `CGO_ENABLED=0`. These micro-apps are pure Go by
  design (embedded bbolt, no cgo), so nothing is lost — and the result is a statically linked
  binary that runs in minimal containers (Alpine/musl, distroless, scratch) and can be
  bind-mounted into an MCP host's container as-is:

  ```make
  build:
  	CGO_ENABLED=0 $(GO) build -o bin/<binary-name> .
  ```

- Rationale: a default (glibc-dynamic) build fails on musl images with a **misleading**
  `spawn /usr/local/bin/<binary-name> ENOENT` — the "missing file" is the ELF interpreter
  `/lib64/ld-linux-x86-64.so.2`, not the binary itself. After building, `file bin/<binary-name>`
  must say `statically linked`, never `dynamically linked`.
- This matches the release workflow (`build-and-release` skill), which already cross-compiles
  with `CGO_ENABLED=0` — local `make build` output must not behave differently from a release
  asset.
- If a project genuinely needs cgo (e.g. `mattn/go-sqlite3`), do not silently force
  `CGO_ENABLED=0` — prefer a pure-Go replacement (e.g. `modernc.org/sqlite`), or build against
  musl and document the exception with a comment in the Makefile.
