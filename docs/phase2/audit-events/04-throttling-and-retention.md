# Counted failures, flush, and retention

This section completes the [audit event envelope](01-envelope.md), [covered
actions and recording time](02-actions.md), and [redaction allowlist](03-redaction.md).
It defines future behavior; it does not mean an audit recorder, aggregation
worker, or retention job exists today. The [PRD security requirements](../../prd.md#security-and-privacy)
require throttle-before-audit-write ordering, counted repeated authentication
failures, bounded aggregation memory, and configurable 90-day default
retention.

## Ordering and counted failures

For sign-in, invitation, and password-reset request paths, perform the
applicable [throttle admission check](../access-contract/05-throttling.md)
first. A request rejected by that check must not proceed to password hashing,
SMTP work, or a synchronous per-request audit write. Do not sleep in the
request to implement backoff. The throttle contract's rejected requests do
not increment its account/IP counters or extend their windows. Count each throttled rejection as a failure in the in-memory aggregation,
using its covered action and a fixed `throttled` failure code. It must not
create a SQLite write for each rejected request.

Aggregate repeated failures into one event per aggregation group and
aggregation window. A group is determined only by fixed, server-defined
`action`, `outcome`, and `failure_code` values (with absent `failure_code`
treated as `null`). The covered action is the one defined for the attempted
operation by the actions contract (for example, `account.created` for failed
invitation acceptance or `password.changed` for failed reset completion). Do not group by, retain, or emit account/email/IP values,
throttle counter digests, submitted identifiers, request data, actor, or any
other per-request value. Aggregated events use `actor: null` and the action's
fixed operation target, such as `account.sign_in`. This intentionally records
that a class of failures occurred, not which account or address produced each
failure. Successful actions remain individual events with `count: 1`; do not
coalesce successes.

The event has the ordinary envelope fields, with `count` equal to the number
of represented failures. Add `first_time` and `last_time`, both server-owned
UTC RFC 3339 timestamps with milliseconds, for the earliest and latest
occurrence in that aggregation window. Set the required envelope `time` to
`last_time`. Keep an event's `action`, `outcome`, `failure_code`, and operation
target fixed throughout its group. `failure_code` must be a fixed safe code
from an implementation allowlist, or `null`; never put identity, error text,
or other request-derived content there. The two timestamp fields are the only
extension to the [redaction allowlist](03-redaction.md) for counted events.

The grouping domain is finite: use only the covered action identifiers for
throttle-protected sign-in, invitation, and password-reset requests, the
`failure` outcome, and each action's fixed safe failure-code set (including
`null`). CLI recovery and other non-throttled operations remain individual
events under the actions contract. Aggregation capacity is implementation configuration,
but its configured capacity must cover that finite supported grouping domain.
Do not evict an unflushed group or create a group keyed by untrusted input to
make room. If a configuration cannot hold every supported group, reject it
rather than silently discard or misgroup failures. Count values must remain
positive integers representable by the event envelope; do not wrap on
overflow.

An aggregation window begins with the first occurrence added to an empty
group. Flush closes that window; later occurrences start a new window for the
same group. The flush cadence and any count/size trigger are implementation
configuration, not product-level timing promises. Concurrent occurrences
must update a group's count and first/last timestamps consistently.

## Flush, restart, and durability boundary

Flush pending groups periodically and during orderly shutdown. After a
successful flush, remove only the exact occurrences included in the persisted
event; occurrences arriving during the write remain pending in a new/current
group. On a failed flush, retain the pending count and timestamps for retry,
without exceeding the configured memory bound or issuing one audit write per
request. Never report an event as persisted when its sink did not accept it.

Pending in-memory counts are not durable. A process crash or forced shutdown
before successful flush can lose those counts; restart does not reconstruct
them and must not invent a count or claim that all failures survived. A flush
that succeeds before a crash is subject to the selected sink's own guarantees.

Be explicit about the recorder boundary tracked by [#191](https://github.com/markdlabrecque/composure/issues/191): the P2-18 log-line recorder is not proof of durable audit storage or atomicity with a state change. A successful log write establishes only the log sink's documented write result; it does not establish that a line reached stable storage. The later SQLite recorder (P2-45b / #131) can establish a durable audit event only after the database transaction commits. This section does not specify or implement #191's recorder interface, transaction integration, or crash-reconciliation mechanism. In particular, aggregation flush durability must not be presented as atomic with an unrelated state mutation unless that transaction boundary is actually provided and verified.

## Retention

Retain persisted audit events for 90 days by default. Retention is configurable; the deployment's configured duration determines the cutoff, and the default applies when no override is set. A periodic deletion task removes events whose event time is strictly earlier than the current UTC time minus the configured retention duration. An event exactly at the cutoff remains until a later deletion pass. The deletion cadence and scheduling mechanism are implementation configuration; missed runs do not change the cutoff rule. Retention applies to persisted audit events, not to pending in-memory groups, which are governed by flush/restart behavior above.

## Examples

The examples show two separately grouped failure classes. They use synthetic IDs and times; they contain no account, email, IP, throttle-key digest, or submitted identity.

- [Repeated credential failures](../examples/audit-counted-failure.json)
- [Repeated throttled sign-in rejections](../examples/audit-counted-throttled.json)
