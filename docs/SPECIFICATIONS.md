# Leadzaar — Specification

> This document is the **single source of truth** for what this app is. Code follows the spec;
> any scope change updates the spec **first** (see `.claude/rules/specification-rules.md`).

## Overview

**Leadzaar** is a local-first, single-user sales CRM that runs as one self-contained Go binary.
It helps a solo operator (freelancer, consultant, independent salesperson) work a simple funnel:
capture **Leads**, qualify and **convert** the good ones into **Contacts**, and track the money on
the table as **Deals** moving through a pipeline. There is no team, no web app, and no cloud — all
data lives in one embedded bbolt file owned by the process.

The app presents the same data through two surfaces: an interactive **tview TUI** for the human
operator, and an **MCP stdio server** so an AI assistant can read and update the CRM directly. Each
surface is a **mode of the same binary** (selected at launch). They **may run concurrently as
separate local processes** against the same bbolt file: persistence uses a **connection-per-operation**
strategy so no process holds the bbolt lock while idle — it opens the file only for the duration of
each read or write. The TUI also polls the file's transaction ID and refreshes when the MCP process
writes, so the two stay in sync. See `docs/bbolt-concurrent-access-strategy.md`.

## Goals & Non-Goals

### Goals
- Track the lead → contact → deal funnel for **one user** on **one machine**.
- Provide CRUD over Leads, Contacts, Deals, and Offers (email-style proposals linked to a Lead), plus
  a lead **conversion** action and a read-only **pipeline summary** (deal stages + lead funnel, value
  grouped by currency).
- Expose the model through both a **TUI** and an **MCP server**, each consuming the same repository
  layer.
- Allow the **TUI and MCP modes to run concurrently** as separate local processes against the same
  file, via a **connection-per-operation** persistence strategy (no process holds the bbolt lock
  while idle). The TUI auto-refreshes when the other process writes.
- Keep all persistence in a single embedded **bbolt** file with no external dependencies.

### Non-Goals (the local-only envelope)
- ❌ **No** web server, REST/GraphQL API, network service, cloud sync, or message broker.
- ❌ **No** networked daemon or always-on background service, and **no** internet access required to
  function. (The TUI and MCP modes may run as concurrent **local** processes against the same file —
  see Goals and Persistence Design.)
- ❌ **No** multi-user / team features: no accounts, ownership, assignment, or sharing.
- ❌ **No** currency conversion / FX (there is no network) — monetary totals are reported **per
  currency**, never summed across currencies.
- ❌ **No** separate Tasks/reminders entity and **no** general Interactions/activity-log entity in
  v1. Day-to-day context is captured in a freeform `notes` field on each entity.
- ✔️ **Offer is a first-class entity.** A Lead has zero-or-more **Offers** — email-style proposals
  (`title`, `description`, email `subject`, raw email `body`) linked to the Lead by `LeadID`. Deleting
  a Lead **cascade-deletes** its Offers. Offers are local-only: composing/sending email is out of
  scope — an Offer just stores the drafted content.
- ✔️ **Company is a first-class entity.** Leads, Contacts, and Deals optionally **link to a Company
  by ID** (`CompanyID`, `0` = unlinked); a Company is plain reference data (no funnel state).
  Deleting a Company **unlinks** it from any referencing records — their `CompanyID` is reset to `0`
  and the records are kept (no cascade delete of people/deals). Companies are still **local-only**:
  no enrichment, no network lookups.
- ❌ **No** networked or cross-machine concurrency — concurrent access is limited to **local
  processes on one machine** sharing the file (serialized at the file level, brief per-operation
  locks). High write contention is out of scope; this is a single-user tool.

## Domain Model

Five entities. Every entity has a surrogate `uint64` ID (bbolt `NextSequence`), encoded big-endian
as its primary key so records sort in creation order. Email is **optional** and **non-unique**; it
is indexed only as a lookup/dedup hint, never as identity. Leads, Contacts, and Deals optionally
reference a **Company** by `CompanyID` (`0` = unlinked). A **Lead** has zero-or-more **Offers**, each
linked back to it by `LeadID`.

### Entities & attributes

**Lead** — a raw, unqualified prospect; the inbox of the funnel.
| field       | type        | notes                                                            |
|-------------|-------------|------------------------------------------------------------------|
| `ID`        | uint64      | surrogate, `NextSequence` on `leads` bucket                      |
| `Name`      | string      | **required**                                                     |
| `CompanyID` | uint64      | optional link to a Company (`0` = none); must reference one      |
| `Email`     | string      | optional, indexed                                                |
| `Phone`     | string      | optional                                                         |
| `Tags`      | []string    | optional, ad-hoc grouping                                        |
| `Quality`   | int         | optional lead score `1`–`10` (`0` = unscored)                   |
| `Source`    | string enum | `web` \| `referral` \| `event` \| `cold-outreach` \| `other`    |
| `Status`    | string enum | `new` \| `contacted` \| `contacted-first-touch` \| `contacted-followup-1` \| `contacted-followup-2` \| `contacted-followup-3` \| `qualified` \| `converted` \| `lost` (granular `contacted-*` states sit between the legacy `contacted` and `qualified`) |
| `Notes`     | string      | freeform, multi-line                                             |
| `UnavailableUntil` | time.Time | optional **date-only** block (stored midnight UTC, zero = none); the lead is unreachable until it, **exclusive** — see below |
| `ContactID` | uint64      | `0` until converted; set to the Contact created on conversion    |
| `DealID`    | uint64      | `0` unless a Deal was created during conversion                  |
| `CreatedAt` | time.Time   | RFC3339                                                          |
| `UpdatedAt` | time.Time   | RFC3339                                                          |

**Lead availability.** `UnavailableUntil` records a date a lead is known to be unreachable until —
typically transcribed from an out-of-office autoresponder ("away until the 15th") — so a follow-up
or an Offer can be held back instead of wasted. It is:

- **Date-only.** Surfaces exchange `YYYY-MM-DD`; the repository normalizes any supplied instant to
  midnight UTC on its own calendar date, so one date is always one instant.
