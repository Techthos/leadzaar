# microapp-crm — Specification

> This document is the **single source of truth** for what this app is. Code follows the spec;
> any scope change updates the spec **first** (see `.claude/rules/specification-rules.md`).

## Overview

**microapp-crm** is a local-first, single-user sales CRM that runs as one self-contained Go binary.
It helps a solo operator (freelancer, consultant, independent salesperson) work a simple funnel:
capture **Leads**, qualify and **convert** the good ones into **Contacts**, and track the money on
the table as **Deals** moving through a pipeline. There is no team, no web app, and no cloud — all
data lives in one embedded bbolt file owned by the process.

The app presents the same data through two surfaces: an interactive **tview TUI** for the human
operator, and an **MCP stdio server** so an AI assistant can read and update the CRM directly. The
two surfaces run as **alternate modes of the same binary** (selected at launch), never at the same
time, because bbolt holds a process-wide write lock.

## Goals & Non-Goals

### Goals
- Track the lead → contact → deal funnel for **one user** on **one machine**.
- Provide CRUD over Leads, Contacts, and Deals, plus a lead **conversion** action and a read-only
  **pipeline summary** (deal stages + lead funnel, value grouped by currency).
- Expose the model through both a **TUI** and an **MCP server**, each consuming the same repository
  layer.
- Keep all persistence in a single embedded **bbolt** file with no external dependencies.

### Non-Goals (the local-only envelope)
- ❌ **No** web server, REST/GraphQL API, network service, cloud sync, or message broker.
- ❌ **No** second binary, daemon, or background process; **no** internet access required to function.
- ❌ **No** multi-user / team features: no accounts, ownership, assignment, or sharing.
- ❌ **No** currency conversion / FX (there is no network) — monetary totals are reported **per
  currency**, never summed across currencies.
- ❌ **No** separate Tasks/reminders entity and **no** separate Interactions/activity-log entity in
  v1. Context is captured in a freeform `notes` field on each entity.
- ❌ **No** separate Organization entity in v1 — company is a plain string field on Lead/Contact.
- ❌ **No** concurrent TUI + MCP access to the same file (single-writer; run one mode at a time).

## Domain Model

Three entities. Every entity has a surrogate `uint64` ID (bbolt `NextSequence`), encoded big-endian
as its primary key so records sort in creation order. Email is **optional** and **non-unique**; it
is indexed only as a lookup/dedup hint, never as identity.

### Entities & attributes

**Lead** — a raw, unqualified prospect; the inbox of the funnel.
| field       | type        | notes                                                            |
|-------------|-------------|------------------------------------------------------------------|
| `ID`        | uint64      | surrogate, `NextSequence` on `leads` bucket                      |
| `Name`      | string      | **required**                                                     |
| `Company`   | string      | optional, plain string                                           |
| `Email`     | string      | optional, indexed                                                |
| `Phone`     | string      | optional                                                         |
| `Tags`      | []string    | optional, ad-hoc grouping                                        |
| `Source`    | string enum | `web` \| `referral` \| `event` \| `cold-outreach` \| `other`    |
| `Status`    | string enum | `new` \| `contacted` \| `qualified` \| `converted` \| `lost`    |
| `Notes`     | string      | freeform, multi-line                                             |
| `ContactID` | uint64      | `0` until converted; set to the Contact created on conversion    |
| `DealID`    | uint64      | `0` unless a Deal was created during conversion                  |
| `CreatedAt` | time.Time   | RFC3339                                                          |
| `UpdatedAt` | time.Time   | RFC3339                                                          |

**Contact** — a known person you are actively dealing with.
| field          | type      | notes                                                       |
|----------------|-----------|-------------------------------------------------------------|
| `ID`           | uint64    | surrogate, `NextSequence` on `contacts` bucket              |
| `Name`         | string    | **required**                                                |
| `Company`      | string    | optional, plain string                                      |
| `Email`        | string    | optional, indexed                                           |
| `Phone`        | string    | optional                                                    |
| `Tags`         | []string  | optional                                                    |
| `Notes`        | string    | freeform, multi-line                                        |
| `SourceLeadID` | uint64    | `0` if created directly; else the Lead it was converted from |
| `CreatedAt`    | time.Time | RFC3339                                                     |
| `UpdatedAt`    | time.Time | RFC3339                                                     |

