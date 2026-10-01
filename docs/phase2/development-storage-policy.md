# Phase 2 development storage policy

## Decision and scope

Mark's [owner decision on issue #189](https://github.com/markdlabrecque/composure/issues/189#issuecomment-5925982387) fixes the supported development path: initialize a fresh disposable site with the current binary. This is development-only. No real sites exist, and no existing-site upgrade is required.

This policy does not provide a migration mechanism, change a version number, or authorize deleting existing data. It does not define production deployment or recovery. The [storage and deployment ADR](../adr/0001-runtime-content-and-deployment.md) still governs those later workflows and their data-preservation requirements.

## Current behavior and the schema-1 mismatch

The following describes code inspected at `0a6c93616be0dbcde4b629de54ec9a785926237c`, not a new compatibility guarantee.

- [Store initialization](../../internal/store/store.go), `InitializeWithConfig`, applies the embedded SQL files in filename order inside the initialization transaction. At this revision these are `001_page.sql` and [002_accounts.sql](../../internal/store/schema/002_accounts.sql). A freshly initialized database contains the Page tables and an empty `accounts` table. It has no `sessions` table and no first administrator.
- Both an older Phase 1 database without `accounts` and a fresh account-capable database carry `PRAGMA user_version = 1`. `SchemaVersion` is also still `1`. The `002` filename is an initialization ordering convention, not schema version 2 or evidence of an installed upgrade. There is no applied-migration ledger.
- `store.Open` checks database identity, integrity, WAL mode, schema/configuration versions and the active configuration. It does not check that `accounts` exists or execute the embedded schema files. An otherwise valid older Phase 1 database can therefore open successfully with this binary. The current `serve` path can listen after that open succeeds.
- [Account store methods](../../internal/store/accounts.go) query `accounts` directly. On an older database without that table, they return a SQLite missing-table error. Successful open, successful Page serving, and the reported schema version do not establish account readiness or protected-admin readiness.

This mismatch is also recorded in the [#70 completion evidence](https://github.com/markdlabrecque/composure/issues/70#issuecomment-5924712120). Fresh account storage is implemented; sign-in, session storage and first-administrator setup must not be inferred from it. The [accounts and sessions contract](access-contract/01-accounts-and-sessions.md) defines the account/session design and identifies later consumers.

## Supported development workflow

Use a new absent or empty directory for each disposable site that needs the current storage shape. Keep any older site separate. With the binary built as described in [repository checks](../testing.md), a fresh-site example is:

```sh
SITE="$(mktemp -d "${TMPDIR:-/tmp}/composure-phase2.XXXXXX")"
/tmp/composure init --site "$SITE"
/tmp/composure init --site "$SITE" --apply
```

`mktemp` explicitly creates an empty directory. The first `init` only prints the plan; the second applies it. No cleanup or deletion is part of this example. `--example` is optional and adds a published demonstration Page, not an account. `--config FILE` selects a validated [Page configuration](../phase1/page-config-v1.md) for the new site. Using an exported definition with `init --config` does not transfer accounts, drafts or publication history and is not an existing-site upgrade or recovery procedure.

[Site initialization](../../internal/site/site.go) refuses non-empty destinations, including a directory containing an older `composure.db`, with or without `--apply`. There is no overwrite or reinitialize flag. It builds a temporary database, closes and checkpoints it, then publishes it without replacing an existing final database. Its failure cleanup targets its temporary files and its own empty directory, not a pre-existing site's data.

If initialization refuses a destination, choose another new directory. Do not bypass the refusal by changing `user_version`, running the numbered SQL files against an old database, replacing `composure.db`, or deleting the old directory. None is a supported upgrade under this policy. Any later decision to discard a known disposable site must be a separate, explicit operator action; no application command may silently make that choice.

## Safety limits and later implementation

Ordinary startup must remain DDL-free. It must not migrate, add missing tables, import configuration, bump markers, rebuild storage, or delete data to make an older site appear compatible. Current startup uses SQLite `mode=rw` and read/check operations rather than a read-only file open. DDL-free does not promise that SQLite creates no WAL/SHM sidecars or that all files remain byte-for-byte unchanged under every open or crash-recovery condition.

The development policy is a supported-use boundary, not a new runtime rejection check. This documentation change does not make startup reject a schema-1 database missing `accounts`, add a tailored compatibility error, or test a new readiness guarantee. Later account/session consumers must not treat marker 1 alone as proof that their tables exist. Any required readiness checks and operator errors need separately scoped code and tests; they must refuse incompatibility without repairing or discarding data at startup.

If real or non-disposable data appears, stop and ask Mark for a preservation and compatibility decision before using a replacement workflow. A future supported upgrade would need its own reviewed versioning decision, preservation procedure and acceptance evidence. This policy supplies none and does not waive those requirements.
