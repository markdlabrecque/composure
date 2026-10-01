# Accounts and sessions

This section defines the proposed account and session tables for Phase 2. The [PRD](../../prd.md) FR-02 sets the account lifecycle and independent roles. [Phase 2 tickets](../tickets.md) define the first administrator and account store interfaces. The [Phase 1 content contract](../../phase1/content-contract.md) defines the ID, timestamp, storage, and startup conventions. SQL and the SQLite driver remain in `internal/store`. Phase 1 startup checks schema identity and integrity without running DDL; this contract does not change that rule.

## Accounts

An account has one of two states, `active` or `deactivated`. Deactivation prevents sign-in while retaining the account ID used for historical attribution. Accounts are not permanently deleted in v1. Administrator and editor are independent role flags, so either or both must be set. The PRD requires a site to retain at least one active administrator. A cross-row invariant cannot be enforced by this table's `CHECK`; P2-38 requires the store to guard deactivation and role removal transactionally. Phase 2 ticket P2-11 requires the first account to hold both roles.

IDs use the unprefixed, server-assigned UUIDv7 convention in the Phase 1 content contract. Phase 1's later-user actor example uses `usr_...`; this section does not change that contract or define the mapping for legacy actor values.

| Column | Type | Rule |
| --- | --- | --- |
| `id` | `TEXT` | Primary key; server-assigned UUIDv7. |
| `email` | `TEXT` | Required canonical address: trim surrounding ASCII spaces and lowercase ASCII A-Z. Unique across active and deactivated accounts. |
| `password_hash` | `TEXT` | Required encoded password hash. Hash parameters and format belong to the passwords section. |
| `is_administrator` | `INTEGER` | Required boolean, constrained to `0` or `1`. |
| `is_editor` | `INTEGER` | Required boolean, constrained to `0` or `1`. |
| `state` | `TEXT` | Required, either `active` or `deactivated`. |
| `created_at` | `TEXT` | Required UTC timestamp in the Phase 1 format. |
| `updated_at` | `TEXT` | Required UTC timestamp in the Phase 1 format. |

Normalize email input to the stored form on the server for every account write and lookup path, including web forms, CLI commands, invitation and reset flows. Inputs are trimmed of surrounding ASCII spaces and ASCII uppercase letters are lowercased before storing or querying `email`. Form inputs should display this lowercase form as well, but server normalization remains required. Do not apply provider-specific alias rules such as removing dots or plus-tags.

This rule folds ASCII case only. It does not define Unicode normalization or Unicode case folding, so Unicode code points remain unchanged. The schema check below uses SQLite's ASCII-only `lower()` and space-only `trim()` behavior to require a canonical stored value. Deactivation does not release the canonical address. Whether an account can be reactivated remains outside this section.

## Sessions

A session belongs to one account. Its presented credential is looked up through a stored digest, so a database read does not expose a reusable cookie credential. A session is usable only while its account is active, its `revoked_at` is null, and the supplied current time is before `expires_at`. Cookie name, attributes, lifetime policy, credential generation, and CSRF behavior belong to the later cookie section.

| Column | Type | Rule |
| --- | --- | --- |
| `id` | `TEXT` | Primary key; server-assigned UUIDv7. |
| `account_id` | `TEXT` | Required foreign key to `accounts(id)`. |
| `token_digest` | `BLOB` | Required, unique digest of the presented session credential. |
| `created_at` | `TEXT` | Required UTC timestamp in the Phase 1 format. |
| `expires_at` | `TEXT` | Required UTC timestamp after which the session is unusable. The duration is defined by the cookie contract. |
| `revoked_at` | `TEXT` | Null while not revoked; otherwise the UTC revocation time. |

The unique `accounts.email` index supports lookup by canonical address and prevents duplicate accounts. It applies to every row, including deactivated accounts. The `sessions.account_id` index supports finding and revoking all sessions for one account. The unique `sessions.token_digest` index supports session lookup and prevents duplicate credentials.

## Proposed SQL

The SQL below records the proposed table shape for later store tickets. It is not added to the current `internal/store/schema.sql` by this documentation ticket. Ticket #70 moves the Phase 1 schema into `internal/store/schema/001_page.sql` and adds the account table in `002_accounts.sql`; ticket #73 adds the session table in a later numbered migration. Each migration remains a separate SQL file under `internal/store/schema/`.

```sql
CREATE TABLE accounts (
  id                TEXT PRIMARY KEY,
  email             TEXT NOT NULL UNIQUE CHECK (email = lower(trim(email))),
  password_hash     TEXT NOT NULL,
  is_administrator  INTEGER NOT NULL CHECK (is_administrator IN (0, 1)),
  is_editor         INTEGER NOT NULL CHECK (is_editor IN (0, 1)),
  state             TEXT NOT NULL CHECK (state IN ('active', 'deactivated')),
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  CHECK (is_administrator = 1 OR is_editor = 1)
) STRICT;

CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id),
  token_digest BLOB NOT NULL UNIQUE,
  created_at   TEXT NOT NULL,
  expires_at   TEXT NOT NULL,
  revoked_at   TEXT
) STRICT;

CREATE INDEX sessions_account_id_idx ON sessions(account_id);
```