**Deal** — an opportunity (money on the table), owned by exactly one Contact.
| field       | type        | notes                                                                |
|-------------|-------------|----------------------------------------------------------------------|
| `ID`        | uint64      | surrogate, `NextSequence` on `deals` bucket                          |
| `Title`     | string      | **required**                                                         |
| `ContactID` | uint64      | **required**, must reference an existing Contact                     |
| `Value`     | float64     | monetary amount                                                      |
| `Currency`  | string      | 3-letter code (e.g. `EUR`, `USD`); accompanies `Value`              |
| `Stage`     | string enum | `qualification` \| `proposal` \| `negotiation` \| `won` \| `lost`   |
| `Notes`     | string      | freeform, multi-line                                                 |
| `CreatedAt` | time.Time   | RFC3339                                                              |
| `UpdatedAt` | time.Time   | RFC3339                                                              |

### Relationships

```
 Lead ──(convert)──▶ Contact ──1:N──▶ Deal
   │  contactID,dealID   ▲   sourceLeadID    │ contactID (required)
   └─── set on convert ──┘                   │
                                             ▼
                          delete Contact ⇒ CASCADE delete its Deals
```

- A **Lead** converts into exactly one **Contact** (always) and optionally one **Deal**; after
  conversion the Lead is retained with `Status = converted` and back-references (`ContactID`,
  `DealID`) for provenance.
- A **Contact** has zero-or-more **Deals** (1:N via `Deal.ContactID`). A Contact optionally records
  the Lead it came from (`SourceLeadID`).
- A **Deal** belongs to exactly one **Contact**. Deleting a Contact **cascades**: all its Deals are
  deleted in the same transaction.

### Identity / key strategy
- Canonical identity for all three entities is the surrogate `uint64` ID (creation-ordered).
- Email is a searchable, non-unique attribute — multiple records may share or omit it.
- All cross-entity references are by `uint64` ID.

## Persistence Design

- **Store:** `go.etcd.io/bbolt` (aliased `bolt`), one file owned by the process, opened once at
  startup with a `Timeout`. See `.claude/rules/db-rules.md`.
- **Serialization:** `encoding/json` for all values. `time.Time` marshals to RFC3339.
- **Models stay storage-agnostic** (`internal/models`, no bbolt import); all marshal/unmarshal and
  all index maintenance happen in `internal/db` repositories. Callers receive domain models, never
  `*bolt.Tx`.
- Bucket names are package-level `[]byte` constants in `internal/db`. All buckets are
  `CreateBucketIfNotExists` in a single startup migration.

### Buckets

| bucket                 | key encoding                                        | value        | purpose                                            |
|------------------------|-----------------------------------------------------|--------------|----------------------------------------------------|
| `leads`                | 8-byte big-endian `uint64` ID                       | JSON `Lead`  | primary store, creation-ordered                    |
| `contacts`             | 8-byte big-endian `uint64` ID                       | JSON `Contact`| primary store, creation-ordered                   |
| `deals`                | 8-byte big-endian `uint64` ID                       | JSON `Deal`  | primary store, creation-ordered                    |
| `idx_contact_by_email` | `lower(email)` + `0x00` + 8-byte BE contactID       | empty / `nil`| email lookup & dedup hint (prefix-scan by email)   |
| `idx_deal_by_contact`  | 8-byte BE contactID + 8-byte BE dealID              | empty / `nil`| list deals per contact; drives cascade delete      |

### Index maintenance
- `idx_contact_by_email`: write on contact create; on update, delete the old-email key and write the
  new one when email changes; delete the key on contact delete. Email is lowercased before encoding.
  Composite key tolerates duplicate emails (the trailing contactID disambiguates).
- `idx_deal_by_contact`: write on deal create; delete+rewrite if a deal's `ContactID` changes;
  delete on deal delete. Cascade delete of a Contact prefix-scans this index by contactID to find
  and remove all its Deals (and their index entries) in one `Update`.