- **Exclusive.** A lead is available again *from* that date: "away until the 15th" is reachable on
  the 15th. A lead is *available* when `UnavailableUntil` is zero or not after now — an elapsed
  block is indistinguishable from none, so the field never needs manual clearing.
- **Independent of `Status`.** An unavailable lead keeps its funnel position; availability is a
  timing signal, not a stage. There is no `unavailable` status, and no notification or scheduling
  behavior is implied — the operator or agent queries for it (nothing in the local-only envelope
  fires on a date).

**Contact** — a known person you are actively dealing with.
| field          | type      | notes                                                       |
|----------------|-----------|-------------------------------------------------------------|
| `ID`           | uint64    | surrogate, `NextSequence` on `contacts` bucket              |
| `Name`         | string    | **required**                                                |
| `CompanyID`    | uint64    | optional link to a Company (`0` = none); must reference one  |
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
| `CompanyID` | uint64      | optional link to a Company (`0` = none); must reference one          |
| `Value`     | float64     | monetary amount                                                      |
| `Currency`  | string      | 3-letter code (e.g. `EUR`, `USD`); accompanies `Value`              |
| `Stage`     | string enum | `qualification` \| `proposal` \| `negotiation` \| `won` \| `lost`   |
| `Notes`     | string      | freeform, multi-line                                                 |
| `CreatedAt` | time.Time   | RFC3339                                                              |
| `UpdatedAt` | time.Time   | RFC3339                                                              |

**Company** — an organization that Leads, Contacts, and Deals may optionally link to. Reference data
with no funnel state.
| field       | type      | notes                                                       |
|-------------|-----------|-------------------------------------------------------------|
| `ID`        | uint64    | surrogate, `NextSequence` on `companies` bucket             |
| `Name`      | string    | **required**                                                |
| `Website`   | string    | optional                                                    |
| `Industry`  | string    | optional, freeform                                          |
| `Phone`     | string    | optional                                                    |
| `Notes`     | string    | freeform, multi-line                                        |
| `CreatedAt` | time.Time | RFC3339                                                     |
| `UpdatedAt` | time.Time | RFC3339                                                     |

**Offer** — an email-style proposal made to a Lead (1:N via `LeadID`). Stores drafted content only;
sending email is out of scope.
| field         | type      | notes                                                          |
|---------------|-----------|----------------------------------------------------------------|
| `ID`          | uint64    | surrogate, `NextSequence` on `offers` bucket                   |
| `LeadID`      | uint64    | **required**, must reference an existing Lead                  |
| `Title`       | string    | **required**                                                   |
| `Description` | string    | optional, short summary of the offer                          |
| `Subject`     | string    | optional, email subject line                                  |
| `Body`        | string    | optional, raw email body content (may be long, multi-line)     |
| `CreatedAt`   | time.Time | RFC3339                                                        |
| `UpdatedAt`   | time.Time | RFC3339                                                        |

### Relationships

```
 Lead ──(convert)──▶ Contact ──1:N──▶ Deal
   │  contactID,dealID   ▲   sourceLeadID    │ contactID (required)
   └─── set on convert ──┘                   │
                                             ▼
                          delete Contact ⇒ CASCADE delete its Deals

 Lead ──1:N──▶ Offer   (Offer.LeadID, required)
   delete Lead ⇒ CASCADE delete its Offers

 Company ──0:N──▶ Lead / Contact / Deal   (optional link via CompanyID)
   delete Company ⇒ UNLINK referencing records (CompanyID → 0; records kept)
```

- A **Lead** converts into exactly one **Contact** (always) and optionally one **Deal**; after
  conversion the Lead is retained with `Status = converted` and back-references (`ContactID`,
  `DealID`) for provenance. The Contact inherits the Lead's `CompanyID`.
- A **Contact** has zero-or-more **Deals** (1:N via `Deal.ContactID`). A Contact optionally records
  the Lead it came from (`SourceLeadID`).
- A **Deal** belongs to exactly one **Contact**. Deleting a Contact **cascades**: all its Deals are
  deleted in the same transaction.
- A **Lead** has zero-or-more **Offers** (1:N via `Offer.LeadID`, required). Deleting a Lead
  **cascades**: all its Offers (and their index entries) are deleted in the same transaction.
- A **Company** is optionally referenced by zero-or-more Leads, Contacts, and Deals (each via its
  `CompanyID`). The link is **validated** on write (a non-zero `CompanyID` must reference an existing
  Company). Deleting a Company **unlinks** every referencing Lead/Contact/Deal — their `CompanyID` is
  reset to `0` in the same transaction; the records themselves are retained (no cascade delete).

### Identity / key strategy
- Canonical identity for all three entities is the surrogate `uint64` ID (creation-ordered).
- Email is a searchable, non-unique attribute — multiple records may share or omit it.
- All cross-entity references are by `uint64` ID.

## Persistence Design

- **Store:** `go.etcd.io/bbolt` (aliased `bolt`). The `Store` holds only the file **path**, not a
  live handle. **Connection-per-operation:** every read opens a short-lived read-only handle and
  every write a short-lived read-write handle, each closed immediately, so an idle process holds no
  lock and the TUI and MCP modes can run concurrently. Opening uses a short per-attempt `Timeout`
  with backoff retry so a brief cross-process collision becomes a sub-second wait, not a failure. A
  one-time read-write bootstrap at startup creates the file and runs the idempotent bucket migration
  (read-only opens cannot create the file). See `.claude/rules/db-rules.md` and
  `docs/bbolt-concurrent-access-strategy.md`.
- **Database location:** resolved identically by both surfaces, in this order — the `-db` flag, then
  the `LEADZAAR_DB` environment variable, then the default `~/.local/leadzaar/default.db`. The
  parent directory is created (`0755`) on first open if missing; the file itself is `0600`. Because
  the default is machine-wide rather than working-directory-relative, starting the TUI and the MCP
  server from different directories still lands both on the same file.
