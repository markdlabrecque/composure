# Invitation and password-reset tokens

This section defines the proposed token table for invitation and password-reset links. The [PRD](../../prd.md) requires random, single-use, expiring links and says audit records must not contain secrets. The [accounts and sessions contract](01-accounts-and-sessions.md) defines canonical email addresses, IDs, and UTC timestamps. This is a contract for later implementation, not a table in the current Phase 1 schema.

## Token records

Both link types use the same table. The application generates a random, unguessable token value and sends that value in the link. It stores only a digest of the value, as the session contract does for session credentials. The raw value must not be written to the database, logs, or audit events.

| Column | Type | Rule |
| --- | --- | --- |
| `id` | `TEXT` | Primary key; server-assigned UUIDv7. |
| `purpose` | `TEXT` | Required: `invitation` or `password_reset`. |
| `token_digest` | `BLOB` | Required, unique digest of the random value presented in the link. |
| `account_id` | `TEXT` | Null for invitations; for password resets, required foreign key to `accounts(id)`. |
| `email` | `TEXT` | Required canonical address for invitations; null for password resets. Apply the account contract's ASCII trim and lowercase normalization. |
| `created_at` | `TEXT` | Required UTC timestamp in the Phase 1 format. |
| `expires_at` | `TEXT` | Required UTC timestamp. The token is invalid when the supplied current time is equal to or later than this instant. |
| `used_at` | `TEXT` | Null until successful consumption; otherwise the UTC time of consumption. |

An invitation records the address that will become the account's canonical email after acceptance. A password-reset token records the existing account ID, so a later email change does not redirect the reset to another account.

## Issuing and consuming

Generate each value with a cryptographically secure random source so it is unguessable. Resolve the raw value to its digest when looking up a token. A token is eligible only when its purpose matches the operation, `used_at` is null, and the supplied current time is before `expires_at`.

Consume the token in the same database transaction as the operation it authorizes: create the invited account or change the account's password. The transaction must claim an unused, unexpired token atomically and commit the state change and `used_at` together. If the state change fails, roll back token consumption. Concurrent attempts may produce at most one successful operation; later or concurrent reuse must fail. Use the injected UTC clock for issuance, expiry checks, and `used_at`.

The PRD does not set a random-value length or digest algorithm. Those values remain open for the later implementation and security review. The approved lifetimes below define `expires_at` for each token kind.

The approved invitation lifetime is seven days; the approved password-reset lifetime is one hour. Issue both kinds of tokens using the injected server clock, and check expiry against that clock when the token is used.

## Proposed SQL

The SQL below records the proposed shape for P2-25. It is not added to the current schema by this documentation ticket.

```sql
CREATE TABLE tokens (
  id           TEXT PRIMARY KEY,
  purpose      TEXT NOT NULL CHECK (purpose IN ('invitation', 'password_reset')),
  token_digest BLOB NOT NULL UNIQUE,
  account_id   TEXT REFERENCES accounts(id),
  email        TEXT CHECK (email IS NULL OR email = lower(trim(email))),
  created_at   TEXT NOT NULL,
  expires_at   TEXT NOT NULL,
  used_at      TEXT,
  CHECK (
    (purpose = 'invitation' AND account_id IS NULL AND email IS NOT NULL)
    OR
    (purpose = 'password_reset' AND account_id IS NOT NULL AND email IS NULL)
  )
) STRICT;

CREATE INDEX tokens_account_id_idx ON tokens(account_id);
```