### Lookups served
- **By ID** (all entities): direct `Get` on the primary bucket.
- **List all / newest-first** (all entities): cursor walk of the primary bucket; reverse for
  newest-first (IDs are creation-ordered).
- **Contacts by email:** prefix-scan `idx_contact_by_email` on `lower(email)\x00`.
- **Deals for a contact:** prefix-scan `idx_deal_by_contact` on the 8-byte contactID prefix.
- **Leads by status / Deals by stage / Contacts by name-substring or tag:** full primary-bucket scan
  with in-memory filtering. Acceptable because this is a single-user dataset (hundreds–low
  thousands of rows); **no status/stage index in v1.** If volume ever demands it, add
  `idx_lead_by_status` / `idx_deal_by_stage` — that is a spec change.

### Validation enforced at the repository layer
- `Lead.Name`, `Contact.Name`, `Deal.Title` non-empty.
- `Lead.Source`, `Lead.Status`, `Deal.Stage` must be one of their enum values.
- `Deal.ContactID` must reference an existing Contact (checked before write).
- `Deal.Currency` is a non-empty 3-letter code when `Value != 0` (recommended always set).

## Use-Cases

Each use-case names its entities, the repository operations, and the surfaces that invoke it (TUI,
MCP, or both — all are available on both surfaces unless noted).

### Leads
1. **Create lead** — insert a `Lead` (`Status` defaults to `new`). Repo: `leads.Put`. Surfaces: both.
2. **List leads** — list all leads, optional filter by `Status`. Repo: scan `leads`. Surfaces: both.
3. **Get lead** — fetch by ID. Repo: `leads.Get`. Surfaces: both.
4. **Update lead** — edit fields / advance `Status`. Repo: `leads.Put` (+ email index if Lead email
   were indexed — leads are not email-indexed in v1, so no index work). Surfaces: both.
5. **Convert lead** — `convert(leadID, makeDeal, dealTitle?, dealValue?, dealCurrency?)`: create a
   Contact from the lead's fields (`SourceLeadID = leadID`); if `makeDeal`, create a Deal for that
   contact; set `lead.ContactID` (+`DealID`) and `lead.Status = converted`. All in one `Update`.
   Rejects an already-`converted` lead. Repo: `contacts.Put`, optional `deals.Put`, `leads.Put`,
   index writes. Surfaces: both.
6. **Delete lead** — remove by ID. Repo: `leads.Delete`. Surfaces: both.

### Contacts
7. **Create contact (direct)** — insert a `Contact` not originating from a lead
   (`SourceLeadID = 0`). Repo: `contacts.Put` + `idx_contact_by_email`. Surfaces: both.
8. **List / search contacts** — list all, or filter by name-substring, email, or tag. Repo: email →
   `idx_contact_by_email` prefix-scan; name/tag → `contacts` scan. Surfaces: both.
9. **Get contact** — fetch by ID. Repo: `contacts.Get`. Surfaces: both.
10. **Update contact** — edit fields; maintain email index on email change. Repo: `contacts.Put` +
    index. Surfaces: both.
11. **Delete contact (cascade)** — delete the contact **and all its Deals** and the related index
    entries, atomically. Repo: prefix-scan `idx_deal_by_contact`, delete each deal + its index entry,
    delete the contact + email index. Surfaces: both.
12. **List deals for a contact** — all deals owned by a contact. Repo: `idx_deal_by_contact`
    prefix-scan → `deals.Get`. Surfaces: both.

### Deals
13. **Create deal** — insert a `Deal` for an existing contact (validates `ContactID`). Repo:
    `deals.Put` + `idx_deal_by_contact`. Surfaces: both.
14. **List deals** — list all, optional filter by `Stage` and/or `ContactID`. Repo: `deals` scan, or
    `idx_deal_by_contact` when filtering by contact. Surfaces: both.
15. **Get deal** — fetch by ID. Repo: `deals.Get`. Surfaces: both.
16. **Update deal** — edit fields / advance `Stage`. Repo: `deals.Put` (+ index if `ContactID`
    changes). Surfaces: both.