- **Change detection:** `Store.TxID()` returns bbolt's latest committed transaction ID (monotonic).
  Long-lived readers (the TUI) poll it to detect that another process has written, without scanning
  data.
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
| `companies`            | 8-byte big-endian `uint64` ID                       | JSON `Company`| primary store, creation-ordered                   |
| `offers`               | 8-byte big-endian `uint64` ID                       | JSON `Offer` | primary store, creation-ordered                    |
| `idx_contact_by_email` | `lower(email)` + `0x00` + 8-byte BE contactID       | empty / `nil`| email lookup & dedup hint (prefix-scan by email)   |
| `idx_deal_by_contact`  | 8-byte BE contactID + 8-byte BE dealID              | empty / `nil`| list deals per contact; drives cascade delete      |
| `idx_offer_by_lead`    | 8-byte BE leadID + 8-byte BE offerID                | empty / `nil`| list offers per lead; drives cascade delete        |

There is **no Company index** — Company name/website/industry search and the reverse "records linked
to a Company" lookup (used by the unlink-on-delete) are in-memory primary-bucket scans, consistent
with the no-status/stage-index decision below. A legacy on-disk record that still stores `company`
as a plain string is upgraded to a `CompanyID` reference by an idempotent startup migration
(find-or-create a Company by name; drop the legacy key).

### Index maintenance
- `idx_contact_by_email`: write on contact create; on update, delete the old-email key and write the
  new one when email changes; delete the key on contact delete. Email is lowercased before encoding.
  Composite key tolerates duplicate emails (the trailing contactID disambiguates).
- `idx_deal_by_contact`: write on deal create; delete+rewrite if a deal's `ContactID` changes;
  delete on deal delete. Cascade delete of a Contact prefix-scans this index by contactID to find
  and remove all its Deals (and their index entries) in one `Update`.
- `idx_offer_by_lead`: write on offer create; delete+rewrite if an offer's `LeadID` changes; delete
  on offer delete. Cascade delete of a Lead prefix-scans this index by leadID to find and remove all
  its Offers (and their index entries) in one `Update`.

### Lookups served
- **By ID** (all entities): direct `Get` on the primary bucket.
- **List all** (all entities): cursor walk of the primary bucket. The internal `List*`/`Search*`
  helpers (TUI, prompts) walk in creation order; the `list_*` MCP tools re-sort the matching set by
  **last-updated first** (see below).
- **Contacts by email:** prefix-scan `idx_contact_by_email` on `lower(email)\x00`.
- **Deals for a contact:** prefix-scan `idx_deal_by_contact` on the 8-byte contactID prefix.
- **Offers for a lead:** prefix-scan `idx_offer_by_lead` on the 8-byte leadID prefix.
- **Leads by status / Deals by stage / Contacts by name-substring or tag:** full primary-bucket scan
  with in-memory filtering. Acceptable because this is a single-user dataset (hundreds–low
  thousands of rows); **no status/stage index in v1.** If volume ever demands it, add
  `idx_lead_by_status` / `idx_deal_by_stage` — that is a spec change.
- **Every `list_*` tool — search, sort, paginate (minimal items):** each list tool runs one full
  primary-bucket scan, applies its filters + an optional case-insensitive substring search, sorts the
  whole matching set in memory (default **`updated`** = last-updated first; `order` desc by default,
  ID as a stable tiebreaker), then slices out a 1-based page. `page_size` is clamped to `[1, 50]`
  (default 50; values above 50 are silently capped), and the result carries
  `total`/`total_pages`/`has_more` so an agent can walk the rest. Sort fields are `created`/`updated`
  for every entity, plus `quality` for leads. The returned items are **minimal projections** (short
  scalars only — no long/freeform text); the full record is fetched with `get_*` or the
  `crm://.../{id}` resource. No per-field index in v1 — the scan + in-memory sort is acceptable at
  single-user scale.
- **Companies (list / search by name·website·industry):** full `companies` scan. **Records linked to
  a Company** (driving unlink-on-delete): scan `leads`, `contacts`, `deals` for a matching
  `CompanyID`. No company index in v1.

### Validation enforced at the repository layer
- `Lead.Name`, `Contact.Name`, `Deal.Title`, `Company.Name`, `Offer.Title` non-empty.
- `Offer.LeadID` must reference an existing Lead (checked before write).
- `Lead.Source`, `Lead.Status`, `Deal.Stage` must be one of their enum values.
- `Lead.Quality`, when set, is an integer `1`–`10` (`0` means unscored).
- `Lead.UnavailableUntil` is normalized to midnight UTC on write (never rejected — a past date is a
  legitimate elapsed block). Surfaces reject a value that is not a `YYYY-MM-DD` calendar date.
- `Deal.ContactID` must reference an existing Contact (checked before write).
- A non-zero `CompanyID` on a Lead, Contact, or Deal must reference an existing Company (checked
  before write).
- `Deal.Currency` is a non-empty 3-letter code when `Value != 0` (recommended always set).

## Use-Cases

Each use-case names its entities, the repository operations, and the surfaces that invoke it (TUI,
MCP, or both — all are available on both surfaces unless noted).

### Leads
1. **Create lead** — insert a `Lead` (`Status` defaults to `new`). Repo: `leads.Put`. Surfaces: both.
2. **List leads** — list leads with optional `Status` filter, optional **availability** filter
   (`available` / `unavailable`, evaluated against the clock at query time), substring search, sort
   (`updated` default / `created` / `quality` / `unavailable-until`), and pagination. Filters compose
   with AND. Ordering by `unavailable-until` sorts an unset date **last when ascending** (an unset
   date means "nothing to wait for", not the zero instant), so ascending answers "who frees up
   soonest". The MCP `list_leads` tool returns **minimal items** (no `Notes`, but **including**
   `unavailableUntil`, since it drives the contact-now decision the list exists to answer) ordered
   last-updated-first. Repo: scan `leads`. Surfaces: both.
