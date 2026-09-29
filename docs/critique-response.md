# Critique review and plan changes

Reviewed `CLAUDE_CRITIQUE.md` against the current PRD, architecture plan, work plan and proposed phase 1 Page contract. The critique cites an older revision. This review changes planning documents, not application behaviour. A subsequent ticket pass updated the affected remote issues. The Markdown plans are authoritative; the old `.lavish/work-plan.html` is not updated and must not be used as the current plan.

## Scope and architectural findings

- C1: Accepted the estimate problem. Removed the obsolete effort table, deferred partial portable exports/imports and added an ordered cut list with an explicit scope-change gate. Did not silently downgrade every suggested feature: menu history and the existing field set remain P0 until Mark approves further cuts. There is no replacement calendar estimate without implementation evidence.
- C2: Accepted production config locking. Edit in development/staging, review in Git, deploy with an expected-active-version check. Production UI and direct mutations fail.
- C3: Partly already covered by the Page contract's JSON storage. Added an ADR to reconcile architecture wording and specify safe model changes. Destructive transformations and cardinality/type conversions are rejected in v1 rather than promising a general migration engine.
- C4: Accepted, with stronger safeguards than age alone. Immutable hashed files, rollback pins, a recovery window, and a shared storage/export/GC lock protect restores. Back up SQLite consistently before copying pinned files; a database-first ordering without a GC pin is insufficient.
- C5: Accepted. V1 ships full recovery only. Portable database/file import-export waits for a migration use case.

## Security, hosting and performance

- H1: Accepted. Phase 2 now deploys the protected Page to Hetzner and records a smoke load. Phase 6 hardens deployment; phase 7 retains full release measurements.
- H2: Accepted. Preserve pure-Go releases; decode WebP and encode PNG rather than introduce a WebP encoder/cgo. Required variants finish before publication with bounded concurrency.
- H3: Accepted. Added pixel/dimension limits, metadata stripping, DOCX structure and expansion validation, safe download headers, generated names and fuzz checks. Phase 4 must fix numeric archive budgets before implementation.
- H4: Accepted throttling, generic reset responses and session revocation. Existing explicit weak-password confirmation already applies to administrators; retain that product decision rather than add TOTP now. Use a local common-password list without sending passwords to an external service.
- H5: Accepted. Throttle before audit writes, aggregate failures with bounded counters, and expire audit entries after a configurable 90-day default.
- H6: Accepted. Bounded in-process cache with whole-site generation invalidation; no CDN/proxy cache in acceptance runs. Add mixed publication/upload traffic and shared-host contention. Do not arbitrarily raise throughput targets without measurements.
- H7: Accepted. Dependency pinning, govulncheck, staticcheck and bounded fuzzing join human review. An independent reviewer must inspect auth/uploads before client launch; Mark remains release acceptor.

## Editorial and operational gaps

- M1: Accepted the sequencing correction. Phase 1 fixes Page and safety boundaries only. Future sections in the Page contract remain constraints, not a demand to implement later schemas now. Narrow #3 before dispatch.
- M2: Already specified by the Page contract's revision comparison and 409 response. Promoted it into the PRD and phase 3 acceptance, including restoration.
- M3: Related-item preview was already specified in the Page contract. Clarified menus and made the rule visible in the PRD and acceptance checks.
- M4: Hierarchical explicit paths and reservation errors were already specified in the Page contract. Adopted them for v1; rejected adding configurable URL patterns and mass redirects, which would expand scope unnecessarily.
- M5: Already covered in the contract's future menu rules. Added explicit PRD and phase 5 checks for hiding, flagging and restoring targets.
- M6: Accepted for phase 2. The first real account gets both roles; phase 1 deliberately has only a local prototype actor.
- M7: Accepted the dependency clarification, no inline images, and ID-based internal links. Require a pinned prebuilt editor and sanitizer choice/security review before phase 3. Library selection remains a bounded implementation gate, not an unsupported claim that a library has been evaluated.
- M8: Accepted. Prefer compatible binary rollback; database rollback reports lost writes, requires acknowledgement and captures the current state first.
- M9: Accepted. Per-site systemd resource limits and one initial image worker; choose actual quotas from the early host run.
- M10: Accepted a keyboard/labels/focus/axe baseline now. Full WCAG audit stays on the roadmap; automated checks alone do not establish conformance.

## Cleanup findings

- L1: Replaced the throwaway spike wording and made the architecture checklist defer to the work plan's complete sequence.
- L2: Already addressed by the current work plan's #14 prerequisite and serial ticket order. Preserved it; no duplicate phase-zero ticket.
- L3: Moved minimal agent conventions/test commands before application implementation. This review does not create speculative commands in a new AGENTS.md.
- L4: Added readiness failure checks, structured request IDs and item IDs on render errors.
- L5: Added UTC instants plus IANA zones, UTC site default, and explicit DST gap/ambiguity handling.
- L6: Added explicit CLI access-recovery audit coverage in phase 2 and the PRD.

## Ticket handover

Updated remote issues #1, #3, #4, #11, #13 and #19–#23. #3 and #23 now cover Page configuration only; #21 moves to phase 6 full recovery and #22 to phase 2 bounded audit design. The phase 1 tracker and child relationships reflect that split. #4 requires early agent guidance and a pure-Go release build; #13 carries the accessibility baseline, revised phase 2 hosting/security handoff and re-estimation gate. #19/#20 distinguish new-site definitions from production deployment rules; #11 no longer implies content-reference validation in phase 1.

#2 remains completed; its updated local contract and the ADR must land through documentation review rather than reopening historical acceptance. Existing revision-conflict, path and preview tickets remain unchanged. No issue was closed by this planning pass. Later phase implementation tickets do not yet exist; when created, copy the detailed work-plan checks, especially phase 4 GC/upload budgets and phase 6 export/rollback races.
