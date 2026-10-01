# CLI and HTTP interface inventory

This section inventories the Phase 2 access boundary. **Phase 2 target** rows are planned interfaces and controls; they are not evidence that the route, command, or authorization is implemented. **Phase 1 prototype** rows identify the current CLI and HTTP surface. The prototype still has a local actor and does not enforce accounts, roles, authenticated sessions, or CSRF. Its host check and cross-origin protection are not substitutes for those controls. Route and command definitions below follow the Phase 2 ticket plan and current implementations, not implied functionality from the role matrix.

`Administrator` and `Editor` mean an active account with that role, as defined in [Roles](06-roles.md); a dual-role account has their union. `Operator` means the fixed CLI principal holding both roles, not the invoking shell user's web account. “Public” means no account role is required. Every admin `POST` uses the CSRF mode specified below, and every route must enforce authorization on direct requests rather than relying on UI visibility.

## Phase 2 target interfaces

| Interface | Method / command and path | Principal and required role | Access controls and contract mapping |
| --- | --- | --- | --- |
| CLI | `composure init --site DIR [--apply] [--admin-email EMAIL] [--example] [--config FILE]` | Operator; both roles for `--apply` (P2-37c). A plan-only invocation is non-mutating. | `--admin-email` with `--apply` creates the first active account with both roles. Read its password from `COMPOSURE_ADMIN_PASSWORD`; hash per [Passwords](03-passwords.md). Reject an existing account. P2-37c checks the operator permission before writing. No browser cookie, CSRF, request token, or throttle applies. |
| CLI | `composure admin reset-password --email EMAIL` | Operator; both roles (P2-37b). | Read the replacement password from `COMPOSURE_ADMIN_PASSWORD`; hash per [Passwords](03-passwords.md), audit the operation without secrets, and revoke all sessions for the target account per [Session revocation](07-revocation.md). No browser cookie, CSRF, request token, or throttle applies. |
| CLI | `composure admin create --email EMAIL` | Operator; both roles (P2-37b). | Recovery command creates an account with both roles and emits an audit event without secrets. No browser cookie, CSRF, request token, or throttle applies. |
| CLI | `composure serve --site DIR [--addr ADDRESS]` | Operator starts the service; no account role is checked at startup. | Startup is not a browser-authenticated operation. Once serving, each HTTP request is checked by its route row below. |
| CLI | `composure version` | Any local caller. | Read-only version information; no account role or browser controls. |
| HTTP | `GET /healthz` | Public health probe. | No session, role, CSRF, token, or throttle. |
| HTTP | `GET /` and `GET /{published-path}` | Public reader. | Published content only; no session, role, CSRF, token, or throttle. An unpublished draft is not exposed. |
| HTTP | `GET /media/{name}` | Public reader. | Public media retrieval only; no session, role, CSRF, token, or throttle. Serve only the stored object named by the route, without directory listing. |
| HTTP | `GET /admin/static/admin.css` | Public static asset. | No session, role, CSRF, token, or throttle. |
| HTTP | `GET /admin/sign-in` | Unauthenticated visitor. | Issue/use the pre-authentication CSRF nonce; no session or role. |
| HTTP | `POST /admin/sign-in` | Unauthenticated visitor; resulting account must be active. | Pre-authentication nonce CSRF, then atomically admit both `signin` throttle subjects before password hashing or audit writes: account subject `email:<canonical submitted email>` and parsed client IP. Generic credential failure. On success create a fresh session and set `__Host-composure_session`; cookie and nonce behavior per [Cookies and CSRF](04-cookies-and-csrf.md). Throttled requests do not hash or write an audit event. |
| HTTP | `POST /admin/sign-out` | Current active signed-in account; any role. | Valid session plus session-derived CSRF. Revoke the current session and clear the session cookie per [Cookies and CSRF](04-cookies-and-csrf.md). |
| HTTP | `GET /invite/{token}` | Unauthenticated visitor presenting an invitation token. | Invitation token must be valid, unused, and unexpired per [Tokens](02-tokens.md). Pre-authentication nonce CSRF form mode; the token authorizes account setup only, not general account permissions. |
| HTTP | `POST /invite/{token}` | Unauthenticated visitor presenting an invitation token. | Pre-authentication nonce CSRF; atomically consume the invitation token with account creation. Apply the assigned role(s) only; password hash per [Passwords](03-passwords.md). Token reuse/expiry fails generically. On success clear the nonce; if sign-in is created as part of the flow, issue a fresh session cookie. No throttle is specified for token acceptance. |
| HTTP | `GET /reset` | Unauthenticated visitor; signed-in role is not required. | Pre-authentication nonce CSRF form mode. A presented session cookie does not change route mode. |
| HTTP | `POST /reset` | Unauthenticated visitor; signed-in role is not required. | Pre-authentication nonce CSRF. Atomically admit both `password_reset` throttle subjects before token issue, SMTP, or audit: `email:<canonical submitted email>` whether known or unknown, and parsed client IP. Keep response indistinguishable for known and unknown addresses; do not emit per-rejected-request audit writes. |
| HTTP | `GET /reset/{token}` | Unauthenticated visitor presenting a password-reset token. | Token must be valid, unused, and unexpired per [Tokens](02-tokens.md). Pre-authentication nonce CSRF form mode. An optional valid session for the target account is considered only for the revocation exception; it does not replace the token or change CSRF mode. |
| HTTP | `POST /reset/{token}` | Unauthenticated visitor presenting a password-reset token. | Pre-authentication nonce CSRF; consume token and update password atomically. Hash per [Passwords](03-passwords.md); revoke all target-account sessions except a valid current session belonging to that same account, per [Session revocation](07-revocation.md). A reset token alone grants no session exception. Clear nonce on success. |
| HTTP | `GET /admin`, `GET /admin/pages`, `GET /admin/pages/new`, `GET /admin/pages/{id}/edit`, `GET /admin/pages/{id}/preview` | Active Editor. An administrator without Editor is denied. | Valid session; Editor permission per [Roles](06-roles.md). No mutation or CSRF on GET. |
| HTTP | `POST /admin/pages`, `POST /admin/pages/{id}`, `POST /admin/pages/{id}/publish` | Active Editor. An administrator without Editor is denied. | Valid session, session-derived CSRF, Editor permission. Content attribution is the signed-in account ID, not a local prototype identity. |
| HTTP | `GET /admin/invitations/new` | Active Administrator. | Valid session; Administrator permission per [Roles](06-roles.md). |
| HTTP | `POST /admin/invitations` | Active Administrator. | Valid session, session-derived CSRF, Administrator permission. Atomically admit `invitation` throttle subjects before token creation, SMTP, or audit: `account-id:<UUIDv7>` of inviting account and parsed client IP. Issue invitation token per [Tokens](02-tokens.md); raw token is only delivered in the invitation link and is not logged/audited. |
| HTTP | `POST /admin/accounts/{id}/roles` | Active Administrator. | Valid session, session-derived CSRF, Administrator permission. Preserve at least one active administrator; on actual role change revoke all sessions of the target account, including the initiator's if target and actor are the same. No throttle. |
| HTTP | `POST /admin/accounts/{id}/deactivate` | Active Administrator. | Valid session, session-derived CSRF, Administrator permission. Preserve at least one active administrator; on success revoke all target sessions. No throttle. |
| HTTP | `GET /admin/settings` | Active Administrator. | Valid session; Administrator permission. |
| HTTP | `POST /admin/settings` | Active Administrator. | Valid session, session-derived CSRF, Administrator permission. No throttle. |
| HTTP | `GET /admin/audit` | Active Administrator. | Valid session; Administrator permission. Audit view must apply the audit contract's redaction rules; no CSRF or throttle. |
| HTTP | `POST /admin/pages/{id}/image` | Active Editor. | Valid session, session-derived CSRF, Editor permission. Apply upload validators and the image worker before storage; no account/IP throttle is specified for uploads. |