3. **Get lead** — fetch by ID. Repo: `leads.Get`. Surfaces: both.
4. **Update lead** — edit fields / advance `Status`. This is where an out-of-office reply is
   recorded: set `UnavailableUntil` to the date the autoresponder names, or clear it to unblock the
   lead early. Repo: `leads.Put` (+ email index if Lead email were indexed — leads are not
   email-indexed in v1, so no index work). Surfaces: both.
5. **Convert lead** — `convert(leadID, makeDeal, dealTitle?, dealValue?, dealCurrency?)`: create a
   Contact from the lead's fields (`SourceLeadID = leadID`); if `makeDeal`, create a Deal for that
   contact; set `lead.ContactID` (+`DealID`) and `lead.Status = converted`. All in one `Update`.
   Rejects an already-`converted` lead. Repo: `contacts.Put`, optional `deals.Put`, `leads.Put`,
   index writes. Surfaces: both.
6. **Delete lead (cascade)** — remove by ID **and all its Offers** and their index entries,
   atomically. Repo: prefix-scan `idx_offer_by_lead`, delete each offer + its index entry, delete the
   lead. Returns the deleted offer IDs. Surfaces: both.

### Contacts
7. **Create contact (direct)** — insert a `Contact` not originating from a lead
   (`SourceLeadID = 0`). Repo: `contacts.Put` + `idx_contact_by_email`. Surfaces: both.
8. **List / search contacts** — filter by name-substring/email/tag, with sort
   (`updated` default / `created`) and pagination. The MCP `list_contacts` tool returns **minimal
   items** (no `Notes`) ordered last-updated-first. Repo: email → `idx_contact_by_email` prefix-scan;
   otherwise `contacts` scan. Surfaces: both.
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
14. **List deals** — filter by `Stage` and/or `ContactID` and an optional substring search over
    title/company, with sort (`updated` default / `created`) and pagination. The MCP `list_deals`
    tool returns **minimal items** (no `Notes`) ordered last-updated-first. Repo: `deals` scan, or
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

### Companies
19. **Create company** — insert a `Company` (Name required). Repo: `companies.Put`. Surfaces: both.
20. **List / search companies** — filter by name/website/industry substring, with sort
    (`updated` default / `created`) and pagination. The MCP `list_companies` tool returns **minimal
    items** (no `Notes`) ordered last-updated-first. Repo: scan `companies`. Surfaces: both.
21. **Get company** — fetch by ID. Repo: `companies.Get`. Surfaces: both.
22. **Update company** — edit fields. Repo: `companies.Put`. Surfaces: both.
23. **Delete company (unlink)** — delete the company and reset `CompanyID = 0` on **every** Lead,
    Contact, and Deal that referenced it, atomically (the records are kept). Returns the count of
    records unlinked. Repo: scan `leads`/`contacts`/`deals`, rewrite matches, delete the company —
    all in one `Update`. Surfaces: both.

Linking a Lead, Contact, or Deal to a Company is part of its create/update (UC-1,4,7,10,13,16): the
record carries an optional `CompanyID`, validated to reference an existing Company.

### Offers
24. **Create offer** — insert an `Offer` for an existing lead (validates `LeadID`; `Title` required).
    Repo: `offers.Put` + `idx_offer_by_lead`. Surfaces: both.
25. **List offers** — filter by `LeadID` and an optional substring search over title/subject, with
    sort (`updated` default / `created`) and pagination. The MCP `list_offers` tool returns **minimal
    items** (id, leadId, title, subject, timestamps — no `Description`/`Body`) ordered
    last-updated-first. Repo: `offers` scan, or `idx_offer_by_lead` when filtering by lead. Surfaces:
    both.
26. **Get offer** — fetch by ID. Repo: `offers.Get`. Surfaces: both.
27. **Update offer** — edit fields (title, description, subject, body); maintain `idx_offer_by_lead`
    if `LeadID` changes (new lead must exist). Repo: `offers.Put` + index. Surfaces: both.
28. **Delete offer** — remove by ID and its index entry. Repo: `offers.Delete` + `idx_offer_by_lead`.
    Surfaces: both. (Deleting a Lead cascade-deletes its Offers — see UC-6.)

## User Stories

### As the operator using the TUI
- I want to add a lead the moment it comes in, so I don't lose it (UC-1).
- I want to see my leads filtered by status, so I know who to chase next (UC-2).
- I want to record the return date from a lead's out-of-office autoresponder, so I can see at a
  glance who is away and stop chasing someone who is on holiday (UC-2, UC-4).
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
- I want to write an autoresponder's return date onto a lead and later list only the leads that are
  contactable today, so I follow up or send an offer when it will actually be read (UC-2, UC-4).
- I want to read any record by URI resource, so I can pull context without a tool round-trip
  (UC-3/9/15).
- I want a pipeline summary, so I can report funnel health to the user (UC-18).
- I want guided prompts (triage new leads, draft a follow-up), so I can kick off common workflows
  consistently.

## MCP Surface

Server built with `github.com/mark3labs/mcp-go` (`internal/server`), stdio transport selected in
`cmd/`. Capabilities: tools, resources, prompts; `WithRecovery()` + `WithLogging()` enabled. Logs go
to **stderr** only (stdout is the protocol channel). User/input errors →
`NewToolResultError(...), nil`; infrastructure errors → `nil, err`. CRUD tool results additionally
carry an embedded interactive widget built with `github.com/techthos/gadget` — see "Interactive
widget UI" below.

### Tools

