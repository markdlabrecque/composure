# Phase 2 work plan: parallel lanes

Status: active, progress updated 2026-10-02. Lane and wave layout written 2026-09-30 from `docs/phase2/tickets.md` and GitHub issues #58 to #176 in the Phase 2 milestone. Scope decision: Phase 2 is development-only; use the Debian-based Docker harness for automated deployment/security checks. No real sites exist and no existing-site upgrades are required. Human security signoff and real Hetzner evidence are deferred beyond Phase 2; the harness is not approval or host evidence, and production remains blocked pending those release requirements.

## Progress as of 2026-10-02

The latest batch is merged into `phase-2`, summarized on GitHub, closed as completed, and retired. The batch ends at merge commit `cd580be9ff36402df50b5f161357386e04443dfb`; the subsequent documentation snapshot is `949253da6ee8c9f96dde8e4de728c115d2713014`, which adds the progress and ticket-session pilot documentation. Each ticket has independent final approval and passing required hosted `Composure checks` for its publication head. The completion summaries identify the local-gate candidate and CI-tested revision separately; historical local runs are not attributed to later checkpoint-only heads.

| Completed issue | Delivered scope | Merged PR | Merge commit | Completion summary |
| --- | --- | --- | --- | --- |
| [#172](https://github.com/markdlabrecque/composure/issues/172) | Staticcheck cleanup and retirement of the obsolete findings lease | [#213](https://github.com/markdlabrecque/composure/pull/213) | `e3c765a` | [Evidence](https://github.com/markdlabrecque/composure/issues/172#issuecomment-5942706289) |
| [#69](https://github.com/markdlabrecque/composure/issues/69) | CLI/HTTP inventory distinguishing future interfaces from a revision-pinned source snapshot | [#214](https://github.com/markdlabrecque/composure/pull/214) | `63b16d2` | [Evidence](https://github.com/markdlabrecque/composure/issues/69#issuecomment-5943941036) |
| [#72](https://github.com/markdlabrecque/composure/issues/72) | Atomic first-administrator initialization for fresh development sites, including canonical-empty email refusal before planning, hashing or writes | [#215](https://github.com/markdlabrecque/composure/pull/215) | `fb1c6c3` | [Evidence](https://github.com/markdlabrecque/composure/issues/72#issuecomment-5944109241) |
| [#137](https://github.com/markdlabrecque/composure/issues/137) | Pure signature-prefix and extension classifier; DOCX classification remains provisional | [#216](https://github.com/markdlabrecque/composure/pull/216) | `cd580be` | [Evidence](https://github.com/markdlabrecque/composure/issues/137#issuecomment-5944339990) |

The summaries preserve review counts, tested revisions and exceptions. #69's local gate history includes a failed browser-prerequisite run followed by one explicitly authorized passing recovery on the unchanged candidate. #137 preserves two intentional Markdown hard breaks under a narrow local whitespace-check exception; its full gate and required CI passed without exception.

GitHub records these 29 original Phase 2 tickets closed: #58, #59, #60, #61, #62, #63, #64, #65, #66, #67, #68, #69, #70, #71, #72, #73, #74, #75, #77, #88, #101, #106, #114, #126, #136, #137, #170, #171 and #172. Related decision issues #189 and #197 are also closed. Do not redispatch completed work. The wave tables below retain the original dependency layout, not a live list of unfinished work.

### Next dependency-ready candidates

These open tickets have closed prerequisites in the lane plan. Before dispatch, verify prerequisite changes are present in the current phase branch and apply the normal scope, ownership and review checks. #81 is the next open ticket on the critical path. Its local worktree already exists at `/home/mark/Projects/worktrees/composure/81`; reconcile its recorded thread/session and evidence before any new dispatch. Worktree existence alone does not establish implementation progress.

| Lane | Issue | Next outcome |
| --- | --- | --- |
| Audit package | #79 | Minimal audit recorder interface |
| Auth package | #111 | CLI operator principal |
| Mail package | #85 | SMTP relay configuration from deployment secrets |
| Web sign-in | #76 | Sign-out route |
| Web guard and CSRF | #81 | Reject unauthenticated admin requests |
| Web invitations | #89 | Invitation form and token issue |
| Web reset | #95 | Request-reset form with generic response |
| CI | #173 | Reconcile acceptance against the strict enforcement already delivered by #172; issue remains open |

#138 has closed ticket prerequisites but remains held for a product decision: reconcile the accepted field-only size/dimension policy with the documented global/category defaults before #138/#139. #140 also needs approved ZIP/OOXML bounds before full DOCX validation. Neither decision was settled by #137.

[#191](https://github.com/markdlabrecque/composure/issues/191) remains open and must be coordinated with #79/#131 before the first audited consumers are treated as complete. Recorder documentation and tests must distinguish development log-line output from durable SQLite recording and define the state-commit boundary.

The completed batch's role handoffs have been removed from the checkout after a verified private archive of their latest contents. GitHub completion summaries above remain the delivery record; historical role evidence is retained outside the checkout under `~/.codex/runtime/ticket-sessions/composure-completed-handoffs/`.

Other reported follow-ups remain separate work: the duplicated but consistent account-email canonicalizer, broader email grammar, SQLite browser-fixture cleanup, and incomplete contrast/manual accessibility checks. This batch does not complete Phase 2 access controls, safe uploads, human security signoff, WCAG conformance or production readiness.

## How to read this

- A **lane** is a set of tickets that touch the same files. Tickets inside a lane run one after another. Different lanes can run at the same time.
- A **wave** is the earliest point a ticket can start, counting both its stated dependencies and its place in its lane. Everything in one wave can run in parallel, and each ticket may start as soon as its own prerequisites are merged, without waiting for the rest of its wave.
- The critical dependency chain has 27 ticket steps and reaches wave 28; it skips wave 17, which has no ticket on this chain. The 28 numbered waves are therefore the earliest finish wave, not a count of tickets on the chain.
- Each ticket still follows the dispatch, review, merge and closure rules in `docs/work_plan.md`. A ticket starts only when its prerequisites are merged and closed.

## Rules that keep lanes from colliding

1. **New code goes in new files.** Each web ticket adds its handlers in its own file and adds only its route registrations to `internal/web/web.go`. Expect trivial rebases on that one line block; nothing else should overlap.
2. **One SQL file per table.** P2-09 (#70) first moves the Phase 1 schema from `internal/store/schema.sql` to `internal/store/schema/001_page.sql` loaded with `//go:embed schema/*.sql` in name order. Every later store ticket adds its own numbered file and its own `<table>.go`, so store lanes never touch the same file.
3. **Contract sections are separate files.** P2-03 (#62) creates `docs/phase2/access-contract.md` as an index and P2-01a (#58) does the same for `docs/phase2/audit-events.md`. Each later contract ticket writes its section to its own file under the matching directory and adds one link to the index. This lets all contract sections run in parallel.
4. **One lane, one agent at a time.** Two agents never work the same lane concurrently.
5. **Rebase on `phase-2` before review.** A green branch does not prove integration with a parallel lane.
6. **Sol tickets get the review cap.** Sol-labelled tickets (#71, #84, #98, #116) are the security-critical core. Do not merge them without the full reviewer round.

## Lanes

| Lane | Files owned | Tickets in order |
| --- | --- | --- |
| Audit contract: envelope | `docs/phase2/audit-events.md` index plus its own file under `docs/phase2/audit-events/` | #58 |
| Audit contract: actions | `docs/phase2/audit-events.md` index plus its own file under `docs/phase2/audit-events/` | #59 |
| Audit contract: redaction | `docs/phase2/audit-events.md` index plus its own file under `docs/phase2/audit-events/` | #60 |
| Audit contract: throttling and retention | `docs/phase2/audit-events.md` index plus its own file under `docs/phase2/audit-events/` | #61 |
| Access contract: index and tables | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #62 |
| Access contract: tokens | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #63 |
| Access contract: passwords | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #64 |
| Access contract: cookies | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #65 |
| Access contract: throttling | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #66 |
| Access contract: roles | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #67 |
| Access contract: revocation | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #68 |
| Access contract: interfaces | `docs/phase2/access-contract.md` index plus its own file under `docs/phase2/access-contract/` | #69 |
| Upload contract doc | `docs/phase2/uploads.md` | #136 |
| Store: accounts | `internal/store/schema/*.sql` and `internal/store/accounts.go`, `sessions.go` | #70 → #73 → #114 |
| Store: throttle and tokens | `internal/store/schema/*.sql` and `internal/store/throttle.go`, `tokens.go` | #77 → #88 |
| Store: settings | `internal/store/schema/*.sql` and `internal/store/settings.go` | #126 |
| Store: audit | `internal/store/schema/*.sql` and `internal/store/audit.go` | #130 → #134 |
| Store: media | `internal/store/schema/*.sql` and `internal/store/media.go` | #146 |
| Auth package | `internal/auth/` (one file per ticket) | #71 → #106 → #101 → #111 |
| Audit package | `internal/audit/` | #79 → #131 → #132 → #133 |
| Mail package | `internal/mail/`, `internal/config/smtp.go` | #85 → #86 → #87 |
| Upload package | `internal/upload/` | #137 → #138 → #139 → #140 → #141 → #142 → #143 → #144 → #145 |
| Image package | `internal/image/` | #149 → #150 |
| CLI | `internal/cli/` | #72 → #104 → #105 → #112 → #113 → #121 |
| Web sign-in | `internal/web/session.go`, `internal/web/signin.go`, template `admin_signin.html` | #74 → #75 → #76 → #78 → #80 |
| Web guard and CSRF | `internal/web/guard.go`, `internal/web/csrf.go`, the existing Page handlers in `web.go` | #81 → #82 → #83 → #84 |
| Web invitations | `internal/web/invitations.go`, templates `admin_invite*.html`, `invite.html` | #89 → #90 → #91 → #92 → #93 → #94 → #110 |
| Web reset | `internal/web/reset.go`, templates `reset*.html` | #95 → #96 → #97 → #98 → #99 → #100 → #102 → #103 |
| Web permissions | `internal/web/permission.go`, route registrations in `web.go` | #107 → #108 → #109 |
| Web accounts | `internal/web/accounts.go`, templates `admin_accounts*.html` | #115 → #116 → #117 → #118 → #119 → #120 |
| Web shell | `internal/web/layout.html`, existing `admin_*.html`, error partials | #122 → #123 → #124 → #125 → #128 → #129 |
| Web settings and audit screens | `internal/web/settings.go`, `internal/web/audit.go`, their templates | #127 → #135 |
| Web media | `internal/web/media.go`, `admin_form.html` image field, `internal/render/page.html` | #147 → #148 → #151 → #152 → #153 → #154 → #155 |
| Deferred human signoff: auth | none (findings become new tickets) | #156; held beyond Phase 2 |
| Deferred human signoff: roles | none (findings become new tickets) | #157; held beyond Phase 2 |
| Deferred human signoff: uploads | none (findings become new tickets) | #158; held beyond Phase 2 |
| Deploy controls (Debian Docker harness) | `deploy/`, `docs/phase2/deploy.md`, `docs/phase2/evidence/` | #159 → #160 → #161 → #162 → #163 → #164 → #165 → #166 → #167 → #168 → #169; automated checks only in Phase 2, no real-host deployment or measurements |
| CI | `.github/workflows/ci.yml`, `go.mod` | #170 → #171 → #172 → #173 → #174 → #175 → #176 |

## Waves

| Wave | Lane | Issue | Ticket | Model | Waits for |
| --- | --- | --- | --- | --- | --- |
| 1 | Audit contract: envelope | #58 | P2-01a Audit contract: event envelope | Luna | Phase 1 closed |
| 1 | Access contract: index and tables | #62 | P2-03 Access contract: account and session tables | Luna | Phase 1 closed |
| 2 | Audit contract: actions | #59 | P2-01b Audit contract: covered actions and timing | Luna | #58 |
| 2 | Audit contract: redaction | #60 | P2-01c Audit contract: redaction allowlist | Luna | #58 |
| 2 | Access contract: tokens | #63 | P2-04 Access contract: invitation and reset tokens | Luna | #62 |
| 2 | Access contract: passwords | #64 | P2-05a Access contract: password hashing | Luna | #62 |
| 2 | Access contract: cookies | #65 | P2-05b Access contract: session cookie and CSRF rules | Luna | #62 |
| 2 | Access contract: throttling | #66 | P2-06 Access contract: throttle counters and backoff | Luna | #62 |
| 2 | Access contract: roles | #67 | P2-07a Access contract: role permission matrix | Luna | #62 |
| 2 | Access contract: revocation | #68 | P2-07b Access contract: session revocation triggers | Luna | #62 |
| 2 | Upload contract doc | #136 | P2-49 Upload contract document | Luna | #62 |
| 2 | Store: accounts | #70 | P2-09 Accounts table and store methods | Luna | #62 |
| 3 | Audit contract: throttling and retention | #61 | P2-02 Audit contract: throttling, counted failures, flush and retention | Luna | #60 |
| 3 | Access contract: interfaces | #69 | P2-08 Access contract: CLI command and HTTP route inventory | Luna | #63, #64, #65, #66, #67, #68 |
| 3 | Store: accounts | #73 | P2-12 Sessions table and store methods | Luna | #70 |
| 3 | Store: throttle and tokens | #77 | P2-16 Throttle counter store and backoff | Luna | #66, #70 |
| 3 | Store: settings | #126 | P2-43a Site settings table and store | Luna | #70 |
| 3 | Auth package | #71 | P2-10 Password hashing package | Sol | #64 |
| 3 | Upload package | #137 | P2-50 File type allowlist validator | Luna | #136 |
| 3 | CI | #170 | P2-66 govulncheck in CI | Luna | #136 |
| 4 | Store: accounts | #114 | P2-38 Last active administrator guard | Luna | #70, #73 (lane) |
| 4 | Store: throttle and tokens | #88 | P2-25 Token table and store methods | Luna | #63, #70, #77 (lane) |
| 4 | Auth package | #106 | P2-35 Permission matrix package | Luna | #67, #70, #71 (lane) |
| 4 | Audit package | #79 | P2-18 Minimal audit recorder interface | Luna | #61 |
| 4 | Upload package | #138 | P2-51 Size limit validator | Luna | #137 |
| 4 | CLI | #72 | P2-11 First administrator on init | Luna | #70, #71 |
| 4 | Web sign-in | #74 | P2-13 Session cookie middleware | Luna | #73 |
| 4 | CI | #171 | P2-67a staticcheck in CI | Luna | #170 |
| 5 | Store: audit | #130 | P2-45a Audit events table and store | Luna | #79 |
| 5 | Auth package | #101 | P2-32a Common-password list package | Luna | #71, #106 (lane) |
| 5 | Mail package | #85 | P2-23 SMTP relay configuration from deployment secrets | Luna | #72 |
| 5 | Upload package | #139 | P2-52 Image dimension validator before decoding | Luna | #137, #138 (lane) |
| 5 | Web sign-in | #75 | P2-14 Sign-in route and screen | Luna | #71, #74 |
| 5 | Web invitations | #89 | P2-26a Invitation form and token issue | Luna | #88 |
| 5 | Web reset | #95 | P2-29a Request-reset form with generic response | Luna | #88 |
| 5 | CI | #172 | P2-67b Fix the four current staticcheck findings | Luna | #171 |
| 6 | Store: audit | #134 | P2-48a Paged audit query | Luna | #130 |
| 6 | Auth package | #111 | P2-37a CLI operator principal | Luna | #106, #101 (lane) |
| 6 | Audit package | #131 | P2-45b SQLite audit recorder | Luna | #130, #79 (lane) |
| 6 | Mail package | #86 | P2-24a In-process SMTP capture server for tests | Luna | #85 |
| 6 | Upload package | #140 | P2-53 DOCX ZIP structure validator | Luna | #137, #139 (lane) |
| 6 | Image package | #149 | P2-57a Image re-encode with metadata removed | Luna | #139 |
| 6 | Web sign-in | #76 | P2-15 Sign-out route | Luna | #75 |
| 6 | Web guard and CSRF | #81 | P2-20a Reject unauthenticated admin requests | Luna | #75 |
| 6 | CI | #173 | P2-67c Make staticcheck fail the check | Luna | #172 |
| 7 | Audit package | #132 | P2-46 Counted repeated failures with bounded memory | Luna | #131 |
| 7 | Mail package | #87 | P2-24b Mailer send function | Luna | #86 |
| 7 | Upload package | #141 | P2-54 Server-generated storage names | Luna | #137, #140 (lane) |
| 7 | Image package | #150 | P2-57b Single image worker per site | Luna | #149 |
| 7 | Web sign-in | #78 | P2-17 Throttle sign-in before hashing | Luna | #75, #77, #76 (lane) |
| 7 | Web guard and CSRF | #82 | P2-20b Replace the local-prototype actor | Luna | #81 |
| 8 | Store: media | #146 | P2-56a Media table and store | Luna | #141 |
| 8 | Audit package | #133 | P2-47 Audit retention deletion | Luna | #131, #132 (lane) |
| 8 | Upload package | #142 | P2-55a Fuzz target for the type validator | Luna | #137, #141 (lane) |
| 8 | Web sign-in | #80 | P2-19 Record sign-in success and failure | Luna | #78, #79 |
| 8 | Web guard and CSRF | #83 | P2-21 Require a session on draft preview | Luna | #82 |
| 8 | Web invitations | #90 | P2-26b Send the invitation email | Luna | #87, #89 |
| 8 | Web reset | #96 | P2-29b Send the reset email | Luna | #87, #95 |
| 9 | Upload package | #143 | P2-55b Fuzz target for the size validator | Luna | #138, #142 (lane) |
| 9 | CLI | #104 | P2-33 CLI administrator password reset | Luna | #80, #72 (lane) |
| 9 | Web guard and CSRF | #84 | P2-22 CSRF tokens on state-changing forms | Sol | #82, #83 (lane) |
| 9 | Web invitations | #91 | P2-27a Accept-invitation creates the account | Luna | #89, #90 (lane) |
| 9 | Web reset | #97 | P2-29c Throttle reset requests | Luna | #95, #96 (lane) |
| 9 | Web media | #147 | P2-56b Media file write | Luna | #146 |
| 10 | Upload package | #144 | P2-55c Fuzz target for the image dimension validator | Luna | #139, #143 (lane) |
| 10 | CLI | #105 | P2-34 CLI create administrator | Luna | #104 |
| 10 | Web invitations | #92 | P2-27b Sign in after accepting an invitation | Luna | #91 |
| 10 | Web reset | #98 | P2-30 Complete-reset route | Sol | #97 |
| 10 | Web permissions | #107 | P2-36a Route permission middleware | Luna | #84, #106 |
| 10 | Web media | #148 | P2-56c Public media route | Luna | #147 |
| 11 | Upload package | #145 | P2-55d Fuzz target for the DOCX validator | Luna | #140, #144 (lane) |
| 11 | CLI | #112 | P2-37b Route account CLI commands through the permission check | Luna | #105, #111 |
| 11 | Web invitations | #93 | P2-27c Reject reused and expired invitation tokens | Luna | #91, #92 (lane) |
| 11 | Web reset | #99 | P2-31a Revoke other sessions on password change | Luna | #98 |
| 11 | Web permissions | #108 | P2-36b Apply permissions to the Page read routes | Luna | #107 |
| 12 | CLI | #113 | P2-37c Route init through the permission check | Luna | #112 |
| 12 | Web invitations | #94 | P2-28 Throttle invitation requests | Luna | #89, #93 (lane) |
| 12 | Web reset | #100 | P2-31b Audit event on password change | Luna | #80, #98, #99 (lane) |
| 12 | Web permissions | #109 | P2-36c Apply permissions to the Page write routes | Luna | #108 |
| 12 | Web accounts | #115 | P2-39a Role assignment route | Luna | #108, #114 |
| 12 | Web shell | #122 | P2-42a Admin shell layout template | Luna | #108 |
| 12 | Web media | #151 | P2-58a Image upload endpoint with type check | Luna | #108, #148 |
| 12 | CI | #174 | P2-68 Fuzz gates in CI | Luna | #145, #173 |
| 13 | CLI | #121 | P2-41 Last administrator guard on CLI | Luna | #112, #114, #113 (lane) |
| 13 | Web invitations | #110 | P2-36d Apply permissions to the invitation routes | Luna | #109, #94 (lane) |
| 13 | Web reset | #102 | P2-32b Password warning on the invitation form | Luna | #91, #101, #100 (lane) |
| 13 | Web accounts | #116 | P2-39b Revoke sessions on role change | Sol | #115 |
| 13 | Web shell | #123 | P2-42b Move the Page list into the shell | Luna | #122 |
| 13 | Web settings and audit screens | #127 | P2-43b Site name settings screen | Luna | #122, #126 |
| 13 | Web media | #152 | P2-58b Size and dimension checks on the upload endpoint | Luna | #151 |
| 13 | CI | #175 | P2-69a Pin Go module versions | Luna | #173, #174 (lane) |
| 14 | Web reset | #103 | P2-32c Password warning on the reset form | Luna | #98, #102 |
| 14 | Web accounts | #117 | P2-39c Audit event on role change | Luna | #115, #116 (lane) |
| 14 | Web shell | #124 | P2-42c Move the Page edit screen into the shell | Luna | #123 |
| 14 | Web media | #153 | P2-58c Re-encode uploads through the image worker | Luna | #150, #152 |
| 14 | CI | #176 | P2-69b Pin CI action revisions | Luna | #173, #175 (lane) |
| 15 | Web accounts | #118 | P2-40a Account deactivation route | Luna | #117 |
| 15 | Web shell | #125 | P2-42d Move the draft preview into the shell | Luna | #123, #124 (lane) |
| 15 | Web media | #154 | P2-58d Image field on the Page draft | Luna | #153 |
| 16 | Web accounts | #119 | P2-40b Revoke sessions on deactivation | Luna | #118 |
| 16 | Web shell | #128 | P2-44a Shared field error partial | Luna | #125 |
| 16 | Web media | #155 | P2-59 Render the image on the published Page | Luna | #153, #154 (lane) |
| 17 | Web accounts | #120 | P2-40c Audit event on deactivation | Luna | #118, #119 (lane) |
| 17 | Web shell | #129 | P2-44b Shared page-level error partial | Luna | #122, #128 (lane) |
| 18 | Web settings and audit screens | #135 | P2-48b Audit log screen | Luna | #120, #128, #134, #127 (lane) |
| 18 | Deploy controls | #159 | P2-61 systemd unit template | Luna | #155 and independent automated auth/roles/uploads checks |
| 19 | Deploy controls | #160 | P2-62 Caddy and TLS template | Luna | #159 |
| 20 | Deploy | #161 | P2-63 IP allowlist in the Caddy template | Luna | #160 |
| 21 | Deploy | #162 | P2-64a Deployment runbook | Luna | #161 |
| 22 | Deploy | #163 | P2-64b Record host size and page mix | Luna | #162 |
| 23 | Deploy | #164 | P2-64c Publish drill on the host | Luna | #163 |
| 24 | Deploy | #165 | P2-64d Restart drill | Luna | #164 |
| 25 | Deploy | #166 | P2-64g Filesystem permissions check | Luna | #165 |
| 26 | Deploy | #167 | P2-64e Readiness failure drill | Luna | #165, #166 (lane) |
| 27 | Deploy | #168 | P2-64f Structured error log check | Luna | #167 |
| 28 | Deploy | #169 | P2-65 Smoke load and recorded evidence | Luna | #168 |

## Critical path

The critical dependency chain has 27 ticket steps across wave numbers 1–16 and 18–28. Keep these moving first; everything else has slack. #159 also requires the independent automated auth, roles and uploads checks listed in its wave dependency; those checks are gates, not additional tickets on this chain.

#62 (P2-03) → #70 (P2-09) → #73 (P2-12) → #74 (P2-13) → #75 (P2-14) → #81 (P2-20a) → #82 (P2-20b) → #83 (P2-21) → #84 (P2-22) → #107 (P2-36a) → #108 (P2-36b) → #151 (P2-58a) → #152 (P2-58b) → #153 (P2-58c) → #154 (P2-58d) → #155 (P2-59) → #159 (P2-61; after independent automated auth/roles/uploads checks) → #160 (P2-62) → #161 (P2-63) → #162 (P2-64a) → #163 (P2-64b) → #164 (P2-64c) → #165 (P2-64d) → #166 (P2-64g) → #167 (P2-64e) → #168 (P2-64f) → #169 (P2-65)

## Parallelism by wave

| Wave | Tickets ready | Lanes active |
| --- | --- | --- |
| 1 | 2 | 2 |
| 2 | 10 | 10 |
| 3 | 8 | 8 |
| 4 | 8 | 8 |
| 5 | 8 | 8 |
| 6 | 9 | 9 |
| 7 | 6 | 6 |
| 8 | 7 | 7 |
| 9 | 6 | 6 |
| 10 | 6 | 6 |
| 11 | 5 | 5 |
| 12 | 8 | 8 |
| 13 | 8 | 8 |
| 14 | 5 | 5 |
| 15 | 3 | 3 |
| 16 | 3 | 3 |
| 17 | 2 | 2 |
| 18 | 2 | 2 |
| 19 | 1 | 1 |
| 20 | 1 | 1 |
| 21 | 1 | 1 |
| 22 | 1 | 1 |
| 23 | 1 | 1 |
| 24 | 1 | 1 |
| 25 | 1 | 1 |
| 26 | 1 | 1 |
| 27 | 1 | 1 |
| 28 | 1 | 1 |

## Suggested agent count

Peak concurrency is 10 tickets in wave 2. Running four to six agents keeps most waves busy without idling; more than that mostly waits on the critical path.

## Open questions carried from the ticket draft

The Argon2id parameter decision is resolved by completed #64 and the [password contract](access-contract/03-passwords.md).

Remaining decisions:

1. Field-only upload limits versus global/category defaults before #138/#139, and ZIP/OOXML bounds before #140.
2. Logo and colour branding: Phase 2 after #155, or Phase 4.

The former Hetzner-host protection choice is not a Phase 2 question: there is no Phase 2 production/test-host deployment. Host protection and real-host evidence remain deferred release work. Issues #156–#158 are held for human signoff beyond Phase 2; automated auth/roles/uploads checks remain Phase 2 acceptance. The reporter should update the affected review/deploy issue bodies to distinguish automated Debian-harness checks from deferred human signoff and Hetzner evidence. Use the current scope and acceptance rules in this plan and `docs/work_plan.md`; retired role handoffs are historical evidence.
