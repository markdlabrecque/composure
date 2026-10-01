# Cookies and CSRF

This section defines the browser credentials and request checks used by the Phase 2 admin. The [accounts and sessions contract](01-accounts-and-sessions.md) defines the session row and stores only a digest of its credential. The server uses that row to validate each presented session before authorizing an admin request.

## Session cookie

The session credential is 32 bytes from the operating system cryptographic random source, encoded as 43 unpadded base64url characters. Decode it strictly and require the canonical encoding before use. Store the 32-byte SHA-256 digest of the raw credential bytes in `sessions.token_digest`. Do not log or otherwise persist the raw credential.

Set the credential in this cookie:

| Attribute | Value | Purpose |
| --- | --- | --- |
| Name | `__Host-composure_session` | The `__Host-` prefix restricts the cookie to the host that set it. |
| `Secure` | Required | Send it only over a secure connection. |
| `HttpOnly` | Required | Keep page scripts from reading the session credential. |
| `SameSite` | `Lax` | Keep it off cross-site POSTs while allowing ordinary top-level links to the admin. |
| `Path` | `/` | Required by the `__Host-` prefix and covers the admin routes. |
| `Domain` | Omitted | Keep the cookie scoped to the setting host, not its subdomains. |
| `Expires`, `Max-Age` | Omitted | Make this a browser-session cookie. |

The prefix is enforced by supporting browsers only when `Secure` is set, `Path=/`, and `Domain` is absent. Always set `Secure`, including local browser testing; use HTTPS to test the cookie. Do not weaken the attribute for local HTTP. The prefix creates a host boundary. Independent installations or mutually untrusted services require separate hostnames; ports and URL paths do not isolate cookies, and both cookies use `Path=/` ([RFC 6265, sections 8.5–8.6](https://www.rfc-editor.org/rfc/rfc6265#section-8.5)).

Set `sessions.expires_at` to the session creation time plus eight hours. This is a fixed absolute lifetime. Requests do not extend it, and there is no separate idle timeout. On every request, accept the session only if its account is active, `revoked_at` is null, and the current server time is before `expires_at`. Reject duplicate session-cookie values. The database expiry is authoritative even when a browser restores session cookies after restart. Once the session expires or is revoked, reject it and clear the cookie. Sign-out revokes the session and clears the cookie using the same name and scope attributes; the deletion response may include an expiry directive to remove it.

On successful sign-in, create a new session credential and session row. Do not promote or reuse any pre-authentication nonce as the authenticated credential. An application restart does not invalidate otherwise-valid sessions: the stored digest and expiry remain in SQLite, and the CSRF value below can be recomputed from the presented credential.

## CSRF tokens

Every admin `POST` form carries exactly one hidden `csrf_token` field. This includes sign-in, sign-out, and pre-authentication forms such as invitation acceptance and password-reset completion. A CSRF token is not an authentication credential.

Select the token mode by route, not by whether a request happens to carry a session cookie. The form `GET` and matching `POST` routes for `/admin/sign-in`, `/invite/{token}`, `/reset`, and `/reset/{token}` use the pre-authentication nonce. Their `POST` requests also require exactly one hidden `csrf_token` field. Every other admin `POST`, including sign-out, requires a valid authenticated session and its session-derived token.

For an authenticated session, derive the token as the unpadded base64url encoding of HMAC-SHA-256, keyed by the raw session credential, over the fixed UTF-8 message `composure:csrf:authenticated:v1`. Render that value in the form. On `POST`, first validate the session credential against `sessions.token_digest` and its account, revocation, and expiry state. Then derive the expected token again and compare it to the submitted field in constant time. The derivation is bound to that session credential, needs no additional table or application secret, and remains stable across an application restart. A new session credential produces a different token.

The pre-authentication routes always use the nonce mode even when a request also carries a session cookie. Their route-specific input or token determines whether the transition proceeds; a session cookie does not authorize them or change their CSRF mode. In particular, `/reset` remains the reset-request form and does not require a current password or a reset token. For `/reset/{token}`, validate an optional session cookie for the current-session revocation exception in [session revocation](07-revocation.md), but never let it replace the required reset token or change the CSRF mode. A missing, expired, or otherwise invalid session cookie grants no authenticated access. An expired session cookie is cleared. Duplicate values of any security cookie fail closed, including on these routes.

These forms use a separate 32-byte random nonce in a `__Host-composure_csrf_nonce` cookie. Encode it as 43 unpadded base64url characters and decode it strictly. Set `Secure`, `HttpOnly`, `SameSite=Lax`, and `Path=/`, omit `Domain`, `Expires`, and `Max-Age`, and never render or echo the nonce itself. Derive the hidden `csrf_token` as unpadded base64url HMAC-SHA-256 keyed by the nonce over the fixed UTF-8 message `composure:csrf:preauth:v1`. On the matching `POST`, require exactly one well-formed nonce cookie and exactly one well-formed hidden token, derive the expected value, and compare in constant time. Reject missing values, malformed values, and mismatches. On a form `GET`, generate a new nonce if there is not exactly one well-formed nonce cookie. Keep the same nonce for other form reads in that browser session so opening another form does not invalidate an already-open form. The `__Host-` prefix blocks sibling-domain cookie injection in browsers that enforce it. Clear the nonce after successful sign-in, invitation acceptance, or password-reset completion; a later form `GET` gets a fresh nonce.

CSRF tokens appear only in server-rendered hidden fields and submitted form bodies. Never put them in URLs, response redirects, audit events, or logs. Never include submitted values in CSRF rejection messages. Admin responses remain `Cache-Control: no-store` so a cached form cannot expose a stale token.

## Request checks and rejection

Keep `http.CrossOriginProtection` and the existing Host validation in front of admin handlers. They reject detected cross-origin browser requests and unexpected hosts. Requests without the browser's Fetch Metadata or Origin headers may pass the cross-origin check, so the token check remains mandatory. SameSite and these checks supplement the token check; they do not replace it. No safe method may change application state.

Apply the CSRF check to every admin `POST`, including pre-authentication transitions. An authenticated route requires a valid session cookie and its session-derived token; it must never fall back to the pre-authentication nonce. Parse a bounded form body once, require exactly one token field with the expected encoding and length, and perform all session and token checks before any state mutation. Reject duplicate security-cookie values. A missing, duplicate, malformed, or mismatched CSRF value receives a generic `403 Forbidden` response. An expired, revoked, or otherwise invalid session is rejected by the session guard before mutation. The handler must not partially apply the request before rejection. Do not reflect the submitted token or nonce in the response or audit record.