| tool              | purpose                              | input (key fields)                                                                 | output                                  |
|-------------------|--------------------------------------|------------------------------------------------------------------------------------|-----------------------------------------|
| `create_lead`     | UC-1 add a lead                      | name (req), company_id?, email, phone, tags[], quality?, source, notes, unavailable_until? (`YYYY-MM-DD`) | created Lead                            |
| `list_leads`      | UC-2 list/search/sort/paginate leads | status?, availability? (available/unavailable), query? (name/company/email/tag substring), sort_by? (updated default/created/quality/unavailable-until), order? (desc/asc), page?, page_size? (≤50) | { leads: MinimalLead[], page, page_size, total, total_pages, has_more } |
| `get_lead`        | UC-3 fetch a lead                    | id (req)                                                                            | Lead                                    |
| `update_lead`     | UC-4 edit/advance a lead             | id (req) + any editable fields incl. status, company_id, quality, unavailable_until (`YYYY-MM-DD`; `""` clears) | updated Lead                            |
| `convert_lead`    | UC-5 convert to contact (+deal)      | id (req), make_deal (bool), deal_title?, deal_value?, deal_currency?               | { contact, deal? , lead }               |
| `delete_lead`     | UC-6 delete a lead (cascade offers)  | id (req)                                                                            | { deleted, deleted_offer_ids[] }        |
| `create_contact`  | UC-7 add a contact                   | name (req), company_id?, email, phone, tags[], notes                               | created Contact                         |
| `list_contacts`   | UC-8 list/search/sort/paginate contacts | query? (name/company/email/tag substring), email?, tag?, sort_by? (updated default/created), order? (desc/asc), page?, page_size? (≤50) | { contacts: MinimalContact[], page, page_size, total, total_pages, has_more } |
| `get_contact`     | UC-9 fetch a contact                 | id (req)                                                                            | Contact                                 |
| `update_contact`  | UC-10 edit a contact                 | id (req) + editable fields incl. company_id                                         | updated Contact                         |
| `delete_contact`  | UC-11 delete (cascade deals)         | id (req)                                                                            | { deleted_deal_ids[] }                  |
| `create_deal`     | UC-13 add a deal                     | title (req), contact_id (req), company_id?, value, currency, stage, notes          | created Deal                            |
| `list_deals`      | UC-14 list/filter/sort/paginate deals | stage?, contact_id?, query? (title/company substring), sort_by? (updated default/created), order? (desc/asc), page?, page_size? (≤50) | { deals: MinimalDeal[], page, page_size, total, total_pages, has_more } |
| `get_deal`        | UC-15 fetch a deal                   | id (req)                                                                            | Deal                                    |
| `update_deal`     | UC-16 edit/advance a deal            | id (req) + editable fields incl. stage, company_id                                 | updated Deal                            |
| `delete_deal`     | UC-17 delete a deal                  | id (req)                                                                            | ok                                      |
| `create_company`  | UC-19 add a company                  | name (req), website?, industry?, phone?, notes?                                     | created Company                         |
| `list_companies`  | UC-20 list/search/sort/paginate companies | query? (name/website/industry substring), sort_by? (updated default/created), order? (desc/asc), page?, page_size? (≤50) | { companies: MinimalCompany[], page, page_size, total, total_pages, has_more } |
| `get_company`     | UC-21 fetch a company                | id (req)                                                                            | Company                                 |
| `update_company`  | UC-22 edit a company                 | id (req) + editable fields                                                          | updated Company                         |
| `delete_company`  | UC-23 delete (unlink references)     | id (req)                                                                            | { deleted, unlinked }                   |
| `create_offer`    | UC-24 add an offer                   | lead_id (req), title (req), description?, subject?, body?                           | created Offer                           |
| `list_offers`     | UC-25 list/filter/sort/paginate offers | lead_id?, query? (title/subject substring), sort_by? (updated default/created), order? (desc/asc), page?, page_size? (≤50) | { offers: MinimalOffer[], page, page_size, total, total_pages, has_more } |
| `get_offer`       | UC-26 fetch an offer                 | id (req)                                                                            | Offer                                   |
| `update_offer`    | UC-27 edit an offer                  | id (req), lead_id (req) + editable fields (title, description, subject, body)       | updated Offer                           |
| `delete_offer`    | UC-28 delete an offer                | id (req)                                                                            | ok                                      |
| `pipeline_summary`| UC-18 funnel + pipeline aggregate    | (none)                                                                              | { deals_by_stage[], leads_by_status[] } |

Tools with more than one or two args use typed input structs (`jsonschema` tags) +
`mcp.WithInputSchema[T]()` / `NewStructuredToolHandler`. Every tool and parameter carries a
description. A `jsonschema` tag value is a plain description string (the schema generator,
`google/jsonschema-go`, treats the whole tag as the description); a field is **required** unless its
`json` tag carries `omitempty`. **Every list tool is paginated** and wraps its item slice under a
single entity key alongside pagination metadata (`{ <items>, page, page_size, total, total_pages,
has_more }`) — the structured content must be a JSON object, never a bare array. List items are
**minimal projections** that carry short scalars only and **drop long/freeform text** to keep
responses small; the complete record (including that text) is fetched with `get_*` or the
`crm://.../{id}` resource. Per entity the omitted fields are:

- **MinimalLead** — drops `notes` (keeps id, name, companyId, email, phone, tags, quality, source,
  status, `unavailableUntil` as a `YYYY-MM-DD` string omitted when unset, contactId, dealId,
  timestamps).
- **MinimalContact** — drops `notes` (keeps id, name, companyId, email, phone, tags, sourceLeadId,
  timestamps).
- **MinimalDeal** — drops `notes` (keeps id, title, contactId, companyId, value, currency, stage,
  timestamps).
- **MinimalCompany** — drops `notes` (keeps id, name, website, industry, phone, timestamps).
- **MinimalOffer** — drops `description` and `body` (keeps id, leadId, title, subject, timestamps).

### Interactive widget UI (embedded, per call)

Every CRUD tool (and `pipeline_summary`) also ships an **interactive UI version** — a
`github.com/techthos/gadget` widget (Table/Form) delivered in **embedded per-call mode**, the
community mcp-ui convention rendered by hosts like LibreChat (gadget's runtime falls back to the
mcp-ui action protocol automatically when no MCP Apps host answers). Contract:

- Each render is a **fresh, self-contained HTML document** with the call's data baked in
  (`InitialData`) and a **unique `ui://leadzaar/<kind>/<render>` URI**, appended to the tool
  result's `content` as an embedded `text/html` resource **after** the JSON text block. The JSON
  result always stands alone — widget build/render failures are logged to stderr and never fail
  the tool.
