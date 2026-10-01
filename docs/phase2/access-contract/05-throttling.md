# Throttling counters and backoff

This section defines the shared throttle-counter policy for later access and
store tickets. The [PRD](../../prd.md) requires bounded per-account and per-IP
counters for sign-in, invitation, and password-reset requests. Route coverage
and request-to-operation mapping belong to #69. This is a contract, not a
runtime implementation or schema migration. SQL and the SQLite driver remain
in `internal/store`.

## Counter keys

Each counter is scoped by an operation namespace and a subject type. The
caller supplies the same namespace for the account and IP counter in one
admission check. Namespace values are stable, bounded ASCII identifiers
selected from application constants; they are not derived from request data.
The route inventory and assignment of routes to namespaces and subject inputs
belong to #69. Using a namespace keeps separate flows from consuming one
another's limits.

The account subject is a typed identity supplied by the caller: either
`account-id:<UUIDv7>` when the caller has an account ID or
`email:<canonical-email>` for a submitted identity. Use the submitted email
form whether or not it resolves to an account, so known and unknown submitted
addresses follow the same key path. Canonical email uses the
[accounts contract](01-accounts-and-sessions.md): trim surrounding ASCII
spaces and lowercase ASCII A-Z only. Preserve all other code points and do not
apply provider-specific aliases. The type prefix prevents an account ID from
colliding with submitted email text. Do not use display names or arbitrary
submitted strings as account IDs.

The IP subject is a parsed IP address in canonical binary form. Convert
IPv4-mapped IPv6 addresses to IPv4 before encoding. Do not accept arbitrary
forwarded-header values as client addresses. The address source must follow the
deployment's trusted-proxy contract; route inventory in #69 does not decide
proxy trust.

Derive the fixed 32-byte SHA-256 key from these exact bytes, in order:
`43 54 48 4b` (ASCII `CTHK`), version byte `01`, a 4-byte unsigned
big-endian namespace byte length, the namespace's ASCII bytes, a one-byte
subject tag, a 4-byte unsigned big-endian subject byte length, and the subject
bytes. Subject tag `01` means account UUID and its value is the UUID's 16
canonical binary bytes; `02` means email and its value is the canonical
email's UTF-8 bytes; `03` means IPv4 and its value is 4 parsed address bytes;
`04` means IPv6 and its value is 16 parsed address bytes. The lengths count
bytes, not characters. For example, namespace `signin` and email `a@b` encode
as `4354484b01000000067369676e696e0200000003614062` before SHA-256.

Store and index the digest, not raw email, IP, or account-key text. Do not
write counter keys or their raw components to logs or audit events. The digest
is an index, not an authentication secret.

## Admission and window

Account and IP counters are independent. Each starts a fixed 15-minute window
when its first request is admitted. The window expires exactly 15 minutes
after that instant; later admissions do not move its start or expiry. At the
exact expiry instant the old counter is expired and a new request may start a
new window with attempt 1. Rejected requests neither increment the count nor
extend the window or delay.

Check and reserve both applicable counters atomically before password hashing,
SMTP, or an audit write. Admit the request and increment both counters only if
neither counter is currently delayed. If either is delayed, reject the request
without changing either counter. A rejection must not sleep in the request or
write a SQLite row for that rejection. The caller may return its normal
rate-limit response. A request is counted once admitted, regardless of whether
later validation, hashing, account lookup, mail delivery, or the requested
operation succeeds. Do not reset the count on success.

The backoff is assigned after each admitted attempt. Its delay controls when a
subsequent request can be admitted; the attempt at a threshold is itself
admitted if the preceding delay has elapsed. A zero-second delay permits an
immediate next admission. The next-allowed deadline is the earlier of
`admission_time + assigned_delay` and the fixed window expiry. This prevents a
cooldown from keeping a counter active past its window. When the fixed window
expires, its delay expires with it.

| Admitted attempt number | Delay after admission |
| --- | --- |
| 1–5 | 0 seconds |
| 6–10 | 1 second |
| 11–15 | 5 seconds |
| 16–20 | 15 seconds |
| 21 and later | 60 seconds |

Thresholds are inclusive. For example, the 6th attempt can be admitted as soon
as the 5th attempt's zero-second delay permits, then imposes a 1-second delay
before another admission. The 11th attempt similarly imposes 5 seconds after
it is admitted. At the 21st admission and thereafter, the delay is 60 seconds.
Clamp the persisted attempt count at 21 because larger values do not change the
policy. There is no permanent account lockout; admissions can resume after the
applicable delay or fixed window expiry.

For example, if attempt 1 is admitted at 12:00:00, its window expires at
12:15:00. Attempt 6 may be admitted without a delay imposed by attempts 1–5;
if admitted at 12:00:02, attempt 7 is delayed until 12:00:03. Once attempt 10's
1-second delay has elapsed, attempt 11 may be admitted and imposes a 5-second
delay. Once attempt 20's 15-second delay has elapsed, attempt 21 may be
admitted and imposes a 60-second delay. If attempt 21 is admitted at 12:14:30,
that delay is capped at the 12:15:00 window expiry, so a new attempt may start
a fresh window at 12:15:00.

## Bounded active-key storage

Counters must have bounded active-key storage. Expire and clean up counters
whose fixed windows have ended, including when traffic stops. Do not evict a
still-active counter to make room, since that would bypass its delay. If the
store cannot safely account for a new key within its configured bound, fail
closed for that admission. The capacity value and cleanup mechanism are
implementation details for the store ticket; this contract does not choose a
numeric capacity.

## Proposed SQL shape

The following table shape is guidance for the later store ticket; it does not
add DDL to the current schema. A uniqueness constraint on namespace, subject
type, and key digest permits atomic lookup and reservation. Timestamps use the
Phase 1 UTC timestamp format. The stored count stays in the range 1–21.

```sql
CREATE TABLE throttle_counters (
  namespace       TEXT NOT NULL,
  subject_type    TEXT NOT NULL CHECK (subject_type IN ('account', 'ip')),
  key_digest      BLOB NOT NULL CHECK (length(key_digest) = 32),
  window_started_at TEXT NOT NULL,
  window_expires_at TEXT NOT NULL,
  attempt_count   INTEGER NOT NULL CHECK (attempt_count BETWEEN 1 AND 21),
  next_allowed_at TEXT NOT NULL,
  PRIMARY KEY (namespace, subject_type, key_digest),
  CHECK (window_expires_at > window_started_at),
  CHECK (next_allowed_at >= window_started_at),
  CHECK (next_allowed_at <= window_expires_at)
) STRICT;

CREATE INDEX throttle_counters_expiry_idx
  ON throttle_counters(window_expires_at);
```

The store must treat an expired row as absent at `now >= window_expires_at`,
then begin a fresh window on admission. Admission of both subjects and updates
to both rows are one atomic operation. On rejection, neither row is changed.
