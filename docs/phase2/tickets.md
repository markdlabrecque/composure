# Phase 2 ticket draft: secure access and establish the admin

Status: Opened as GitHub issues #58 to #176 in the Phase 2 milestone on 2026-09-30, while Phase 1 is still in progress. Broken into single-deliverable tickets on 2026-09-30 so a small model agent can take each one in a single test-first pass. The original coarse draft is kept in git history.

Source: [work plan Phase 2](../work_plan.md#phase-2-secure-access-and-establish-the-admin), [PRD](../prd.md) FR-01, FR-02, FR-07, FR-09, FR-16, FR-18 and the security and privacy requirements. Open ticket #22 is the parent of the audit contract tickets.

Outcome for the phase: real users enter the admin with the correct permissions, and sensitive actions have the required controls. Human security review by Mark gates the Hetzner deployment.

Each ticket has one entry point (CLI command, admin HTTP route, public route, package API or document), one storage concern and one observable result. Ticket order follows the plan's five tracer bullets.

## Contracts (start of phase)

### P2-01a Audit contract: event envelope (#58)

Write the `envelope` section of `docs/phase2/audit-events.md`: the JSON fields every event carries (id, time, action, actor, target, outcome, count) with types. Add one parseable example under `docs/phase2/examples/`. No code. Sub-ticket of #22. Create `docs/phase2/audit-events.md` as an index that links section files under `docs/phase2/audit-events/`; put the envelope in the first section file.

Depends on: Phase 1 closed.

### P2-01b Audit contract: covered actions and timing (#59)

Write the `actions` section of `docs/phase2/audit-events.md`: the list of covered actions and, for each, whether success, failure or both are recorded and at what point in the request. No code. Sub-ticket of #22. Write this section in its own file under `docs/phase2/audit-events/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-01a.

### P2-01c Audit contract: redaction allowlist (#60)

Write the `redaction` section of `docs/phase2/audit-events.md`: the allowlist of fields that may appear in an event and the rule that everything else is dropped. No code. Sub-ticket of #22. Write this section in its own file under `docs/phase2/audit-events/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-01a.

### P2-02 Audit contract: throttling, counted failures, flush and retention (#61)

Write the second half of `docs/phase2/audit-events.md`: throttle-before-log ordering, how repeated failures are counted into one event, flush and restart limits, and the 90-day default retention. Add parseable examples of a counted-failure event. No logger code. Sub-ticket of #22. Write this section in its own file under `docs/phase2/audit-events/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-01c.

### P2-03 Access contract: account and session tables (#62)

Write the `accounts` and `sessions` sections of `docs/phase2/access-contract.md`: columns, states (active, deactivated), indexes and the SQL to add to `internal/store/schema.sql`. No code. Create `docs/phase2/access-contract.md` as an index that links section files under `docs/phase2/access-contract/`; put the tables in the first section file.

Depends on: Phase 1 closed.

### P2-04 Access contract: invitation and reset tokens (#63)

Write the `tokens` section of `docs/phase2/access-contract.md`: one table for invitation and reset tokens with purpose, random value, expiry, used-at, and the single-use rule. No code. Write this section in its own file under `docs/phase2/access-contract/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-03.

### P2-05a Access contract: password hashing (#64)

Write the `passwords` section of `docs/phase2/access-contract.md`: Argon2id with fixed memory, time and parallelism parameters, salt length and the stored hash format. No code. Write this section in its own file under `docs/phase2/access-contract/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-03.

### P2-05b Access contract: session cookie and CSRF rules (#65)

Write the `cookies` section of `docs/phase2/access-contract.md`: cookie name and attributes (Secure, HttpOnly, SameSite, lifetime) and how CSRF tokens are generated, stored and checked. No code. Write this section in its own file under `docs/phase2/access-contract/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-03.

### P2-06 Access contract: throttle counters and backoff (#66)

Write the `throttling` section of `docs/phase2/access-contract.md`: the counter key format for account and for IP, the window length, and a fixed backoff table (attempts to delay). Routes that use the counters are listed in P2-08, not here. No code. Write this section in its own file under `docs/phase2/access-contract/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-03.

### P2-07a Access contract: role permission matrix (#67)

Write the `roles` section of `docs/phase2/access-contract.md`: a table of every admin action against the administrator and editor roles, and the last-active-administrator rule. No code. Write this section in its own file under `docs/phase2/access-contract/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-03.

### P2-07b Access contract: session revocation triggers (#68)

Write the `revocation` section of `docs/phase2/access-contract.md`: the events that revoke sessions (password change, role change, deactivation) and which sessions each one revokes. No code. Write this section in its own file under `docs/phase2/access-contract/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-03.

### P2-08 Access contract: CLI command and HTTP route inventory (#69)

Add one table to `docs/phase2/access-contract.md` listing every Phase 2 CLI command and HTTP route with method, path and required role. No code. Write this section in its own file under `docs/phase2/access-contract/` and link it from the index, so contract tickets do not edit the same file.

Depends on: P2-04, P2-05a, P2-05b, P2-06, P2-07a, P2-07b.

## Tracer bullet 1: initialize, sign in and protect a draft

### P2-09 Accounts table and store methods (#70)

First move the Phase 1 schema from `internal/store/schema.sql` to `internal/store/schema/001_page.sql`, embedded with `//go:embed schema/*.sql` and applied in file-name order. Then add `002_accounts.sql` with the `accounts` table from P2-03 and `internal/store/accounts.go` with methods to create an account, get by id, get by email and list. Test that a created account survives closing and reopening the SQLite file.

Depends on: P2-03.

### P2-10 Password hashing package (#71)

Add `internal/auth/password.go` with `Hash` and `Verify` using Argon2id and the P2-05b parameters. Tests cover round trip, wrong password and malformed hash.

Depends on: P2-05a.

### P2-11 First administrator on init (#72)

Extend `init --apply` with `--admin-email`, reading the password from the `COMPOSURE_ADMIN_PASSWORD` environment variable. It creates the first account with both roles and a hashed password. Re-running init with an existing account fails. Process test through the real CLI.

Depends on: P2-09, P2-10.

### P2-12 Sessions table and store methods (#73)

Add the `sessions` table from P2-03 and store methods to create, look up by token, revoke one and revoke all for an account. Test expiry and revocation.

Depends on: P2-09.

### P2-13 Session cookie middleware (#74)

Add middleware in `internal/web` that reads the session cookie, loads the session and account, and puts them on the request context. Cookie attributes follow P2-05b. Tests cover missing, invalid, expired and valid cookies.

Depends on: P2-12.

### P2-14 Sign-in route and screen (#75)

Add `GET /admin/sign-in` and `POST /admin/sign-in`. On a valid email and password the handler creates a session and sets the cookie. Invalid attempts show one generic error. No throttling yet.

Depends on: P2-10, P2-13.

### P2-15 Sign-out route (#76)

Add `POST /admin/sign-out`. It revokes the current session, clears the cookie and redirects to sign-in.

Depends on: P2-14.

### P2-16 Throttle counter store and backoff (#77)

Add the `throttle_counters` table from P2-06 and a package that increments, checks and decays per-key counters with backoff. Unit tests cover window rollover and backoff steps.

Depends on: P2-06, P2-09.

### P2-17 Throttle sign-in before hashing (#78)

Apply P2-16 to the sign-in route by account and by IP. The throttle check runs before password hashing. Tests prove a throttled request never calls the hasher.

Depends on: P2-14, P2-16.

### P2-18 Minimal audit recorder interface (#79)

Add `internal/audit` with the P2-01c event type, a `Recorder` interface and a log-line recorder. Tests validate the envelope against the examples under `docs/phase2/examples/`.

Depends on: P2-02.

### P2-19 Record sign-in success and failure (#80)

Emit audit events from the sign-in route through the P2-18 recorder. Failed sign-in may have no known actor. Throttled attempts are not logged per request.

Depends on: P2-17, P2-18.

### P2-20a Reject unauthenticated admin requests (#81)

Add a middleware on the `/admin` route group that redirects requests without a valid session to sign-in. Direct-request tests cover save and publish without a session.

Depends on: P2-14.

### P2-20b Replace the local-prototype actor (#82)

Replace the Phase 1 `local-prototype` actor in save and publish with the signed-in account id from the request context. A test proves the stored actor is the account id.

Depends on: P2-20a.

### P2-21 Require a session on draft preview (#83)

Apply the P2-20b middleware to the draft preview route. An unauthenticated preview request is rejected. The published public route stays open.

Depends on: P2-20b.

### P2-22 CSRF tokens on state-changing forms (#84)

Add per-session CSRF tokens following P2-05b. Every admin `POST` form carries the token and the handler rejects a missing or wrong token. Tests cover save and publish.

Depends on: P2-20b.

## Tracer bullet 2: invite and recover an account

### P2-23 SMTP relay configuration from deployment secrets (#85)

Add an `smtp` config struct read from environment variables with validation. Exported site configuration must not carry SMTP settings; a test proves the exporter omits them.

Depends on: P2-11.

### P2-24a In-process SMTP capture server for tests (#86)

Add a test helper in `internal/mail/mailtest` that runs a loopback SMTP server and records received messages. Test that one message sent with the standard library arrives.

Depends on: P2-23.

### P2-24b Mailer send function (#87)

Add `mail.Send` that delivers one plain-text message through the P2-23 relay configuration. Test end to end against the P2-24a capture server.

Depends on: P2-24a.

### P2-25 Token table and store methods (#88)

Add the `tokens` table from P2-04 and store methods to issue a random token with purpose and expiry, and to consume it once. Tests cover reuse and expiry.

Depends on: P2-04, P2-09.

### P2-26a Invitation form and token issue (#89)

Add `GET /admin/invitations/new` and `POST /admin/invitations` for administrators. The handler validates the email and issues a P2-25 invitation token. No email is sent yet; the test reads the token from the store.

Depends on: P2-25.

### P2-26b Send the invitation email (#90)

After P2-26a issues a token, send the invitation link through P2-24b. Test that the captured message contains the link.

Depends on: P2-24b, P2-26a.

### P2-27a Accept-invitation creates the account (#91)

Add `GET /invite/{token}` showing a password form and `POST /invite/{token}`. A valid token creates the account, consumes the token and redirects to sign-in. Test the account exists and the token is used.

Depends on: P2-26a.

### P2-27b Sign in after accepting an invitation (#92)

After P2-27a creates the account, create a session and set the cookie instead of redirecting to sign-in. Test the next request is authenticated.

Depends on: P2-27a.

### P2-27c Reject reused and expired invitation tokens (#93)

`GET` and `POST /invite/{token}` show one generic failure page for a used or expired token. Test both cases.

Depends on: P2-27a.

### P2-28 Throttle invitation requests (#94)

Apply the P2-16 counter to `POST /admin/invitations` keyed by the inviting account id and by client IP, using the P2-06 backoff table. A test shows the request over the limit is rejected before a token is issued.

Depends on: P2-26a.

### P2-29a Request-reset form with generic response (#95)

Add `GET /reset` and `POST /reset`. The handler issues a P2-25 reset token for a known address and returns the same page for known and unknown addresses. No email yet; the test reads the token from the store.

Depends on: P2-25.

### P2-29b Send the reset email (#96)

After P2-29a issues a token, send the reset link through P2-24b. Test that the captured message contains the link and that an unknown address sends nothing.

Depends on: P2-24b, P2-29a.

### P2-29c Throttle reset requests (#97)

Apply the P2-16 counter to `POST /reset` keyed by address and by client IP. A test shows the request over the limit is rejected before a token is issued.

Depends on: P2-29a.

### P2-30 Complete-reset route (#98)

Add `GET /reset/{token}` and `POST /reset/{token}`. A valid token sets a new hashed password and is consumed. Reused or expired tokens fail.

Depends on: P2-29c.

### P2-31a Revoke other sessions on password change (#99)

When P2-30 sets a new password, revoke every other session for the account. A test signs in twice, resets, and proves the second session no longer works.

Depends on: P2-30.

### P2-31b Audit event on password change (#100)

Record a `password.changed` event through the P2-18 recorder when P2-30 completes. Test the event envelope.

Depends on: P2-19, P2-30.

### P2-32a Common-password list package (#101)

Add `internal/auth/commonpasswords` with a pinned local list embedded at build time and a `Contains` function. Tests cover a listed and an unlisted password.

Depends on: P2-10.

### P2-32b Password warning on the invitation form (#102)

On the P2-27a form, show a warning when the password is under the recommended length or in the P2-32a list. Submission succeeds only with a confirmation checkbox. Tests cover both triggers.

Depends on: P2-27a, P2-32a.

### P2-32c Password warning on the reset form (#103)

Apply the same warning and confirmation checkbox as P2-32b to the P2-30 form. Tests cover both triggers.

Depends on: P2-30, P2-32b.

### P2-33 CLI administrator password reset (#104)

Add `composure admin reset-password --email` reading the new password from `COMPOSURE_ADMIN_PASSWORD`. It writes an audit event with the CLI operation as actor and revokes the account's sessions.

Depends on: P2-19.

### P2-34 CLI create administrator (#105)

Add `composure admin create --email` for recovery when no administrator can sign in. It creates an account with both roles and writes an audit event with the CLI operation as actor.

Depends on: P2-33.

## Tracer bullet 3: exercise each role on a real action

### P2-35 Permission matrix package (#106)

Add `internal/auth/permissions.go` encoding the P2-07b matrix as a pure function from roles and account state to allowed actions. Table tests cover administrator, editor, both, deactivated and no account.

Depends on: P2-07a, P2-09.

### P2-36a Route permission middleware (#107)

Add a middleware that takes a required action, looks up the account on the request context and applies P2-35. Test it on one route for the five user states: administrator, editor, both, deactivated and unauthenticated.

Depends on: P2-22, P2-35.

### P2-36b Apply permissions to the Page read routes (#108)

Wrap the Page list, edit and preview routes with P2-36a requiring the editor role. A table test covers each route for the five user states.

Depends on: P2-36a.

### P2-36c Apply permissions to the Page write routes (#109)

Wrap the Page save and publish routes with P2-36a requiring the editor role. A table test covers both routes for the five user states. An editor publishes a Page; an administrator without the editor role cannot.

Depends on: P2-36b.

### P2-36d Apply permissions to the invitation routes (#110)

Wrap the P2-26a invitation routes with P2-36a requiring the administrator role. A table test covers both routes for the five user states.

Depends on: P2-36c.

### P2-37a CLI operator principal (#111)

Add a fixed CLI operator principal in `internal/auth` holding both roles, usable with P2-35. Unit test its permissions.

Depends on: P2-35.

### P2-37b Route account CLI commands through the permission check (#112)

Make `admin reset-password` and `admin create` call P2-35 with the P2-37a principal before acting. One test per command proves the check runs.

Depends on: P2-34, P2-37a.

### P2-37c Route init through the permission check (#113)

Make `init --apply` call P2-35 with the P2-37a principal before writing. A test proves the check runs.

Depends on: P2-37b.

### P2-38 Last active administrator guard (#114)

Add a store-level check that refuses to deactivate or remove the administrator role from the last active administrator. Unit tests at the store layer.

Depends on: P2-09.

### P2-39a Role assignment route (#115)

Add `POST /admin/accounts/{id}/roles` for administrators that sets the account's roles. It calls the P2-38 guard. Test the guard refusal and a successful change.

Depends on: P2-36b, P2-38.

### P2-39b Revoke sessions on role change (#116)

After P2-39a changes roles, revoke the account's sessions. Test that a signed-in session stops working.

Depends on: P2-39a.

### P2-39c Audit event on role change (#117)

After P2-39a changes roles, record a `roles.changed` event through the recorder. Test the envelope.

Depends on: P2-39a.

### P2-40a Account deactivation route (#118)

Add `POST /admin/accounts/{id}/deactivate` for administrators. It marks the account deactivated, calls the P2-38 guard and keeps the account's attribution on past content. Test attribution survives.

Depends on: P2-39c.

### P2-40b Revoke sessions on deactivation (#119)

After P2-40a deactivates an account, revoke its sessions. Test that a signed-in session stops working.

Depends on: P2-40a.

### P2-40c Audit event on deactivation (#120)

After P2-40a deactivates an account, record an `account.deactivated` event. Test the envelope.

Depends on: P2-40a.

### P2-41 Last administrator guard on CLI (#121)

Apply P2-38 to the CLI account commands. Tests cover the last administrator through both entry points.

Depends on: P2-37b, P2-38.

### P2-42a Admin shell layout template (#122)

Add a shared admin layout template with a navigation list and render it on a new `GET /admin` index screen. Existing screens are untouched.

Depends on: P2-36b.

### P2-42b Move the Page list into the shell (#123)

Render the Phase 1 Page list screen inside the P2-42a layout. Existing tests still pass.

Depends on: P2-42a.

### P2-42c Move the Page edit screen into the shell (#124)

Render the Phase 1 Page edit screen inside the P2-42a layout. Existing tests still pass.

Depends on: P2-42b.

### P2-42d Move the draft preview into the shell (#125)

Render the Phase 1 draft preview inside the P2-42a layout. Existing tests still pass.

Depends on: P2-42b.

### P2-43a Site settings table and store (#126)

Add a `site_settings` key-value table and store methods to get and set one setting. Test round trip.

Depends on: P2-09.

### P2-43b Site name settings screen (#127)

Add `GET /admin/settings` and `POST /admin/settings` for administrators to set the site name from P2-43a. The shell shows the name.

Depends on: P2-42a, P2-43a.

### P2-44a Shared field error partial (#128)

Add one template partial for a field-level validation error and use it on the Page form. Test that an invalid field renders the partial.

Depends on: P2-42d.

### P2-44b Shared page-level error partial (#129)

Add one template partial for a page-level error message and use it on the sign-in screen. Test that a failed sign-in renders the partial.

Depends on: P2-42a.

### P2-45a Audit events table and store (#130)

Add the `audit_events` table and store methods to append one event and list events by time. Test round trip.

Depends on: P2-18.

### P2-45b SQLite audit recorder (#131)

Implement the P2-18 `Recorder` interface on the P2-45a store and use it in `serve`. The log-line recorder remains for tests.

Depends on: P2-45a.

### P2-46 Counted repeated failures with bounded memory (#132)

Aggregate repeated failures per key in memory with a fixed cap, flushing one counted event per window following P2-02. Test that a rejected request flood produces one write per window and bounded memory.

Depends on: P2-45b.

### P2-47 Audit retention deletion (#133)

Add a periodic deletion of events older than the configured retention, default 90 days. Tests cover the boundary and the config override.

Depends on: P2-45b.

### P2-48a Paged audit query (#134)

Add a store method that returns audit events newest first with a limit and offset. Test paging boundaries.

Depends on: P2-45a.

### P2-48b Audit log screen (#135)

Add `GET /admin/audit` behind the P2-36a middleware requiring the administrator role. It renders one page of P2-48a events in the shell. Test that the page lists a stored event.

Depends on: P2-40c, P2-44a, P2-48a.

## Tracer bullet 4: upload and retrieve one safe image

### P2-49 Upload contract document (#136)

Write `docs/phase2/uploads.md` from the PRD: allowlist of JPEG, PNG, WebP, PDF and DOCX; global, image and document size limits with the lower one winning; dimension and megapixel limits; DOCX ZIP checks; server-generated names. No code.

Depends on: P2-03.

### P2-50 File type allowlist validator (#137)

Add `internal/upload/kind.go` that identifies a file by magic bytes and rejects anything outside the allowlist or with a mismatched extension. Tests include disguised files.

Depends on: P2-49.

### P2-51 Size limit validator (#138)

Add size checks for global, image and document limits where the lowest applicable limit wins. Table tests.

Depends on: P2-50.

### P2-52 Image dimension validator before decoding (#139)

Read image headers to reject over-dimension and over-megapixel images without decoding pixels. Tests include a crafted huge header.

Depends on: P2-50.

### P2-53 DOCX ZIP structure validator (#140)

Check DOCX uploads for required ZIP entries, entry count and uncompressed size limits without extracting. Tests include a ZIP bomb shape.

Depends on: P2-50.

### P2-54 Server-generated storage names (#141)

Add a function that produces a random storage name with the canonical extension for the detected kind. Original names are never used on disk.

Depends on: P2-50.

### P2-55a Fuzz target for the type validator (#142)

Add a bounded `go test -fuzz` target with fixed seeds and a time limit for P2-50.

Depends on: P2-50.

### P2-55b Fuzz target for the size validator (#143)

Add a bounded fuzz target for P2-51.

Depends on: P2-51.

### P2-55c Fuzz target for the image dimension validator (#144)

Add a bounded fuzz target for P2-52.

Depends on: P2-52.

### P2-55d Fuzz target for the DOCX validator (#145)

Add a bounded fuzz target for P2-53.

Depends on: P2-53.

### P2-56a Media table and store (#146)

Add a `media` table (id, storage name, kind, size, width, height, created by) and store methods to insert and get. Test round trip.

Depends on: P2-54.

### P2-56b Media file write (#147)

Add a function that writes upload bytes under the site directory using the P2-54 storage name with restrictive permissions. Test the file exists with the expected mode.

Depends on: P2-56a.

### P2-56c Public media route (#148)

Add `GET /media/{name}` serving a stored file with the correct content type and no directory listing. Test a stored file and a missing name.

Depends on: P2-56b.

### P2-57a Image re-encode with metadata removed (#149)

Add a function that decodes a JPEG, PNG or WebP and re-encodes it without metadata. Test that EXIF is gone and dimensions are unchanged.

Depends on: P2-52.

### P2-57b Single image worker per site (#150)

Add a per-site queue that runs P2-57a one job at a time. Test that concurrent submissions are processed sequentially.

Depends on: P2-57a.

### P2-58a Image upload endpoint with type check (#151)

Add `POST /admin/pages/{id}/image` requiring the editor role. It reads one multipart file, runs the P2-50 type check, stores the file with P2-56b and returns the media id. Tests cover a disguised file and an unauthorized user.

Depends on: P2-36b, P2-56c.

### P2-58b Size and dimension checks on the upload endpoint (#152)

Add the P2-51 and P2-52 checks to P2-58a before storing. Tests cover an oversized file and an over-dimension image.

Depends on: P2-58a.

### P2-58c Re-encode uploads through the image worker (#153)

In the P2-58b handler, pass the validated file through P2-57b and store the worker's output instead of the original bytes. Test that the stored file has no metadata.

Depends on: P2-57b, P2-58b.

### P2-58d Image field on the Page draft (#154)

Add one image field to the Phase 1 Page form that holds a media id from P2-58a and saves it on the draft. Test save and reload.

Depends on: P2-58c.

### P2-59 Render the image on the published Page (#155)

Show the stored image on the published public Page with width, height and alt text.

Depends on: P2-58c.

## Human review gate

These three are Mark's tasks, not agent tickets, and are exempt from the single-pass agent sizing.

### P2-60a Security review: authentication, sessions and CSRF (#156)

Mark reviews sign-in, sessions, cookies, CSRF, throttling and password handling against the PRD. Findings become tickets. Record the reviewed revision.

Depends on: P2-48b.

### P2-60b Security review: roles and account management (#157)

Mark reviews the permission matrix, route and CLI enforcement, deactivation and the last-administrator guard against the PRD. Findings become tickets. Record the reviewed revision.

Depends on: P2-48b.

### P2-60c Security review: uploads (#158)

Mark reviews the upload validators, re-encoding, storage and media serving against the PRD. Findings become tickets. Record the reviewed revision.

Depends on: P2-59.

## Tracer bullet 5: deploy the protected Page to Hetzner

### P2-61 systemd unit template (#159)

Add a systemd unit template under `deploy/` with a separate service user, MemoryMax and CPUQuota. A test renders the template and checks the values.

Depends on: P2-60c.

### P2-62 Caddy and TLS template (#160)

Add a Caddy configuration template with automatic TLS in front of the service. A test renders the template.

Depends on: P2-61.

### P2-63 IP allowlist in the Caddy template (#161)

Add an `ALLOWED_IPS` deployment variable that the P2-62 Caddy template turns into a remote-IP matcher denying everything else. Test the rendered config. Outer authentication is out of scope.

Depends on: P2-62.

### P2-64a Deployment runbook (#162)

Write `docs/phase2/deploy.md`: host preparation, service user, template rendering, first publish and restart steps. No run yet.

Depends on: P2-63.

### P2-64b Record host size and page mix (#163)

Before any run, add a short section to `docs/phase2/evidence/host.md` with the Hetzner host size and the Page mix that will be exercised.

Depends on: P2-64a.

### P2-64c Publish drill on the host (#164)

On the Hetzner host, run the publish step from the P2-64a runbook for the example Page and fetch it with `curl` over HTTPS. Paste the command output into `docs/phase2/evidence/publish.md`.

Depends on: P2-64b.

### P2-64d Restart drill (#165)

Restart the systemd service on the Hetzner host and confirm the published Page is still served. Record the outcome in `docs/phase2/evidence/restart.md`.

Depends on: P2-64c.

### P2-64g Filesystem permissions check (#166)

On the Hetzner host, list the site directory permissions and confirm they match the runbook. Record the outcome in `docs/phase2/evidence/permissions.md`.

Depends on: P2-64d.

### P2-64e Readiness failure drill (#167)

On the Hetzner host, break readiness on purpose and confirm the service reports not ready. Record the result in `docs/phase2/evidence/`.

Depends on: P2-64d.

### P2-64f Structured error log check (#168)

On the Hetzner host, trigger one handled error and confirm a structured JSON error log line appears. Record the result in `docs/phase2/evidence/`.

Depends on: P2-64e.

### P2-65 Smoke load and recorded evidence (#169)

Run a short uncached and cached smoke load without a CDN. Record memory, CPU and latency under `docs/phase2/evidence/`.

Depends on: P2-64f.

## Cross-cutting

### P2-66 govulncheck in CI (#170)

Add govulncheck to the existing required CI check.

Depends on: P2-49.

### P2-67a staticcheck in CI (#171)

Add staticcheck to the existing required CI check. If it reports findings, allow it to warn only until P2-67b.

Depends on: P2-66.

### P2-67b Fix the four current staticcheck findings (#172)

Fix the four findings staticcheck 2026.2.1 reports today: two unused functions in `internal/config`, one unused template variable in `internal/web`, and one capitalized error string in `internal/content`. Delete the unused code and lowercase the string. Original scope excluded CI changes; Mark subsequently authorized retirement of the temporary warning lease and its tests/wiring. Completed #172 delivered those changes in PR #213 without changing the scanner pin.

Depends on: P2-67a.

### P2-67c Make staticcheck fail the check (#173)

Original deliverable: change the P2-67a step from warn to fail. Strict enforcement was delivered with the authorized #172 scope expansion in PR #213. #173 remains open; reconcile its acceptance against that delivery before proposing further implementation or closing the issue.

Depends on: P2-67b.

### P2-68 Fuzz gates in CI (#174)

Run the P2-55d bounded fuzz targets in the existing required CI check.

Depends on: P2-55d, P2-67c.

### P2-69a Pin Go module versions (#175)

Set exact versions for every module in `go.mod` and verify `go.sum` is complete.

Depends on: P2-67c.

### P2-69b Pin CI action revisions (#176)

Pin every GitHub Action in the workflow to a commit SHA.

Depends on: P2-67c.

## Exit checklist

- Direct-request tests cover all five user states (P2-36c, P2-37c).
- Invitation and reset tokens expire and cannot be reused (P2-27c, P2-30).
- Upload controls reject invalid input (P2-58c).
- Throttling before hashing, SMTP and audit writes; generic reset responses; session revocation on password, role and deactivation changes (P2-17, P2-29c, P2-31b, P2-39c, P2-40c).
- Counted failure logging and 90-day retention (P2-46, P2-47).
- CLI recovery writes an audit entry; init grants both roles (P2-11, P2-33, P2-34).
- Human review findings resolved (P2-60c).
- Protected host smoke run recorded (P2-65).
- govulncheck, staticcheck and fuzz gates active (P2-66 to P2-68).

## Open questions for Mark

1. Does the Hetzner host use an IP allowlist or outer authentication for the phase 2 test? P2-63 assumes an allowlist.
2. Which password hashing scheme should P2-05a fix? Argon2id is the assumed default.
3. Logo and colour branding from the original P2-12 is deferred until the upload pipeline lands; should it be a Phase 2 ticket after P2-59 or move to Phase 4?