- Widget actions and form submits target the **normal model-visible tools**: in an mcp-ui host a
  click becomes a follow-up turn where the model runs that tool (argument types are
  model-mediated). Destructive row actions use gadget's inline two-phase `Confirm` (the sandboxed
  iframe has no native `confirm()`).

Per tool kind:

| tool kind                            | embedded widget                                                                                                    |
|--------------------------------------|--------------------------------------------------------------------------------------------------------------------|
| `list_*`                             | **Table** of the returned page (filter box, client-side page size 10, badge-rendered enums, per-row **Delete** — plus **Convert** on leads — with inline confirm) |
| `get_*`                              | one-row **Table** of that record (same columns/actions)                                                            |
| `create_*` / `update_*` success      | prefilled edit **Form** for the saved record; submit targets `update_*` (create forms omit `id`, and the lead create form omits `status`) |
| `create_*` / `update_*` validation failure | the **Form** with the submitted values baked in plus the error under `errors`, mapped **best-effort** onto the field its message names (fallback: the first required field); the retry form targets the failing tool |
| `delete_*`, `convert_lead`           | **refreshed list Table** (default first page) so the effect is visible — embedded mode has no rows-refresh round-trip |
| `pipeline_summary`                   | two read-only **Tables**: deals by stage (one row per stage-currency pair — totals never summed across currencies) and leads by status |

A `get_*`/`update_*` **not-found** error embeds no widget (there is no record to render). New or
changed widgets are product-surface changes and update this spec in the same commit.

### Resources (read-only)

| URI template          | returns                                  |
|-----------------------|------------------------------------------|
| `crm://leads/{id}`    | a single Lead as JSON                     |
| `crm://contacts/{id}` | a single Contact as JSON                  |
| `crm://deals/{id}`    | a single Deal as JSON                     |
| `crm://companies/{id}`| a single Company as JSON                  |
| `crm://offers/{id}`   | a single Offer as JSON                    |
| `crm://pipeline`      | the pipeline summary (same as UC-18)      |

`{id}` is validated as a numeric ID; unknown IDs return a not-found resource error.

### Prompts

| prompt             | purpose                                                                                |
|--------------------|----------------------------------------------------------------------------------------|
| `triage_new_leads` | guide the assistant to review `new`/`contacted` leads and suggest next status/action. A currently-unavailable lead is tagged `[away until DATE]` with an instruction to defer rather than contact it. |
| `draft_followup`   | given a contact (and optionally a deal), draft a follow-up message. Args: contact_id, deal_id? |

## TUI Surface

Built with `github.com/rivo/tview` (`internal/tui`); one `*tview.Application`. The whole UI is a
single `SetRoot` of the shared **sidebar · body · status** skeleton (see
`.claude/rules/tui-rules.md` "Product design standards"). All data flows through `internal/db`
repositories — no bbolt or business logic in handlers. Slow work runs off the event loop and
mutations come back via `QueueUpdateDraw`.

### Layout

```
┌──────────┬──────────────────────────────┐
│ SIDEBAR  │ HEADER (section title · count)│
│ 1 …  ●   │──────────────────────────────│
│ 2 …      │ BODY  (Pages — swappable)     │
│ 3 …      │  list  |  detail   (split)    │
├──────────┴──────────────────────────────┤
│ row 2 of 7   ✓ saved             ? help  │
└──────────────────────────────────────────┘
```

- **Sidebar** (left, fixed width; `Ctrl-B` collapses; auto-collapses on narrow terminals): the
  numbered navigation menu and the app's home — there is no separate home screen. It lists the six
  sections, each with a numeric shortcut and a record-count badge; the active section is highlighted.
- **Body** (right): a header line (section title · record count) above a `Pages` container whose
  visible page is the current section. Create/edit forms open **full-screen** here; modals layer over
  it. Entity sections are a **master-detail split** — a `Table` on the left, a detail pane on the
  right that tracks the highlighted row.
- **Status bar** (bottom, three zones): **context** (`row x of y`, `(filtered)`, `· n selected`) ·
  transient **message/spinner** (async outcomes land here as `✓`/`✗`) · **key hints** ending in
  `? help`.

### Sections

1. **Dashboard** — read-only pipeline summary: deal count + value per stage (grouped by currency)
   and lead counts by status (UC-18). The landing section.
2. **Leads** — master-detail of leads; `n` new, `e`/Enter edit, `c` **convert** (form: make-deal?
   title/value/currency), `o` **new offer** for the selected lead (opens the offer form pre-filled
   with its Lead ID), `d` delete (UC-1,2,4,5,6,24). The detail pane lists the lead's offers. An
   **Away** column flags a lead that is currently unavailable, in `[yellow]` and relative
   (`today`, `in 12d`); an unset or elapsed block renders as the dim em-dash, since both mean
   "contactable now". The detail pane shows the absolute date, marking an elapsed one `(elapsed)`.
3. **Contacts** — master-detail of contacts; `n` new, `e`/Enter edit, `d` delete (cascade, with a
   confirm modal naming the affected deals). The detail pane lists the contact's deals
   (UC-7,8,10,11,12).
4. **Deals** — master-detail of deals; `n` new, `e`/Enter edit, `s` change **stage** (modal), `d`
   delete (UC-13,14,16,17).
5. **Companies** — master-detail of companies; `n` new, `e`/Enter edit, `d` delete (with a confirm
   modal naming how many records will be **unlinked**, not deleted) (UC-19,20,22,23).
6. **Offers** — master-detail of offers; `n` new, `e`/Enter edit, `l` **go to the offer's lead**
   (jumps to the Leads section and highlights it), `d` delete. Each offer links to a lead by **Lead
   ID**; the create/edit form has a multi-line **Body** text area for raw email content, and the
   detail pane shows the full subject/body plus the resolved lead name (UC-24,25,27,28). The lead↔offer
   link is navigable from both sides (`o` on a lead, `l` on an offer).

