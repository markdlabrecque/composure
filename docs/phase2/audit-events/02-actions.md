# Covered actions and recording time

This section defines the stable action identifiers and the point at which a
future recorder observes each result. It follows [PRD FR-18](../../prd.md#functional-requirements),
the [Phase 2 audit ticket](../tickets.md#p2-01b-audit-contract-covered-actions-and-timing-59),
and the parent [audit contract issue](https://github.com/markdlabrecque/composure/issues/22).
The envelope and its seven fields are defined in [01-envelope.md](01-envelope.md).
This is a future behavior contract; it does not assert that audit recording
exists today.

## Recording boundary

Record one event for each covered operation, using the same action identifier
for HTTP and CLI entry points. Do not emit separate events for one mutation
merely because it was reached through different interfaces. A successful
state-changing event is observable only when the state change is durably
committed. Where the state and audit row share a database transaction, commit
them together so a crash cannot leave a committed change described as rolled
back or vice versa. For operations that span a database and another durable
boundary, report success only after both the operation's durable result and
its audit record are durable; interrupted work must be reconciled before it is
reported complete. A failure event follows the point at which the operation's
failure or rollback is known. A failed event never claims a state change.

An action attempt begins when an HTTP request reaches its recognized
state-changing route after method dispatch, or when the CLI has selected the
corresponding command. It begins before request or flag validation, so
validation failures are failures of that action. A `GET` that only renders a
form or page is not an attempt. Once an attempt begins, rejected account or
resource state, authorization or other operation preconditions, invalid input,
and execution or transaction failures are failures; a failure event follows
the point at which that result is known and never claims a state change. For
token-consuming actions this includes an unknown, mismatched, expired, or
already-used token. In particular, failed invitation acceptance is an
`account.created` failure, and failed password-reset completion is a
`password.changed` failure. This rule does not classify a form-rendering GET
as an action.

For sign-in, invitation, and password-reset requests, evaluate throttling
before password hashing, SMTP work, or a per-request audit write, as required
by the PRD. A throttled request is not synchronously written as one event per
request; any counted-failure representation follows the later throttling and
retention contract. For an admitted sign-in attempt, record the result after
credential checking and session creation have determined whether sign-in
completed. Apply the same throttle-before-expensive-work and throttle-before-
audit-write ordering to invitation and reset request handling; the later
throttling section defines counting and flush details.

## Action catalog

| Action | Covered operation | Outcome | Recording point |
| --- | --- | --- | --- |
| `account.sign_in` | A sign-in POST through the admin after throttling admits it. | Both. | After credential checking and session creation determine whether sign-in completed. Credential rejection, including an unknown or deactivated account, and failure to establish the session are failures. A throttled request follows the counted-failure rule above. |
| `account.created` | An account is created by invitation acceptance, first-site initialization, or the CLI administrator-recovery command. | Both. | Success after the account, its assigned roles, and (for invitation acceptance) token consumption commit together. Failure after the request/command begins and the create operation is rejected or settles without creating the account. This includes invalid, mismatched, expired, and reused invitation tokens and account-state or validation failures. Use this same action for each entry point. |
| `password.changed` | A password is changed through reset or the CLI recovery command. | Both. | Success after the new hash, required session revocations, and (for reset) token consumption commit together. Failure after the request/command begins and the operation is rejected or settles without changing the password. This includes invalid, mismatched, expired, and reused reset tokens, account-state and validation failures. Use this same action for the CLI path. |
| `roles.changed` | An administrator changes an account's roles. | Both. | For an actual role change, success after the role update and every required target-account session revocation are durable. Submitting unchanged role flags is a no-op and revokes no sessions. Failure after the request/command begins and the update is rejected or fails without changing roles, including validation and account-state failures. |
| `account.deactivated` | An administrator deactivates an account. | Both. | Success after deactivation and session revocation complete durably; failure after the request/command begins and the operation is rejected or fails without deactivating the account, including validation and account-state failures. |
| `configuration.deployed` | A reviewed configuration deployment is applied. | Both. | Success after the deployment is active and its required health check succeeds. Failure after the request/command begins and deployment is rejected, fails, or rolls back, once the final result is known; never record success for a rolled-back deployment. |
| `content.published` | An item is published or republished. | Both. | Success after the publication snapshot and published pointer/route effects commit. Failure after the request begins and publication is rejected or its transaction rolls back, including validation and state-precondition failures. |
| `content.unpublished` | A published item is unpublished. | Both. | Success after the unpublished state and routing effects commit. Failure after the request begins and unpublish is rejected or its transaction rolls back, including validation and state-precondition failures. |
| `menu.published` | A complete menu draft is published. | Both. | Success after the menu snapshot and published pointer commit. Failure after the request begins and publication is rejected or its transaction rolls back, including validation and state-precondition failures. |
| `menu.unpublished` | A published menu is unpublished. | Both. | Success after the menu is no longer public and that state commits. Failure after the request begins and unpublish is rejected or its transaction rolls back, including validation and state-precondition failures. |
| `content.permanently_deleted` | An administrator confirms permanent deletion of a trashed item and its dependent cleanup. | Both. | Success after the item and required dependent cleanup commit atomically. Failure after the confirmation request begins if validation or a state precondition rejects the operation, or execution rolls back. Cancellation before confirmation is not a deletion attempt and emits no event. |
| `configuration.exported` | A versioned site configuration export is created. | Both. | Success after the complete export artifact is finalized and verified. Failure after the request/command begins and the operation settles without a complete artifact; partial output is not success. |
| `site.exported` | A full site recovery export is created. | Both. | Success after the complete package, including its manifest, database backup and referenced files, is finalized and verified. Failure after the request/command begins and the operation settles without a complete package; partial output is not success. |
| `site.restored` | A full recovery package is restored into a fresh site. | Both. | Success after validation and installation/cutover complete durably. Failure after the request/command begins and the restore is rejected, fails, or rolls back; never record success for an incomplete restore. |

Ordinary draft saves, including saves that change content but do not publish it,
do not create audit events. Snapshot restore to a draft is also not publication;
the later publish uses `content.published`. Trash, ordinary account sign-out,
invitation-token issuance, and partial portable exports/imports are not covered
by this catalog. Partial portable exports/imports are outside v1 scope. A
covered operation that has no affected resource uses the envelope's safe
operation target convention; resource targets use the stable resource ID.

CLI account recovery uses `account.created` or `password.changed` as applicable,
with the CLI operator principal as actor. CLI configuration deployment,
configuration export, full export and full restore use their corresponding
catalog identifiers. The action identifier describes the operation, not the
transport, so a single CLI mutation does not produce an extra CLI-specific
event.