17. **Delete deal** — remove by ID and its index entry. Repo: `deals.Delete` +
    `idx_deal_by_contact`. Surfaces: both.

### Reporting
18. **Pipeline summary** — read-only aggregate computed by scanning `deals` and `leads`:
    - per **Deal stage**: deal count and total **Value grouped by Currency** (never summed across
      currencies);
    - per **Lead status**: lead count.
    Repo: scan `deals` + `leads`, aggregate in memory. Surfaces: both (TUI Dashboard; MCP
    `pipeline_summary` tool and `crm://pipeline` resource).

## User Stories

### As the operator using the TUI
- I want to add a lead the moment it comes in, so I don't lose it (UC-1).
- I want to see my leads filtered by status, so I know who to chase next (UC-2).
- I want to convert a promising lead into a contact (and optionally start a deal) in one action, so
  qualifying is one keystroke, not re-typing (UC-5).
- I want to browse and edit my contacts, so their details stay current (UC-8, UC-10).
- I want to move a deal along its stages and edit its value, so my pipeline reflects reality
  (UC-16).
- I want a dashboard showing pipeline value per stage (by currency) and my lead funnel counts, so I
  see the state of the business at a glance (UC-18).
- I want deleting a contact to also clear its dead deals, so I don't leave orphans behind (UC-11).

### As an AI assistant using the MCP server
- I want to create, read, update, and delete leads/contacts/deals via tools, so I can maintain the
  CRM on the user's behalf (UC-1…17).
- I want to convert a lead through a single tool call, so I can qualify prospects the user flags
  (UC-5).
- I want to read any record by URI resource, so I can pull context without a tool round-trip
  (UC-3/9/15).
- I want a pipeline summary, so I can report funnel health to the user (UC-18).
- I want guided prompts (triage new leads, draft a follow-up), so I can kick off common workflows
  consistently.

## MCP Surface

Server built with `github.com/mark3labs/mcp-go` (`internal/server`), stdio transport selected in
`cmd/`. Capabilities: tools, resources, prompts; `WithRecovery()` + `WithLogging()` enabled. Logs go
to **stderr** only (stdout is the protocol channel). User/input errors →
`NewToolResultError(...), nil`; infrastructure errors → `nil, err`.

### Tools

| tool              | purpose                              | input (key fields)                                                                 | output                                  |
|-------------------|--------------------------------------|------------------------------------------------------------------------------------|-----------------------------------------|
| `create_lead`     | UC-1 add a lead                      | name (req), company, email, phone, tags[], source, notes                           | created Lead                            |
| `list_leads`      | UC-2 list/filter leads               | status?                                                                            | Lead[]                                  |
| `get_lead`        | UC-3 fetch a lead                    | id (req)                                                                            | Lead                                    |
| `update_lead`     | UC-4 edit/advance a lead             | id (req) + any editable fields incl. status                                        | updated Lead                            |
| `convert_lead`    | UC-5 convert to contact (+deal)      | id (req), make_deal (bool), deal_title?, deal_value?, deal_currency?               | { contact, deal? , lead }               |
| `delete_lead`     | UC-6 delete a lead                   | id (req)                                                                            | ok                                      |
| `create_contact`  | UC-7 add a contact                   | name (req), company, email, phone, tags[], notes                                   | created Contact                         |
| `list_contacts`   | UC-8 list/search contacts            | query? (name substring), email?, tag?                                              | Contact[]                               |
| `get_contact`     | UC-9 fetch a contact                 | id (req)                                                                            | Contact                                 |
| `update_contact`  | UC-10 edit a contact                 | id (req) + editable fields                                                          | updated Contact                         |
| `delete_contact`  | UC-11 delete (cascade deals)         | id (req)                                                                            | { deleted_deal_ids[] }                  |
| `create_deal`     | UC-13 add a deal                     | title (req), contact_id (req), value, currency, stage, notes                       | created Deal                            |
| `list_deals`      | UC-14 list/filter deals              | stage?, contact_id?                                                                 | Deal[]                                  |
| `get_deal`        | UC-15 fetch a deal                   | id (req)                                                                            | Deal                                    |
| `update_deal`     | UC-16 edit/advance a deal            | id (req) + editable fields incl. stage                                             | updated Deal                            |
| `delete_deal`     | UC-17 delete a deal                  | id (req)                                                                            | ok                                      |
| `pipeline_summary`| UC-18 funnel + pipeline aggregate    | (none)                                                                              | { deals_by_stage[], leads_by_status[] } |