Every list supports `/` incremental **case-insensitive filter** across the visible columns, `Space`
**multi-select** with batch actions, `r` reload, and renders one of the mandatory
**loading / empty / error** states (never blank). Lists show relative timestamps (`2h ago`); the
detail pane shows absolute ones. Missing values render as a dim em-dash.

### Keys (no F-keys)

Shared vocabulary: `1`–`6` jump to a section; `↑↓` / `j k` move; `Enter` open/confirm; `Esc`
back / cancel / clear-filter; `Ctrl-B` toggle sidebar; `Tab` cycle sidebar↔table↔detail; `/` filter;
`Space` toggle row select; `n`/`e`/`d`/`r` row actions; `c` convert (Leads), `o` new offer (Leads),
`s` stage (Deals), `l` go to lead (Offers); `Ctrl-S` save (forms); `?` help overlay; `q` / `Ctrl-C`
quit. Single letters act while a list is
focused; Ctrl-chords act in forms/inputs so typing never fires an action.

### Forms & confirmation

Create/edit forms are full-screen in the body, one field per row, reused for create and edit (edit
pre-fills). The Lead, Contact, and Deal forms link a Company through a **dropdown picker** (first
option `— none —`, mapping to no link); Lead and Contact forms edit **Tags** as a single
comma-separated field, and the Lead form has a **Quality** field (blank, or an integer `1`–`10`,
live-validated) and an **Away until** field (blank, or a `YYYY-MM-DD` date, live-validated) that
sets `UnavailableUntil` — blanking it clears the block. Validation is **live and per-field** — an
inline `[red]` error appears beneath
an offending field and `Ctrl-S` is blocked while any field is invalid (it focuses the first
offender).
`Esc` cancels, prompting `Discard changes? [y/N]` when the form is dirty. Destructive actions confirm
via a centered modal whose focus **defaults to the safe choice (Cancel)** and which names the target
and warns it cannot be undone; `y` / Enter-on-Yes confirms, `n` / `Esc` cancels. A batch delete names
the count.

Quit: `q` / `Ctrl-C` quit from a top-level list or a button-only modal (stage picker, confirm), which
have no text entry. A dirty form or an in-flight write prompts a confirm first. Inside a text form
`q` is normal input — `Esc` (with the discard prompt) backs out. Header rows are fixed; row-0
selection is guarded. Below 80×24 the UI shows a centered "Terminal too small" notice until resized.

## Acceptance Criteria

- **UC-1 Create lead:** a lead with a non-empty name persists with a fresh monotonic ID,
  `Status = new`, and timestamps set; empty name is rejected; invalid `source` is rejected; a
  `quality` outside `1`–`10` is rejected (`0`/unset is accepted).
- **UC-2 List leads:** by default leads are returned **most-recently-updated first** (`sort_by`
  defaults to `updated`), and the `list_leads` items are **minimal** (no `notes`); filtering by a
  status returns exactly the leads in that status; a `query` substring matches case-insensitively on
  name/company/email/tag; `sort_by` (`updated` default/`created`/`quality`/`unavailable-until`) with
  `order` (`desc` default, `asc`) reorders the full filtered set with ID as a stable tiebreaker;
  results are paginated 1-based with `page_size` clamped to `[1, 50]` (default 50), and the response
  reports `total`, `total_pages`, and `has_more` for the full filtered set. An invalid status or
  `sort_by` is rejected.
- **UC-2 Lead availability filter:** `availability=unavailable` returns exactly the leads whose
  `UnavailableUntil` is still in the future; `availability=available` returns the rest — those with
  no block **and** those whose block has elapsed. The boundary is exclusive: a lead whose block ends
  at the start of today is **available**. Filters compose with status and `query` via AND. Sorting by
  `unavailable-until` ascending returns dated blocks soonest-first with unset dates **last**;
  descending reverses it. An invalid `availability` value is rejected. Every lead in one page is
  judged against a single clock reading, so a scan cannot straddle a date boundary.
- **UC-3 Get lead:** a known ID returns the lead; an unknown ID returns a clean not-found (no panic).
- **UC-4 Update lead:** a **partial update** — only supplied fields change; any field the caller
  omits keeps its stored value (an explicit empty value clears it). Edited fields persist,
  `UpdatedAt` advances, ID and `CreatedAt` are unchanged; an invalid status value is rejected.
  `unavailable_until` follows the same rule: omitted keeps the stored date, `""` clears the block,
  and a value that is not a `YYYY-MM-DD` calendar date (wrong order, prose, an impossible day, or a
  full timestamp) is rejected as a tool error on both `create_lead` and `update_lead`. A stored date
  is normalized to midnight UTC, so any time-of-day supplied through the Go API is dropped.
- **UC-5 Convert lead:** converting a non-converted lead creates a Contact whose fields mirror the
  lead and whose `SourceLeadID` is the lead; with `make_deal` it also creates a Deal for that contact
  with the given value/currency; the lead becomes `converted` with `ContactID` (and `DealID`) set;
  the whole thing is atomic; converting an already-`converted` lead is rejected.
- **UC-6 Delete lead (cascade):** deleting a lead removes the lead, **every** offer with that
  `LeadID`, and all related index entries, atomically; afterward no offer references the deleted lead
  and no `idx_offer_by_lead` entry remains for it; the returned deleted-offer IDs match. Deleting an
  unknown ID is a clean no-op/error, not a panic.
- **UC-7 Create contact:** a named contact persists with a fresh ID and, if email is present, an
  `idx_contact_by_email` entry exists.
- **UC-8 List/search contacts:** email search returns all contacts with that email via the index;
  name-substring and tag filters return the matching contacts; the `list_contacts` items are
  **minimal** (no `notes`), default most-recently-updated first, `sort_by` (`updated`/`created`) with
  `order` reorders, results are paginated with `page_size` clamped to `[1, 50]`, and an invalid
  `sort_by` is rejected.