The route inventory assigns throttle namespaces as stable application constants: `signin`, `invitation`, and `password_reset`. Both counters for one admission use that same operation namespace and are reserved atomically. Account subjects are canonical email for sign-in and reset request (known and unknown addresses alike), and the inviting account's UUIDv7 for invitation creation. The IP subject is the parsed client IP in canonical binary form, with IPv4-mapped IPv6 normalized to IPv4. Do not derive it from arbitrary forwarded headers; follow the deployment's trusted-proxy configuration. The window, backoff, rejection, bounded-storage, and digest rules are all in [Throttling](05-throttling.md).

The same route and method may receive a redirect or generic rejection for unauthenticated, unauthorized, invalid-token, or invalid-CSRF requests; those outcomes do not relax the principal or controls in the table. All state-changing admin requests are POST in this phase. Safe methods must not mutate state.

## Current Phase 1 prototype (source inventory)

These are the interfaces actually registered in `internal/web/web.go` and `internal/cli/cli.go` at the Phase 2 base revision. They are not security-approved Phase 2 behavior. The `admin` routes currently operate without accounts, sessions, roles, CSRF tokens, token flows, or throttles; draft writes use the `local-prototype` actor. The server currently constrains its listener to loopback and checks Host/cross-origin requests, but these controls do not grant authentication or authorization.