Tools with more than one or two args use typed input structs (`jsonschema` tags) +
`mcp.WithInputSchema[T]()` / `NewStructuredToolHandler`. Every tool and parameter carries a
description.

### Resources (read-only)

| URI template          | returns                                  |
|-----------------------|------------------------------------------|
| `crm://leads/{id}`    | a single Lead as JSON                     |
| `crm://contacts/{id}` | a single Contact as JSON                  |
| `crm://deals/{id}`    | a single Deal as JSON                     |
| `crm://pipeline`      | the pipeline summary (same as UC-18)      |

`{id}` is validated as a numeric ID; unknown IDs return a not-found resource error.

### Prompts

| prompt             | purpose                                                                                |
|--------------------|----------------------------------------------------------------------------------------|
| `triage_new_leads` | guide the assistant to review `new`/`contacted` leads and suggest next status/action.  |
| `draft_followup`   | given a contact (and optionally a deal), draft a follow-up message. Args: contact_id, deal_id? |

## TUI Surface

Built with `github.com/rivo/tview` (`internal/tui`); one `*tview.Application`, screens composed via
`Pages`. All data flows through `internal/db` repositories — no bbolt or business logic in handlers.
Slow work runs off the event loop and mutations come back via `QueueUpdateDraw`.

### Screens

1. **Dashboard** — read-only pipeline summary: deal count + value per stage (grouped by currency)
   and lead counts by status (UC-18). Landing screen.
2. **Leads** — a `Table` of leads (status-filterable). Enter opens a detail/edit **Form**; actions:
   `n` new, edit on Enter, `c` **convert** (opens a small form: make-deal? title/value/currency),
   `d` delete, `f` cycle status filter (UC-1,2,4,5,6).
3. **Contacts** — a `Table` of contacts. Enter opens detail/edit Form; `n` new, `d` delete
   (cascade, with a confirm `Modal` listing affected deals). A contact's deals are listed in its
   detail view (UC-7,8,10,11,12).
4. **Deals** — a `Table` of deals (stage-filterable). Enter opens detail/edit Form; `n` new (pick
   contact), `s` change **stage**, `d` delete, `f` cycle stage filter (UC-13,14,16,17).

### Navigation map

```
        ┌──────────── global keys (app.SetInputCapture) ────────────┐
        │  F1 Dashboard · F2 Leads · F3 Contacts · F4 Deals · q/Ctrl-C quit │
        └───────────────────────────────────────────────────────────┘
 [Dashboard] ⇄ [Leads] ⇄ [Contacts] ⇄ [Deals]      (Pages: SwitchToPage)
      Leads:    table ──Enter──▶ lead form ──Esc──▶ table
                table ──'c'────▶ convert form ──submit──▶ table (+ new contact/deal)
   Contacts:    table ──Enter──▶ contact form (+its deals) ──Esc──▶ table
                table ──'d'────▶ confirm Modal ──▶ cascade delete
      Deals:    table ──Enter──▶ deal form ──Esc──▶ table
                table ──'s'────▶ stage picker (modal) ──Esc/Cancel──▶ table
```

Quit: `Ctrl-C` quits from anywhere. `q` quits from any table and from the button-only **modals**
(stage picker, delete confirm), which have no text entry. Inside the text **forms** `q` is normal
input, so the way back is `Esc` (or the Cancel button); their title shows "Esc cancels". Header rows
are fixed; row-0 selection is guarded.

## Acceptance Criteria

- **UC-1 Create lead:** a lead with a non-empty name persists with a fresh monotonic ID,
  `Status = new`, and timestamps set; empty name is rejected; invalid `source` is rejected.
