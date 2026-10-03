# Audit events

Status: Contract sections completed through #58–#61. The linked sections define the event envelope, covered actions and recording time, redaction, and counted failures and retention. The #79 log-line recorder reports sink write acceptance; the #131 SQLite recorder reports a committed single-event append. Neither makes an independent domain-state change atomic with the event. Crash and interrupted-work guarantees remain for #191, coordinated with #79/#131.

- [01 Event envelope](audit-events/01-envelope.md)
- [02 Covered actions and recording time](audit-events/02-actions.md)
- [03 Redaction allowlist](audit-events/03-redaction.md)
- [04 Counted failures, flush, and retention](audit-events/04-throttling-and-retention.md)

These documents define a future audit contract. They do not mean audit recording or an audit screen is implemented. The audit log records selected actions and does not reconstruct site state.
