# Redaction allowlist

This section defines the fields that may be persisted in an audit event. It applies to every action and outcome. Build events from these approved values; do not serialize a request, account, session, error, or other application object and then try to remove sensitive parts.

## Allowed event fields

The seven required fields and their names and types are defined by the [event envelope](01-envelope.md). Only these fields may appear, with the single optional `failure_code` extension described below.

| Field | Allowed value | Source and constraints |
| --- | --- | --- |
| `id` | String | Server-assigned UUIDv7 as defined by the envelope. Never accept an ID from submitted input. |
| `time` | String | Server-owned UTC event time in the envelope's RFC 3339 format with milliseconds. The PRD requires first and last occurrence times for aggregated failures; their names, types, and allowlist extension belong to the counted-failure contract. |
| `action` | String | A fixed, stable machine-readable action identifier from the covered-actions contract. Never copy a submitted action name or label. |
| `actor` | String or `null` | `null` when unknown; otherwise a stable server-owned actor ID, including `local-prototype` where the envelope requires it. Never use a display name, email address, username, or other submitted identifier. |
| `target` | Object or `null` | `null` when there is no specific target. Otherwise the object has exactly the two string fields `kind` and `id`. A resource target uses its stable server-assigned ID. An operation target uses `kind: "operation"` and a fixed, safe operation identifier in `id`. Never include a whole request, account, session, or serialized resource. |
| `outcome` | String | Exactly `success` or `failure`, as defined by the envelope. It is a fixed contract value, not submitted text. |
| `count` | Positive integer | Server-owned number of occurrences represented by this event. A single occurrence is `1`; aggregation rules belong to the counted-failure contract. Never accept a client-supplied count. |
| `failure_code` | String or `null`; optional | A fixed, non-secret, machine-readable failure code when one is defined; otherwise `null`. Do not include error messages, causes, validation details, or submitted text. This optional field does not replace or rename any required envelope field. |

No other top-level field is allowed. No other field is allowed inside `target`. The `failure_code` value may be `null` or a fixed code; it is not a place for arbitrary error detail. A future extension must be explicitly reviewed and added to this allowlist and the relevant envelope contract before an implementation persists it.

## Drop sensitive and unknown data

Drop every field and value outside this allowlist. In particular, never persist:

- Free-form submitted input, request parameters, form values, headers, URLs, query strings, or request bodies, including values placed under an otherwise approved field name.
- Raw sign-in or account identifiers such as an email address, username, or submitted identity text, whether successful or rejected.
- Passwords, password hashes, invitation or reset tokens, token digests, session credentials or digests, cookies, CSRF tokens, or other authentication material.
- SMTP, deployment, database, or other operational secrets and credentials.
- Uploaded bytes, file contents, submitted content, or their excerpts, even when the content appears in a field whose name is on the allowlist.
- Free-form error text, stack traces, SQL, exception details, or user-visible validation messages.
- Unknown keys, unknown nested objects, and arbitrary serialized account, session, request, or resource fields.

Field names do not make values safe. Validate each value against the field's allowed source and type before persistence; a value from an untrusted or free-form source remains disallowed even if it is assigned to `actor`, `target`, `action`, or `failure_code`.

## Projection and failure behavior

Construct the persisted event by explicitly projecting the allowed fields. For `target`, construct a new object containing only `kind` and `id`; do not recursively copy or sanitize an input object. Use fixed server-defined action, operation, outcome, and failure-code values, stable server-owned IDs, and server-owned time and count values.

Redaction must not change the envelope's field names or types, nor remove a required field to make an incomplete event look valid. If a required value is absent, malformed, or cannot be obtained without copying prohibited data, do not persist that candidate event as a valid event. Do not replace it with fabricated or misleading values. The required nullable `actor` and `target` fields use `null` only where the envelope allows it; optional `failure_code` may be `null`.

This is a future recording contract; it does not establish that audit recording is implemented. The [envelope example](../examples/audit-envelope.json) illustrates the allowed operation target shape and contains no submitted identity or error text.