- **UC-2 List leads:** all leads are returned newest-first; filtering by a status returns exactly the
  leads in that status.
- **UC-3 Get lead:** a known ID returns the lead; an unknown ID returns a clean not-found (no panic).
- **UC-4 Update lead:** edited fields persist, `UpdatedAt` advances, ID and `CreatedAt` are
  unchanged; an invalid status value is rejected.
- **UC-5 Convert lead:** converting a non-converted lead creates a Contact whose fields mirror the
  lead and whose `SourceLeadID` is the lead; with `make_deal` it also creates a Deal for that contact
  with the given value/currency; the lead becomes `converted` with `ContactID` (and `DealID`) set;
  the whole thing is atomic; converting an already-`converted` lead is rejected.
- **UC-6 Delete lead:** the lead is gone after delete; deleting an unknown ID is a clean no-op/error,
  not a panic.
- **UC-7 Create contact:** a named contact persists with a fresh ID and, if email is present, an
  `idx_contact_by_email` entry exists.
- **UC-8 List/search contacts:** email search returns all contacts with that email via the index;
  name-substring and tag filters return the matching contacts.
- **UC-9/10 Get/Update contact:** get returns the record; update persists field changes, advances
  `UpdatedAt`, and rewrites the email index when email changes (old key gone, new key present).
- **UC-11 Delete contact (cascade):** deleting a contact removes the contact, **every** deal with
  that `ContactID`, and all related index entries, atomically; afterward no deal references the
  deleted contact and no `idx_deal_by_contact` entry remains for it.
- **UC-12 Deals for a contact:** returns exactly the deals whose `ContactID` matches, via the index.
- **UC-13 Create deal:** a deal with a non-empty title and an **existing** `contact_id` persists with
  a fresh ID and an `idx_deal_by_contact` entry; a deal referencing a non-existent contact is
  rejected; empty title is rejected; invalid stage is rejected.
- **UC-14 List deals:** unfiltered returns all; `stage` filter and `contact_id` filter each narrow
  correctly and compose.
- **UC-15/16 Get/Update deal:** get returns the record; update persists changes incl. stage, advances
  `UpdatedAt`, and maintains the contact index if `ContactID` changes.
- **UC-17 Delete deal:** the deal and its `idx_deal_by_contact` entry are removed.
- **UC-18 Pipeline summary:** for each deal stage, count and per-currency value totals are correct
  and **never summed across currencies**; lead counts per status are correct; an empty DB yields
  zeroed groups without error.
- **MCP:** each tool is reachable through an in-process client; input/business errors come back as
  tool-error results (not transport errors); resources return the right record for a valid ID and a
  not-found error otherwise; logs never touch stdout.
- **TUI:** all four screens render via a `SimulationScreen`; global keys switch pages and quit; the
  convert action and the cascade-delete confirm modal work; no DB call runs on the event loop.
- **Concurrency:** opening the bbolt file fails fast (via `Timeout`) if another instance holds the
  lock, proving the single-writer / alternate-mode contract.

## Open Questions / Assumptions

- **Single app-wide vs. per-deal currency:** resolved to **per-deal currency**; totals are therefore
  reported grouped by currency (no FX, offline). If the user later wants one blended figure, that
  needs either a fixed manual rate table or an app-wide currency — a spec change.
- **No status/stage indexes in v1:** assumed dataset is small enough that scanning the primary
  buckets to filter leads-by-status and deals-by-stage is fine. Revisit (add `idx_lead_by_status` /
  `idx_deal_by_stage`) if data grows large — spec change.
- **Hard deletes only:** no soft-delete/archive or audit trail in v1; deletes are permanent
  (contact deletes cascade to deals).
- **No tasks / no activity log:** "what's next" and "what was said" are captured only in freeform
  `notes`. A Task entity and/or an Interactions log are explicit later candidates.
- **Lead email not indexed:** only Contact email is indexed in v1; lead email search (if ever needed)
  would be a primary-bucket scan.
- **Mode selection:** assumed the binary picks TUI vs. MCP at launch (e.g. a flag/subcommand or env
  var, decided in `cmd/`), never running both against the file at once.
