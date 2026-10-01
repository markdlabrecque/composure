# Event envelope

Every audit event is a JSON object with these seven required fields. Event identifiers and times follow the Phase 1 content contract. This document defines the envelope only. Later contract sections will define covered actions and recording time, the field allowlist and redaction behavior, and count aggregation, flushing, and retention. Those decisions belong to their respective tickets.

| Field | JSON type | Contract |
| --- | --- | --- |
| `id` | string | Server-assigned UUIDv7, unique and never reused. It follows the ID rules in the [Phase 1 content contract](../../phase1/content-contract.md#ids). |
| `time` | string | UTC timestamp formatted as RFC 3339 with milliseconds, following the [Phase 1 time contract](../../phase1/content-contract.md#metadata). It records the event occurrence time; the actions section defines the request point at which each action is recorded, and the throttling and retention section defines time handling for aggregated events. |
| `action` | string | Stable machine-readable action identifier. The covered action catalog is defined in the actions section. |
| `actor` | string or `null` | Opaque, stable actor reference when known. Use the account's stable ID for an account, and preserve `local-prototype` for Phase 1 writes. Use `null` when no actor is known, such as an unauthenticated sign-in attempt. Do not store a display name or submitted identity text here. |
| `target` | object or `null` | The affected resource or operation when one exists. A target object has exactly `kind` and `id`, both strings. For a resource, `kind` identifies its entity type and `id` is its server-assigned stable ID. For an operation without a resource, use `kind: "operation"` and a fixed, safe operation identifier in `id`. Use `null` when there is no specific target. |
| `outcome` | string | `success` or `failure`. Failed attempts are represented as failures even when the actor or target is unknown. |
| `count` | integer | Positive number of occurrences represented by this event. A single occurrence has count `1`; combining repeated events and its time window are defined in the throttling and retention section. |

These fields carry event identity and context, not arbitrary request data. Never place passwords, reset or invitation tokens, uploaded content, or other secrets in them. The redaction section defines which additional fields, if any, may be recorded.

The [parseable JSON example](../examples/audit-envelope.json) is illustrative. Its action and target identifiers are examples, not the complete action or operation catalog. Audit behavior described here is a contract for later implementation, not evidence that a recorder or audit log exists today.

```json
{
  "id": "01a0f39f-e3a3-7abc-8def-0123456789ab",
  "time": "2026-09-30T18:42:17.123Z",
  "action": "account.sign_in",
  "actor": null,
  "target": {
    "kind": "operation",
    "id": "account.sign_in"
  },
  "outcome": "failure",
  "count": 1
}
```