| Interface | Current method / command and path | Current principal / role | Current controls and source |
| --- | --- | --- | --- |
| CLI | `composure init --site DIR [--example] [--apply] [--config FILE]` | Local operator; no account or role. | `--apply` writes the Phase 1 site; no first-account flags yet. `internal/cli/cli.go`. |
| CLI | `composure serve --site DIR [--addr ADDRESS]` | Local operator starts loopback server; HTTP routes have their own current behavior below. | Loopback resolution in `internal/cli/cli.go` and `internal/web/web.go`. |
| CLI | `composure version` | Any local caller. | Read-only. `internal/cli/cli.go`. |
| CLI | `composure config validate --file FILE` | Any local caller. | Validates Page configuration; no account or role. `internal/cli/config.go`. |
| CLI | `composure config export --site DIR --out FILE` | Local operator with filesystem access. | Reads active Page configuration; no account or role. `internal/cli/config.go`. |
| HTTP | `GET /healthz` | Public. | Health response. `internal/web/web.go`. |
| HTTP | `GET /` and `GET /{published-path}` | Public. | Renders published Page or 404. `internal/web/web.go`. |
| HTTP | `GET /admin/static/admin.css` | Public. | Static stylesheet. `internal/web/web.go`. |
| HTTP | `GET /admin` | Prototype local visitor; no role. | Redirects to `/admin/pages`; no session. `internal/web/web.go`. |
| HTTP | `GET /admin/pages` | Prototype local visitor; no role. | Lists Pages; no session. `internal/web/web.go`. |
| HTTP | `GET /admin/pages/new` | Prototype local visitor; no role. | New Page form; no session. `internal/web/web.go`. |
| HTTP | `POST /admin/pages` | Prototype local visitor; no role. | Creates draft; form validation only, no CSRF; `internal/web/web.go`. |
| HTTP | `GET /admin/pages/{id}/edit` | Prototype local visitor; no role. | Loads edit form; no session. `internal/web/web.go`. |
| HTTP | `GET /admin/pages/{id}/preview` | Prototype local visitor; no role. | Renders draft preview; currently unauthenticated. `internal/web/web.go`. |
| HTTP | `POST /admin/pages/{id}` | Prototype local visitor; no role. | Saves draft with actor `local-prototype`; no CSRF. `internal/web/web.go`. |
| HTTP | `POST /admin/pages/{id}/publish` | Prototype local visitor; no role. | Publishes with actor `local-prototype`; no CSRF. `internal/web/web.go`. |

Phase 1 `config validate` and `config export` are listed as existing tools, not new Phase 2 account-management commands. The Phase 2 role contract mentions deployment/export/restore as operator functions; those future Phase 6 commands are outside this Phase 2 inventory because no Phase 2 ticket defines their command syntax or routes. Do not infer authorization for an unlisted future interface from this table. The P2-33/P2-34 command tickets specify `--email` but do not define how those commands select a site/database; this inventory preserves their stated syntax and does not choose a site-selection behavior. Likewise, the role matrix grants administrators account viewing, but the Phase 2 ticket list defines no account-list/read HTTP route; no path is invented here.
