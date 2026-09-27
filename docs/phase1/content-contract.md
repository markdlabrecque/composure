# Phase 1 content contract: Page state, storage and runtime

Status: Proposed for review (P1-01, issue #2)
Scope authority: [PRD](../prd.md). Technical baseline: [architecture plan](../architecture_plan.md). Delivery: [work plan](../work_plan.md#phase-1-prove-the-foundation-and-publishing-model).

This contract fixes what the phase 1 tickets build for the one **Page** type: the storage model, the draft/publication rules, URL ownership, versions, the CLI, and the HTTP routes. Sections marked **Future (not implemented in phase 1)** are design constraints for later phases; phase 1 code must not build them, but must not make them impossible. The configuration *file format*, export ZIP manifests, and the audit entry shape belong to P1-02 (#3); this document stores the active configuration but does not define its format.

## 1. Decisions at a glance

| Topic | Decision |
| --- | --- |
| Go toolchain | `go 1.27` / `toolchain go1.27.1` in `go.mod`. CI reads it via `actions/setup-go` `go-version-file`. |
| SQLite driver | `modernc.org/sqlite` v1.59.0 through `database/sql`. Pure-Go (no cgo). `modernc.org/libc` pinned to v1.75.7, exactly as the driver's `go.mod` requires. |
| Other dependencies | None. Standard library only (`net/http`, `html/template`, `flag`, `embed`, `crypto/rand`). |
| Storage model | One row per item holds the working draft. One immutable row per publish snapshot. One `routes` row per owned public path. |
| IDs | UUIDv7, server-assigned, never reused, never client-supplied. |
| Actor | Fixed identity `local-prototype`. No user or administrator account exists in phase 1. |
| Time | UTC, RFC 3339 with milliseconds, stored as text, from an injected clock. |
| Changed published path | Rejected with `path_change_unsupported` until phase 5 redirects exist. |
| Startup | Checks versions and integrity only. Never migrates, never imports configuration. |
| Initialization | Prints a plan. Changes nothing without `--apply`. Refuses a non-empty site directory. |
| Network | Loopback only. There is no external bind mode in phase 1. |

## 2. Toolchain and driver

**Go.** 1.27.1 is the current stable release (1.27.0 released 2026-08-19, 1.27.1 on 2026-09-01; [release history](https://go.dev/doc/devel/release)), with 1.26 the still-supported prior major. `go.mod` declares `go 1.27` and `toolchain go1.27.1`; raise the patch line for security releases without a contract change. Phase 1 uses two standard-library features that pin the minimum:

- `http.ServeMux` method and `{wildcard}` patterns (Go 1.22+). A request whose path matches but whose method does not gets `405` with an `Allow` header.
- `http.CrossOriginProtection` (Go 1.25+), which rejects non-safe cross-origin browser requests using Fetch metadata ([Go 1.25 notes](https://go.dev/doc/go1.25)). Real sessions and CSRF tokens arrive in phase 2.

**SQLite driver.** `modernc.org/sqlite` v1.59.0 (current, 2026-09-15) is selected ([module source](https://gitlab.com/cznic/sqlite)):

- It is a **CGo-free port of SQLite**, so the release binary builds with `CGO_ENABLED=0`, preserving the architecture's single static binary. Its own `go.mod` requires `go 1.25.0`, so Go 1.27 is compatible.
- It compiles against **SQLite 3.53.4**, which supports everything this contract uses: `STRICT` tables, `json_valid()`, WAL mode, and triggers.
- Its `go.mod` requires `modernc.org/libc` **v1.75.7** exactly. The driver and libc upgrade together; a libc mismatch is a build error, not a runtime surprise.

Rejected alternatives, checked against the same documentation: `github.com/mattn/go-sqlite3` requires cgo for every build (breaks the static binary); `github.com/ncruces/go-sqlite3` is also cgo-free but runs SQLite under a WebAssembly layer, adding a runtime without a phase 1 benefit.

**Race detector.** `go test -race` requires cgo and a C compiler on Linux ([race detector docs](https://go.dev/doc/articles/race_detector)). The pure-Go driver does not change that: ordinary builds and tests stay cgo-free, but the race run needs `gcc`, which the GitHub-hosted Ubuntu runners provide. `scripts/test` already runs both the plain and race test passes.

**Connection settings.** Only `internal/store` opens the driver, with this DSN (line breaks added):

```
file:<site>/composure.db?mode=rw
  &_txlock=immediate
  &_pragma=foreign_keys(1)
  &_pragma=busy_timeout(5000)
  &_pragma=synchronous(FULL)
```

- `mode=rw` makes a missing file an error, so startup never creates an empty replacement database. `init` is the one exception: it builds `composure.db.init-<random>` with the same pragmas but file creation enabled, because that file does not exist yet (section 8).
- `_txlock=immediate` makes every `BeginTx` a `BEGIN IMMEDIATE`, so a write transaction holds the write lock before it reads; concurrent saves serialize instead of deadlocking.
- `journal_mode=WAL` is set once by `init` and persists in the file; startup reads `PRAGMA journal_mode` and fails if it is not `wal` (section 8).

## 3. Package boundaries

Module path: `github.com/markdlabrecque/composure`.

| Package | Owns | May import |
| --- | --- | --- |
| `cmd/composure` | `main`: calls `cli.Run(os.Args, stdout, stderr)`, exits with its code | `internal/cli` |
| `internal/cli` | Flag parsing, command dispatch, plan output, exit codes | `site`, `web`, `content`, `config` |
| `internal/site` | Site directory layout, `Init` (plan and apply), `Open` with version and integrity checks | `store`, `content`, `config` |
| `internal/store` | Embedded schema SQL, the DSN, and the SQLite implementation of `content.Repository`. The only package that imports `database/sql` or the driver. | `content`, `config` |
| `internal/content` | Domain types, ID generation, path normalization, validation, the `Repository` interface, and the create/save/publish operations | `config` |
| `internal/config` | The Page type definition types and the configuration codec (document format owned by #3) | standard library |
| `internal/render` | The Page template shared by public pages and preview | `content` |
| `internal/web` | HTTP server, loopback and `Host` checks, public and admin handlers, admin templates | `content`, `render`, `config` |

Rules: dependency direction is downward in the table, so `content` never imports `store`. Handlers call `content` operations and never run SQL. Templates, admin CSS, and schema SQL are embedded with `embed`; the binary reads no files outside the site directory. The clock and ID source are injected into `content` so tests can fix them.

## 4. Site directory and storage

```
<site>/
  composure.db        SQLite database (WAL mode)
  composure.db-wal    created by SQLite while open
  composure.db-shm    created by SQLite while open
```

Phase 1 stores no files; a future `files/` directory holds uploads (section 10).

### Identity and versions in the file

| Marker | Phase 1 value | Meaning |
| --- | --- | --- |
| `PRAGMA application_id` | `0x434D5053` (ASCII `CMPS`) | This file is a Composure site. |
| `PRAGMA user_version` | `1` | Schema version. |
| `site.config_format_version` | `1` | Format version of the document stored in `active_config` (format owned by #3). |

Schema version and configuration format version are **separate numbers**. A schema change never implies a configuration format change, or the reverse.

### Schema version 1

All tables are `STRICT`. Timestamps are text `YYYY-MM-DDTHH:MM:SS.sssZ`. `items.published_snapshot_id` references `snapshots`, which is created after `items`; SQLite resolves the foreign key at DML time, so the circular reference loads.

```sql
CREATE TABLE site (
  singleton             INTEGER PRIMARY KEY CHECK (singleton = 1),
  site_id               TEXT NOT NULL,
  created_at            TEXT NOT NULL,
  config_format_version INTEGER NOT NULL
) STRICT;

CREATE TABLE active_config (
  singleton  INTEGER PRIMARY KEY CHECK (singleton = 1),
  revision   INTEGER NOT NULL CHECK (revision >= 1),
  document   TEXT NOT NULL CHECK (json_valid(document)),
  applied_at TEXT NOT NULL,
  applied_by TEXT NOT NULL
) STRICT;

CREATE TABLE items (
  id                      TEXT PRIMARY KEY,
  type_id                 TEXT NOT NULL,   -- "page" in phase 1; a type ID from the active config
  title                   TEXT NOT NULL,   -- working draft title
  path                    TEXT NOT NULL,   -- working draft path, normalized
  fields                  TEXT NOT NULL CHECK (json_valid(fields)),  -- working draft field values
  draft_revision          INTEGER NOT NULL CHECK (draft_revision >= 1),
  created_at              TEXT NOT NULL,
  created_by              TEXT NOT NULL,
  updated_at              TEXT NOT NULL,
  updated_by              TEXT NOT NULL,
  published_snapshot_id   TEXT REFERENCES snapshots(id)   -- NULL until first publish
) STRICT;

CREATE TABLE snapshots (
  id                    TEXT PRIMARY KEY,
  item_id               TEXT NOT NULL REFERENCES items(id),
  seq                   INTEGER NOT NULL CHECK (seq >= 1),
  type_id               TEXT NOT NULL,
  config_revision       INTEGER NOT NULL,   -- active_config.revision when published
  source_draft_revision INTEGER NOT NULL,   -- items.draft_revision that was published
  title                 TEXT NOT NULL,
  path                  TEXT NOT NULL,
  fields                TEXT NOT NULL CHECK (json_valid(fields)),
  published_at          TEXT NOT NULL,
  published_by          TEXT NOT NULL,
  UNIQUE (item_id, seq)
) STRICT;

CREATE TABLE routes (
  path       TEXT PRIMARY KEY,   -- normalized public path; primary key is the uniqueness rule
  kind       TEXT NOT NULL CHECK (kind IN ('item')),   -- 'redirect' added in phase 5
  item_id    TEXT NOT NULL UNIQUE REFERENCES items(id),
  claimed_at TEXT NOT NULL
) STRICT;

CREATE TRIGGER snapshots_no_update BEFORE UPDATE ON snapshots
BEGIN SELECT RAISE(ABORT, 'snapshots are immutable'); END;

CREATE TRIGGER snapshots_no_delete BEFORE DELETE ON snapshots
BEGIN SELECT RAISE(ABORT, 'snapshots are immutable'); END;
```

Notes:

- `items` is the working draft. There is **no separate drafts table**: every item always has exactly one working draft, so there is no nullable draft to get out of sync.
- `fields` is a JSON object keyed by field ID from the active configuration. The phase 1 Page has one long-text field, `body`; values are JSON strings. Unknown keys are rejected on write.
- `routes` is the single namespace for public paths; its primary key is the path-uniqueness rule. Phase 5 adds `kind = 'redirect'` rows to the same table, so an item and a redirect can never claim one path.
- The two triggers make snapshot immutability a **database rule**, not only an application rule. The future permanent-deletion migration is the only thing allowed to remove `snapshots_no_delete`, and only for the deleted item's own rows (section 10).
- The `init` transaction writes the whole schema, `site`, `active_config`, and the optional example Page.

## 5. Field and metadata rules

### IDs

Item and snapshot IDs are **UUIDv7**: 48 bits of Unix-millisecond time from the injected clock, the version and variant bits fixed by the layout, and the remaining 74 random bits from `crypto/rand`. The entity kind (item, snapshot, and later file, menu, redirect, user) is determined by which table or endpoint holds the ID, not by a prefix. IDs are assigned once by the server, never change, are never reused, and are never derived from a title or path. A request field named `id`, `draft_revision`, or any system column is ignored unless it is a documented form field; an item is addressed only by the ID in the URL.

### Metadata

| Field | Set by | Changes when |
| --- | --- | --- |
| `created_at`, `created_by` | Create | Never. |
| `updated_at`, `updated_by` | Create, then each draft save | A draft save that changes title, path, or any field value. A save with identical values writes nothing. |
| `draft_revision` | Create sets `1` | Increments by 1 on each content-changing draft save. |
| `published_snapshot_id` | Publish | Each successful publish. |
| `snapshots.published_at`, `published_by` | Publish | Never (immutable). |

Actor values are text actor references. In phase 1 the only value is `local-prototype` (shown in the admin as "Local prototype"). Phase 1 never stores or displays a person's name, and **nothing may assume an administrator account exists**. Phase 2 introduces user IDs (`usr_...`) in these same columns and decides how `local-prototype` rows are displayed.

All timestamps come from the injected clock, in UTC. Within one write transaction a single timestamp value is used for every row it writes, so a publish's rows are internally consistent.

### Title and body

- Title: required; leading/trailing whitespace trimmed; at most 200 characters after trimming; no control characters.
- Body (`body`, long text): plain text. `\r\n` and `\r` are stored as `\n`. At most 100,000 characters. Whether it is required comes from the active configuration. The renderer escapes it with `html/template` and preserves line breaks (CSS `white-space: pre-line`); no HTML or Markdown is interpreted.

### Path normalization

An editor types a path; the server normalizes it, then validates it:

1. Trim leading/trailing whitespace.
2. Lowercase ASCII letters.
3. Add a leading `/` if missing.
4. Remove one trailing `/`, except for the root path `/`.

The result must match `^/$|^(/[a-z0-9]+(-[a-z0-9]+)*){1,8}$` and be at most 200 characters. Segments use lowercase letters, digits, and single hyphens; dots, underscores, percent-encoding, empty segments, and `..` are invalid (`path_invalid`). Reserved first segments: `admin`, `static`, `media`, `files`, `healthz`, `api` (`path_reserved`). `/` is allowed.

| Input | Result |
| --- | --- |
| `About-Us` | `/about-us` |
| ` /news/2026/ ` | `/news/2026` |
| `/` | `/` |
| `/about us` | error `path_invalid` |
| `/a//b` | error `path_invalid` |
| `/admin/pages` | error `path_reserved` |

Public requests match the request path exactly after `http.ServeMux` cleaning. `/About-Us` and `/about-us/` return `404` in phase 1 and are not redirected (there is no redirect model yet).

**Path ownership.** A path is *owned* when a `routes` row exists for it. Ownership is created by the first successful publish and persists across later unpublish/trash (section 10). Create and Save draft reject a path owned by another item (`path_taken`); because two never-published drafts own nothing, they may both propose the same path, and the first to publish wins while the second publish fails with `path_taken` and keeps its draft. The check that counts is the one inside the publish transaction.

## 6. States and transitions

An item's state is **derived** from its rows; there is no status column to drift.

| State | `published_snapshot_id` | Route row | Pending changes |
| --- | --- | --- | --- |
| Draft only | `NULL` | none | n/a |
| Published, clean | snapshot S | `path → item` | `draft_revision = S.source_draft_revision` |
| Published, edited | snapshot S | `path → item` | `draft_revision > S.source_draft_revision` |

Public output always comes from the snapshot named by `published_snapshot_id`; preview always comes from the working draft in `items`. Neither reads the other.

### Transition table

| Action | Precondition | Effect in one transaction | Rejected when |
| --- | --- | --- | --- |
| Create | Valid title, path, fields | Insert `items` row with `draft_revision = 1` | Validation fails; `path_reserved`; path owned by another item (`path_taken`) |
| Save draft | Item exists; submitted `draft_revision` equals stored | Update title, path, fields, `updated_*`; increment `draft_revision`. No snapshot, no route change. | `stale_draft`; validation fails; `path_taken`; published and path differs from route (`path_change_unsupported`) |
| First publish | No snapshot yet; `draft_revision` matches; stored draft valid | Insert snapshot `seq = 1`; insert route; set `published_snapshot_id` | `stale_draft`; draft invalid; `path_taken` or `path_reserved` |
| Republish | Published; `draft_revision` matches; draft valid; draft path equals route path | Insert snapshot `seq = n+1`; set `published_snapshot_id`. Earlier snapshots unchanged. | `stale_draft`; draft invalid; `path_change_unsupported` |

Publish always publishes the **stored** draft, never unsaved form values. If the form has unsaved changes, the editor saves first; the `draft_revision` check makes the version the editor previewed the version that goes public. Publishing a clean item still creates a new snapshot: every publish action records one (FR-06).

### Worked transitions

Fixed clock and shortened IDs for readability; real IDs are unprefixed UUIDv7 (section 5). The item is `itm_01`; content version **A** (body `Hello`) is first published as snapshot `snp_01`, and the edited version **B** (body `Hello again`) as `snp_02`.

**New draft** (Create: title `About`, path `about`, body `Hello`)

Before: no rows.

After:

```
items:     itm_01 | "About" | /about | {"body":"Hello"} | draft_revision 1
           created 10:00 local-prototype | updated 10:00 | published_snapshot_id NULL
routes:    (none)     snapshots: (none)
GET /about -> 404
GET /admin/pages/itm_01/preview -> 200 "About / Hello"
```

**First publish** (publish `draft_revision=1` at 10:05)

After:

```
items:     itm_01 | draft_revision 1 | published_snapshot_id snp_01
snapshots: snp_01 | itm_01 | seq 1 | source_draft_revision 1 | "About" | /about | {"body":"Hello"} | 10:05
routes:    /about -> itm_01
GET /about -> 200 "About / Hello"
```

**Edited published draft** (Save body `Hello again` with `draft_revision=1` at 10:10)

After:

```
items:     itm_01 | {"body":"Hello again"} | draft_revision 2 | updated 10:10
           published_snapshot_id snp_01      (pending: 2 > 1)
snapshots: snp_01 unchanged
routes:    /about -> itm_01 unchanged
GET /about -> 200 "About / Hello"                  (visitors still see snapshot 1)
GET /admin/pages/itm_01/preview -> 200 "About / Hello again"
```

**Republish** (publish `draft_revision=2` at 10:15)

After:

```
items:     itm_01 | draft_revision 2 | published_snapshot_id snp_02
snapshots: snp_01 | seq 1 | {"body":"Hello"}         unchanged, byte for byte
           snp_02 | seq 2 | source_draft_revision 2 | {"body":"Hello again"} | 10:15
routes:    /about -> itm_01 unchanged
GET /about -> 200 "About / Hello again"
```

## 7. Repository operations

`content.Repository` is the only data-access boundary; the SQLite implementation lives in `internal/store`.

| Operation | Kind | Behavior |
| --- | --- | --- |
| `ActiveConfig(ctx)` | read | Returns the active definition and its revision. |
| `ListItems(ctx, typeID)` | read | Summaries (ID, title, draft path, public path, updated time, derived state), ordered by `updated_at` desc then ID. |
| `GetItem(ctx, id)` | read | Working draft plus published pointer. `ErrNotFound` if absent. |
| `PublishedByPath(ctx, path)` | read | Joins `routes -> items -> snapshots`. Returns the published snapshot or `ErrNotFound`. |
| `ListSnapshots(ctx, itemID)` | read | Snapshots by `seq`. For phase 1 checks; no screen needed. |
| `CreateItem(ctx, draft, at, actor)` | write | Insert in one transaction, including the route-ownership check. |
| `SaveDraft(ctx, id, expectedRevision, draft, at, actor)` | write | Compare and update in one transaction. `ErrStaleDraft` on revision mismatch, `ErrNotFound` if absent; reports "unchanged" and writes nothing when values are identical. |
| `Publish(ctx, id, expectedRevision, at, actor, validate)` | write | One transaction: load item, check revision, run `validate` on the stored draft, check/insert route, insert snapshot, set pointer. |

Rules:

- Each write operation is exactly one `BEGIN IMMEDIATE` transaction; any error rolls it all back. No write spans two transactions.
- Validation that needs stored state (revision, path ownership) runs **inside** the write transaction.
- Errors are typed sentinels in `content` (`ErrNotFound`, `ErrStaleDraft`, `ErrPathTaken`, `ErrPathChangeUnsupported`, and a `ValidationError` carrying field codes). `store` maps SQLite constraint failures to them: a `routes` primary-key collision becomes `ErrPathTaken`.
- Reads never open a write transaction.

## 8. Versions, startup, and initialization

A phase 1 binary supports **schema version 1** and **configuration format version 1** only. `composure version` prints the binary version and both supported numbers.

### Startup: `composure serve`

Startup runs these checks in order; the first failure stops the process **before it listens**. No check writes to the database.

| Step | Check | Failure message | Exit |
| --- | --- | --- | --- |
| 1 | `--addr` resolves to a loopback address | `serve: address 0.0.0.0:8080 is not loopback; phase 1 serves only 127.0.0.1, ::1 or localhost` | 2 |
| 2 | `<site>` exists and is a directory | `site directory /x does not exist; run composure init --site /x` | 3 |
| 3 | `composure.db` exists and opens with `mode=rw` | `site /x is not initialized (composure.db missing)` | 3 |
| 4 | File is SQLite and `application_id = 0x434D5053` | `/x/composure.db is not a Composure site database` | 3 |
| 5 | `PRAGMA quick_check` returns `ok` and `journal_mode` is `wal` | `/x/composure.db failed integrity check: <detail>` | 3 |
| 6 | `user_version` equals 1 | `0`/older: `schema version 0 is older than supported 1; a migration is required, which phase 1 does not provide`. `2`/newer: `schema version 2 is newer than supported 1; use a newer composure binary` | 4 |
| 7 | `site.config_format_version` equals 1 and `active_config.document` parses | Same wording for configuration format, or `active configuration is invalid: <detail>` | 4 |
| 8 | Listen | `listening on http://127.0.0.1:NNNNN` on stdout | 0 while running |

Startup never runs a migration or changes `user_version`, never reads a configuration file from disk or changes `active_config`, and never creates a database, directory, or example content. An exported configuration file edited on disk has no effect on a restart; it matters only when passed to an explicit command (#3, #11, #12).

`--addr` defaults to `127.0.0.1:8080`. Port `0` picks a free port and the printed line tells tests which. `localhost` is resolved and every result must be loopback.

### Initialization: `composure init`

```
composure init --site DIR [--config FILE] [--example] [--apply]
```

- Without `--apply` it validates everything and prints the plan, changing nothing, not even creating `DIR`. Exit 0.
- With `--apply` it performs exactly the printed plan.
- `DIR` must not exist, or must be empty. A non-empty directory fails with `site directory /x is not empty; phase 1 does not reinitialize sites`, exit 3, with or without `--apply`. There is no reinitialize or overwrite flag; to start over the operator deletes the directory.
- Without `--config` it uses the built-in default Page definition. With `--config` it uses a validated configuration file (format and validation owned by #3, behavior by #12); an invalid file fails before anything is created.
- `--example` adds one published example Page (below). It never copies content from another site.
- Atomicity: apply creates `DIR` if needed, builds the database at `DIR/composure.db.init-<random>` in one transaction, sets WAL mode, closes it, then renames it to `DIR/composure.db`. On any failure it removes the temporary file and any directory it created; a partially built database is never at the final name.

Plan output (stdout):

```
Plan: initialize a Composure site
  site directory:  /home/me/sites/demo (will be created)
  database:        /home/me/sites/demo/composure.db (schema v1, config format v1)
  configuration:   built-in default, revision 1
  content types:   page "Page" (fields: body)
  example content: 1 published Page at /example
No changes made. Re-run with --apply to initialize.
```

With `--apply`, the last line becomes `Initialized site <site_id> at /home/me/sites/demo.`

### Example seed Page

The example is a local demonstration, **not** editorial tooling: it is created only by `init --example`, inside the initialization transaction, stored exactly like editor content (an `items` row, snapshot `seq 1`, and a route, all actor `local-prototype`, with generated IDs). Its content is the fixed local example in [`examples/seed-page.json`](examples/seed-page.json). There is no CLI command to create, edit, or publish content; routine editing happens in the admin.

### CLI conventions

| Exit code | Meaning |
| --- | --- |
| 0 | Success, or a plan printed without `--apply` |
| 1 | Unexpected runtime failure (I/O error, SQLite error) |
| 2 | Usage error: unknown command or flag, invalid address |
| 3 | Invalid input or site state: missing, non-empty, not Composure, corrupt, invalid configuration |
| 4 | Incompatible schema or configuration format version |

Errors go to stderr as one line: `composure <command>: <message>`. Output meant for scripts goes to stdout. Phase 1 commands are `init`, `serve`, and `version`; `config export` and `config validate` arrive with #3 and #11.

## 9. HTTP contract

The server binds only to loopback. Every request must also have a `Host` header of `127.0.0.1:<port>`, `localhost:<port>`, or `[::1]:<port>`; anything else gets `400 Bad Request`. This blocks DNS-rebinding from a page the developer visits. Every non-GET request passes `http.CrossOriginProtection`; a cross-origin browser POST gets `403 Forbidden`.

### Routes

| Method and pattern | Result |
| --- | --- |
| `GET /healthz` | `200`, `text/plain` body `ok`. For process tests. |
| `GET /admin` | `303` to `/admin/pages` |
| `GET /admin/pages` | `200` list of Pages: title, path, state, last update |
| `GET /admin/pages/new` | `200` empty create form |
| `POST /admin/pages` | Created: `303` to `/admin/pages/{id}/edit`. Invalid: `422` form with field errors and submitted values. `path_taken`: `409` with submitted values. |
| `GET /admin/pages/{id}/edit` | `200` edit form with the stored draft and `draft_revision`. Unknown ID: `404`. |
| `POST /admin/pages/{id}` | Save draft. Saved or unchanged: `303` to the edit form. Invalid or `path_change_unsupported`: `422` with submitted values. `stale_draft` or `path_taken`: `409`. Unknown: `404`. |
| `GET /admin/pages/{id}/preview` | `200` the stored draft rendered by `internal/render` under a clear preview banner. Headers `Cache-Control: no-store`, `X-Robots-Tag: noindex`. Unknown: `404`. |
| `POST /admin/pages/{id}/publish` | Form carries `draft_revision`. Published: `303` to the edit form with a notice. Draft invalid or path change: `422`. `stale_draft` or `path_taken`: `409`. Unknown: `404`. |
| `GET /{path...}` | Published Page for the exact path: `200`. Otherwise `404`. Never shows draft data. |

Other rules:

- A path that matches with the wrong method gets `405` and an `Allow` header from `http.ServeMux`. `GET` routes also answer `HEAD`. No `GET` request changes data.
- Mutating requests must be `application/x-www-form-urlencoded`; anything else gets `415`. Bodies over 1 MiB get `413`.
- Form fields: `title`, `path`, one field per configured field ID (`body`), and `draft_revision` on save and publish. Other submitted fields are ignored and cannot set system columns; the item is addressed only by the URL `{id}`.
- Error and form pages are HTML from `html/template`; error codes appear as `data-error-code` attributes so tests assert on them without matching prose.
- Admin responses send `Cache-Control: no-store`. Public responses send no cache headers in phase 1; phase 3 adds caching and invalidation.

### Error codes

| Code | Field | Meaning | Status |
| --- | --- | --- | --- |
| `required` | any | Required value is empty | 422 |
| `too_long` | any | Over the length limit | 422 |
| `invalid_text` | title | Contains control characters | 422 |
| `path_invalid` | path | Fails the normalization grammar | 422 |
| `path_reserved` | path | First segment is reserved | 422 |
| `path_taken` | path | Another item owns the path. The message names that item's title and admin link. | 409 |
| `path_change_unsupported` | path | Item is published and the path differs from its public path. Message: `Changing a published Page's path needs redirects, which arrive in a later phase. Keep /about for now.` | 422 |
| `stale_draft` | form | The draft changed since this form loaded. Reload to see the latest version; your text is kept below. | 409 |

## 10. Future capabilities

**Future (not implemented in phase 1).** Nothing here is built in phase 1. Each item states the rule later phases must follow and how it fits the phase 1 tables.

**Relationships (phase 3).** A relationship value in `fields` stores target item IDs, never paths or titles. An index table `item_references(owner_kind, owner_id, field_id, target_item_id)` is maintained in the same transaction as the draft or snapshot that holds the value, where `owner_kind` is `draft` or `snapshot`; it exists for dependency previews and cleanup, and `fields` stays the source of truth. Public rendering and preview show a related item only if the target is currently published, using its published snapshot; a relationship may point at an unpublished or trashed target, in which case the link is hidden publicly but kept in storage.

**Snapshot restore (phase 3; missing references phase 5).** Restore copies a snapshot's title, path, and fields into the working draft as a normal draft save: a new `draft_revision`, no new snapshot, no public change (FR-06). If a referenced target no longer exists, restore omits that ID from the draft and warns the editor with the field label and the missing ID; the snapshot keeps the ID, so history is never rewritten. The snapshot records `type_id` and `config_revision`; for a field removed from the model, restore reports the dropped values to the editor instead of silently discarding them. A restored path that differs from the item's current public path follows the phase 5 path-change rule (or `path_change_unsupported` before phase 5).

**File references (phase 4).** Uploaded files are write-once rows with server-generated storage names under `<site>/files/`; a replacement uploads a new file and never overwrites one. Field values store file IDs, and a `file_references` index works like `item_references`. A file stays while any draft or snapshot references it; trashed items keep their rows, so their files stay. Deletion is allowed only after the last reference is gone **and** the content change has committed (FR-07); phase 4 may delay deletion to protect rollback, which the PRD's "may delete" permits.

**Whole-menu snapshots (phase 5).** Menu definitions live in configuration; menu items live in working-draft rows per menu. Publishing a menu writes one immutable `menu_snapshots` row holding the whole tree as JSON, then moves the menu's published pointer, in one transaction (FR-12). Restore copies a menu snapshot into the working draft. Menu targets are item IDs or external URLs; links to unpublished, trashed, or missing items are hidden from public output.

**Trash (phase 5).** Trash adds `trashed_at` and `trashed_by` to `items`; trashing and restoring are single transactions. Trashing removes the item from normal lists and public routing; restore brings it back unpublished (`published_snapshot_id` becomes NULL) with every snapshot retained (FR-04). There is no automatic expiry. Permanent deletion (administrators only) previews every menu item, redirect, and relationship it affects, then applies the item deletion and reference cleanup in one transaction. It is the only operation allowed to delete snapshot rows, and only the deleted item's own; the migration that adds it replaces `snapshots_no_delete` with a trigger permitting exactly that case. Other items' snapshots that reference the deleted item stay unchanged; restore handles the missing link.

**Unpublish and path ownership (phase 3 and 5).** Unpublish sets `published_snapshot_id` to NULL and keeps every snapshot. An unpublished or trashed item keeps its `routes` row, so its path stays reserved and republishing cannot hit a conflict; the public route returns `404` while no snapshot is published. The reservation ends on permanent deletion or a path change.

**Redirects and reserved sources (phase 5).** Redirect rules are `routes` rows with `kind = 'redirect'`, so a redirect source and an item path can never coincide; the phase 5 migration widens the `kind` check and scopes the `item_id` uniqueness to `kind = 'item'` rows. An enabled redirect reserves its source even while suspended: a redirect whose target item is unpublished or trashed is suspended, and republication resumes it unless the editor manually disabled the rule (FR-17). A published path change moves the item's route to the new path and creates a permanent redirect row from the old path, in one transaction. Until phase 5 ships, a published path change is rejected with `path_change_unsupported` (section 9). When a reserved path blocks an editor, the error names the rule or item that holds it.

## 11. Worked examples and future tests

Each example names the executable test that must prove it and the ticket or phase that owns that test.

| # | Example | Steps and expected result | Future test | Owner |
| --- | --- | --- | --- | --- |
| E1 | Restart | `init --example --apply`, start, `GET /example` → `200` with the seed body; stop and start again; same body, same item and snapshot IDs. | `TestServeRestartKeepsPublishedPage` (process test) | #4 |
| E2 | Draft isolation | Create a Page at `/about`; `GET /about` → `404`; preview → `200`; `routes` and `snapshots` empty. Publish A; save draft B; preview shows B; `GET /about` still shows A. | `TestDraftOnlyPageIsNotPublic` (#6), `TestPreviewShowsDraftNotPublic` (#8), `TestDraftSaveKeepsPublicVersion` (#10) | #6, #8, #10 |
| E3 | Republish | Publish A; save B; publish B → public B, snapshots `seq 1` (A) and `seq 2` (B); snapshot 1 unchanged byte for byte; restart keeps both. | `TestRepublishAddsSnapshotAndKeepsHistory` | #10 |
| E4 | Duplicate path | Page X published at `/about`. Create Page Y with path `About/` → normalizes to `/about` → `409 path_taken` naming X; no row added. Separately, two drafts both propose `/contact`; the first to publish wins, the second gets `409 path_taken` and keeps its draft. | `TestCreateRejectsPathOwnedByAnotherItem` (#6), `TestPublishRejectsTakenPathAtomically` (#9) | #6, #9 |
| E5 | Blocked published-path edit | Page published at `/about`; save with path `/about-us` → `422 path_change_unsupported`; the form keeps `/about-us` and other entered text; the stored draft, route, and public page are unchanged. | `TestPublishedPathChangeRejected` | #7, #9 |
| E6 | Incompatible version | On a copy of a site, set `user_version` to `2`; `serve` exits `4` with the "newer than supported" message and nothing listens; the file bytes are unchanged. Repeat for `0` and for `config_format_version` `2`. | `TestServeRejectsUnsupportedSchemaVersion`, `TestServeRejectsUnsupportedConfigFormat` | #5 |
| E7 | Snapshot restore after a missing relationship | Event E links Location L; publish E (snapshot holds L's ID); permanently delete L; restore E's snapshot → the draft lacks L's ID, the editor sees a warning naming the field; the snapshot still holds L's ID; public E is unchanged until publish. | `TestRestoreOmitsDeletedReferenceAndWarns` | Phase 5, bullet 4 |
| E8 | Retained files in Trash | A Page with image F is published; trash the Page → F is still stored and its references counted; restore → the Page is unpublished and F intact; permanently delete the item that is F's last reference → F is removed only after commit, and a failed deletion leaves F. | `TestTrashedItemRetainsFiles`, `TestOrphanRemovedOnlyAfterCommit` | Phase 4, bullet 3; phase 5, bullet 3 |
| E9 | Stale form | Two tabs load revision 1; tab 1 saves (revision 2); tab 2 saves or publishes → `409 stale_draft`, the submitted text is shown, and nothing is written. | `TestStaleDraftRejected` | #7, #9 |
| E10 | Reinitialize refused | `init --apply` on a directory containing `composure.db` → exit `3`, database bytes unchanged. `init` without `--apply` on an empty path creates nothing. | `TestInitRefusesNonEmptySite`, `TestInitPlanChangesNothing` | #4 |
| E11 | Failed publish | Inject a failure after the snapshot insert → rollback; no snapshot, no route, pointer unchanged, draft intact. | `TestPublishFailureLeavesNoPartialState` | #9, #10 |

## 12. Requirement trace

| Requirement | What phase 1 fixes here | Left for later |
| --- | --- | --- |
| FR-03 | Type and field IDs come from the active configuration; field values are keyed by field ID. | Field builder, model changes (phase 3). |
| FR-05 | Draft and public data separated by table; preview renders the stored draft; unpublished paths 404. | Unpublish (phase 3). |
| FR-06 | Snapshots immutable by trigger, with actor and time; draft saves add no history. | Snapshot list and restore (phase 3), missing references (phase 5). |
| FR-08 | Public route renders only published snapshots at owned paths. | Five types, theme, menus. |
| FR-15 | Separate schema and config format versions; startup never imports configuration. | Deploy command (phase 6). |
| FR-16 | Stable server-assigned IDs survive export and restore; `init` prints its plan before `--apply`. | Export and import (phase 6). |
| FR-17 | One `routes` namespace; published path changes rejected until redirects exist. | Redirects (phase 5). |
| FR-18 | Actor recorded on every write; draft saves create no audit entry. | Audit log (#3 format; phase 2 recording). |

Phase 1 lays the foundations for these requirements; it completes none of them.

## 13. Decisions to confirm

These are the judgment calls a reviewer should confirm or change before #4 starts. Each is consistent with the PRD; none contradicts it.

1. **Unpublished and trashed items keep their path** (section 10). The PRD reserves paths for redirects but is silent on an item's own path while unpublished or trashed. This contract keeps it reserved, so republishing never fails on a conflict.
2. **Non-canonical public URLs return 404.** Redirecting `/About/` to `/about` would be friendlier, but phase 1 has no redirect model; phase 5 can add it.
3. **Race tests need cgo.** Release builds stay cgo-free; CI must keep `gcc` available for `go test -race`.