- **UC-9/10 Get/Update contact:** get returns the record; update persists field changes, advances
  `UpdatedAt`, and rewrites the email index when email changes (old key gone, new key present).
- **UC-11 Delete contact (cascade):** deleting a contact removes the contact, **every** deal with
  that `ContactID`, and all related index entries, atomically; afterward no deal references the
  deleted contact and no `idx_deal_by_contact` entry remains for it.
- **UC-12 Deals for a contact:** returns exactly the deals whose `ContactID` matches, via the index.
- **UC-13 Create deal:** a deal with a non-empty title and an **existing** `contact_id` persists with
  a fresh ID and an `idx_deal_by_contact` entry; a deal referencing a non-existent contact is
  rejected; empty title is rejected; invalid stage is rejected.
- **UC-14 List deals:** unfiltered returns all; `stage`, `contact_id`, and a `query` substring over
  title/company each narrow correctly and compose; the `list_deals` items are **minimal** (no
  `notes`), default most-recently-updated first, `sort_by` (`updated`/`created`) with `order`
  reorders, results are paginated with `page_size` clamped to `[1, 50]`, and an invalid `stage` or
  `sort_by` is rejected.
- **UC-15/16 Get/Update deal:** get returns the record; update persists changes incl. stage, advances
  `UpdatedAt`, and maintains the contact index if `ContactID` changes.
- **UC-17 Delete deal:** the deal and its `idx_deal_by_contact` entry are removed.
- **UC-18 Pipeline summary:** for each deal stage, count and per-currency value totals are correct
  and **never summed across currencies**; lead counts per status are correct; an empty DB yields
  zeroed groups without error.
- **UC-19…22 Company CRUD:** a company with a non-empty name persists with a fresh monotonic ID and
  timestamps; empty name is rejected; get returns the record, update persists changes and advances
  `UpdatedAt` (ID and `CreatedAt` unchanged); list/search returns matches by name/website/industry.
  The `list_companies` items are **minimal** (no `notes`), default most-recently-updated first,
  `sort_by` (`updated`/`created`) with `order` reorders, results are paginated with `page_size`
  clamped to `[1, 50]`, and an invalid `sort_by` is rejected.
- **Company link validation:** creating or updating a Lead, Contact, or Deal with a non-zero
  `CompanyID` that references no existing Company is rejected; `CompanyID = 0` is always accepted; a
  converted Contact inherits the Lead's `CompanyID`.
- **UC-23 Delete company (unlink):** deleting a company removes it and resets `CompanyID = 0` on
  **every** referencing Lead, Contact, and Deal in the same transaction (those records remain and
  keep their other fields); the returned unlinked count equals the number of records changed; no
  record references the deleted company afterward.
- **UC-24…27 Offer CRUD:** an offer with a non-empty title and an **existing** `lead_id` persists
  with a fresh monotonic ID, timestamps, and an `idx_offer_by_lead` entry; empty title is rejected;
  an offer referencing a non-existent lead is rejected; get returns the record; update persists
  changes (incl. body), advances `UpdatedAt` (ID and `CreatedAt` unchanged), and rewrites the lead
  index when `LeadID` changes; list unfiltered returns all, the `lead_id` filter narrows via the
  index, and a `query` substring narrows over title/subject. The `list_offers` items are **minimal**
  (id, leadId, title, subject, timestamps — no `description`/`body`), default most-recently-updated
  first, `sort_by` (`updated`/`created`) with `order` reorders, results are paginated with
  `page_size` clamped to `[1, 50]`, and an invalid `sort_by` is rejected.
- **UC-28 Delete offer:** the offer and its `idx_offer_by_lead` entry are removed.
- **Legacy migration:** a Lead/Contact persisted with a plain-string `company` is upgraded on open to
  a `CompanyID` referencing a (find-or-created, deduped-by-name) Company, and the legacy key is
  dropped; re-running open is a no-op.
- **MCP:** each tool is reachable through an in-process client; input/business errors come back as
  tool-error results (not transport errors); resources return the right record for a valid ID and a
  not-found error otherwise; logs never touch stdout.
- **TUI:** all six sections render via a `SimulationScreen`; numeric keys (`1`–`6`) switch sections
  (including the Offers section) and `q` quits; the `?` help overlay opens and closes; `/` filtering narrows a list and `Esc` clears
  it; a create form blocks `Ctrl-S` while a required field is empty and saves once valid; the convert
  action, the stage-picker modal, the cascade-delete confirm modal, and the Companies section (with
  its company picker in the lead/contact/deal forms and the unlink-on-delete confirm) work; the
  lead/contact "Company" column and detail resolve a `CompanyID` to its name; the lead↔offer link is
  navigable both ways (`o` on a lead opens a new-offer form pre-filled with that lead; `l` on an offer
  jumps to its lead and highlights it); the heavy list loads run off the event loop.
- **Concurrency:** two `Store`s open on the same file concurrently (standing in for the TUI and MCP
  processes) can both read and write it, and a write through one is visible through the other —
  proving the connection-per-operation contract. `TxID()` is stable across reads and strictly
  increases after a committed write, and the TUI's background poll repaints the list when another
  process writes (no manual reload).

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
- **TUI view-state persistence deferred:** the shared design language calls for persisting the last
  active section and the sidebar collapsed/expanded state in a `Config` singleton. v1 does **not**
  persist these — the app always opens on the Dashboard with the sidebar expanded. Adding a UI-state
  config store (kept separate from domain data) is a later candidate and a spec change.
- **Lead email not indexed:** only Contact email is indexed in v1; lead email search (if ever needed)
  would be a primary-bucket scan.
- **Mode selection:** assumed the binary picks TUI vs. MCP at launch (e.g. a flag/subcommand or env
  var, decided in `cmd/`), never running both against the file at once.
- **Single default database:** v1 has one database per machine (the resolved path above), with no
  named profiles or workspaces. Pointing `-db` / `LEADZAAR_DB` at another file is the whole
  multi-database story; a first-class profile concept would be a spec change.
