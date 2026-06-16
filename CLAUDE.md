# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

**microapp-crm** is a local-first, single-user sales CRM that ships as **one self-contained Go
binary**. It works a `Lead → Contact → Deal` funnel, persisting everything to a single embedded
[bbolt](https://github.com/etcd-io/bbolt) file. The same data is exposed through two surfaces
selected at launch — a **tview TUI** and an **MCP stdio server**. They may run **concurrently as
separate local processes**: the Store opens bbolt **per operation** (connection-per-operation), so
no process holds the lock while idle. See `docs/bbolt-concurrent-access-strategy.md`.

This is a **spec-driven template**. `docs/SPECIFICATIONS.md` is the single source of truth for what
the app is. Implementation is currently partial: `internal/models` and `internal/db` (Leads,
Contacts, Deals) exist; `internal/tui` and `internal/server` are specified but not yet built, and
`main.go` reports `mode %q not yet implemented`.

## The local-only envelope (hard constraints)

These are non-negotiable product boundaries from the spec. Do not cross them without a spec change:

- **No** network of any kind: no web/REST/GraphQL server, no cloud sync, no message broker, no
  second process/daemon. The app must function fully offline.
- **No** multi-user features (no accounts, ownership, assignment, sharing).
- **No** currency conversion/FX — monetary totals are reported **per currency**, never summed across
  currencies.
- **No** separate Tasks/reminders entity and **no** general Interactions/activity-log entity in v1 —
  that context lives in freeform `notes`. **Company and Offer are first-class entities**, though:
  Leads, Contacts, and Deals optionally link to a Company by `CompanyID` (deleting a Company
  **unlinks**, never cascade-deletes), and a Lead has many **Offers** (email-style proposals: title,
  description, email subject, raw body) linked by `LeadID` (deleting a Lead **cascade-deletes** its
  Offers).
- **No** networked or cross-machine concurrency. Concurrent TUI + MCP access to the same file *is*
  supported, but only as **local processes on one machine** (connection-per-operation; brief
  per-operation locks, not high write contention).

## Spec is the contract

Read `.claude/rules/specification-rules.md`. Any change to observable product behavior (an
entity/field, bucket or key encoding, an MCP tool/resource/prompt, a TUI screen or nav path, a
use-case or acceptance criterion) is **not complete** until `docs/SPECIFICATIONS.md` reflects it —
**in the same commit**. Behavior-preserving refactors do not need a spec edit. If code and spec
disagree, reconcile it; never bury the drift. When unsure whether a change needs a spec update, it
probably does — ask.

The three spec-driven slash commands (`disable-model-invocation`, user-run only):
`/product-idea` (write the spec) → `/app-init` (scaffold against it) → `/app-spec-sync` (reconcile
code with spec). Treat `/app-spec-sync` as the audit that should always come back clean.

## Commands

Requires Go 1.26+. Two dev tools: `gofumpt` and `golangci-lint` v2 (see README for install).

```sh
make build          # go build ./...
make test           # go test ./... -race -cover   (tests must pass under -race)
make lint           # golangci-lint run
make fmt            # gofumpt -w .
make check          # fmt + tidy + lint + test  — run before considering work done
make run            # go run .  (defaults to -mode tui)
go run . -mode mcp  # start the MCP stdio server

go test -run TestName ./internal/db   # a single test
```

CI (`.github/workflows/ci.yml`) runs build + `go test -race -cover` and `golangci-lint` on push to
`main` and all PRs. `make check` mirrors it locally.

## Architecture

Strict layering. The dependency rule: **only `internal/db` imports bbolt**; everything else receives
plain domain models, never `*bolt.Tx` or transaction-scoped byte slices.

```
main.go            flag parsing → dispatch to a surface (TUI or MCP). Stays thin.
internal/models    plain domain structs (Lead, Contact, Deal) + enums. NO persistence imports.
internal/db        the Store: the only bbolt-aware package. All CRUD, validation, indexes,
                   and cross-entity use-cases (lead conversion, contact cascade-delete).
internal/server    MCP stdio server — mark3labs/mcp-go. Consumes internal/db.
internal/tui       tview TUI. Consumes internal/db.
```

Both surfaces consume the **same `internal/db` Store**, so business logic lives in one place. The
two can run concurrently as separate processes because the Store opens bbolt **per operation**
(connection-per-operation) — no process holds the lock while idle, and a short open `Timeout` with
backoff retry turns a brief collision into a sub-second wait. The TUI polls `Store.TxID()` and
refreshes when the MCP process writes. See `docs/bbolt-concurrent-access-strategy.md`.

### Persistence model (`internal/db`)

- One `Store` holds the file `path` (never a live `*bolt.DB`) plus an injectable `now func() time.Time`
  (`WithClock` option for deterministic tests). `Open` does a one-time read-write bootstrap that
  creates the file and runs the idempotent bucket migration; thereafter reads/writes open per
  operation via the internal `view`/`update` helpers. `Store.TxID()` exposes bbolt's committed txid
  for change detection.
- Every entity has a surrogate `uint64` ID from `Bucket.NextSequence()`, encoded **big-endian**
  (`itob`/`btoi`) so byte-sorted key order == creation order. List = cursor walk; newest-first =
  reverse walk.
- Values are `encoding/json`; `time.Time` is RFC3339. Models stay backward-compatible (additive
  fields) since old records persist on disk.
- Primary buckets: `leads`, `contacts`, `deals`, `companies`, `offers`. Index buckets:
  `idx_contact_by_email` (`lower(email)\x00` + BE contactID — composite key tolerates
  duplicate/optional emails), `idx_deal_by_contact` (BE contactID + BE dealID — prefix-scan drives
  both "deals for a contact" and the contact cascade delete), and `idx_offer_by_lead` (BE leadID + BE
  offerID — prefix-scan drives both "offers for a lead" and the lead cascade delete). Bucket names are
  package-level `[]byte` vars, never inline literals.
- **No status/stage indexes in v1** — filtering leads-by-status / deals-by-stage is an in-memory
  scan, acceptable for a single-user dataset. **Companies have no index either**: name search and the
  reverse "records linked to a company" scan (driving unlink-on-delete) are primary-bucket scans.
  Adding such an index is a spec change.
- Cross-entity operations run in **one `db.Update`** for atomicity: lead conversion
  (create contact, optional deal, back-reference the lead), contact delete (cascade-delete its
  deals + all index entries), lead delete (cascade-delete its offers + index entries), and company
  delete (unlink every referencing lead/contact/deal).
- Validation is enforced at the repository layer: non-empty `Name`/`Title`; valid `Source` /
  `LeadStatus` / `DealStage` enum (`.Valid()` on the enum type); `Deal.ContactID` must reference an
  existing contact; `Offer.LeadID` must reference an existing lead; a non-zero `CompanyID` on a
  lead/contact/deal must reference an existing company;
  `Lead.Quality`, when set, is `1`–`10` (`0` = unscored); non-zero `Deal.Value` requires a `Currency`.
  Use the `ErrNotFound` sentinel; match errors with `errors.Is`, never on string text.

## Layer-specific rules

Before working in a layer, read its rule file in `.claude/rules/` — they carry the binding
conventions and API guidance:

- `db-rules.md` — bbolt constraints, transactions, key encoding, the repository pattern, backups.
- `mcp-server.md` — `mark3labs/mcp-go` server construction, typed tool handlers, tool-error vs.
  transport-error semantics, stdio logging to **stderr only**, in-process client tests.
- `tui-rules.md` — `rivo/tview` Application/primitives and the concurrency model (DB work off the
  event loop, mutations back via `QueueUpdateDraw`).
- `go-testing.md` — black-box `_test.go` packages, table-driven subtests with `t.Parallel()`,
  got/want ordering, `go-cmp` for structs.
- `specification-rules.md` — the spec-is-the-contract rule above.
- `github-actions.md` — CI workflow conventions.
