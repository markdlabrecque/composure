# Ticket #72 test-writer checkpoint

## Stop state

Mark requested this committed documentation-only checkpoint on 2026-10-01. Live agent `t72-test-writer` owns this document only for this operation. No stage work resumed, and no implementation, test, gate, rebase, push, issue, or cleanup action was performed.

Verified checkout `/home/mark/Projects/worktrees/composure/72`, branch `72`. Before this checkpoint, HEAD was `100bcb496e05c250679fab53fcf00f70af15de3e`; its integration base and merge-base with local `phase-2` were `f5a010dce03d2f4a8fb797029b59c2d1bacfcb1d`. These are historical pre-checkpoint revisions, not the new documentation commit. Index and tracked working tree were clean. The only untracked entry was the coordinator-owned `.venv-browser`, which was preserved along with the ignored axe asset.

Ticket #72 is not complete or approved. Review round 1 of cap 2 bounced. The authoritative `bash scripts/test-phase1` gate has not run for this ticket. No provisional or final approval is claimed.

## Work I personally completed

I wrote only `tests/integration/admin_init_test.go` during the original stage 1 and committed it as `067e129fb8fc4b79ec221f068a1d8c5f128769cc`, on original base `a4642649a74fce94b0d7c6dd2aa30cf582fc5d58`.

With Go 1.27.1 selected through `/home/mark/.local/share/composure-toolchains/go1.27.1/go/bin`, I observed:

- `CGO_ENABLED=0 go test ./...` passed before the new tests.
- `CGO_ENABLED=0 go test ./tests/integration -run '^$' -count=1` passed, confirming compilation and the real CLI build.
- `CGO_ENABLED=0 go test ./tests/integration -run '^TestAdminInit' -count=1 -v` failed before and after the test commit with `composure init: flag provided but not defined: -admin-email`.

This was a runtime missing-required-CLI-API RED, not a compile failure or demonstrated downstream account behavior. The tests define canonical email, one active dual-role account, accepted fixed-profile hashing of exact password bytes, verification, fresh salts, persistence, no-change planning, and non-destructive refusal. SQL and the SQLite driver were not added to the new integration test.

Original evidence is `/tmp/composure-72-test-writer-evidence/baseline.log`, `compile.log`, `red.log`, `committed-red.log`, and `revision.txt`.

Mark subsequently clarified that missing and empty passwords require input validation before creation, and confirmed CLI support for `--admin-email`. I did not dispatch another role from this session. Main owns dispatch.

## Later work observed at this checkpoint

The following work belongs to later physical attempts or other roles, not my original stage result. I inspected their existing handoffs, revision history, and immutable test hashes. I did not rerun their checks now.

- A fresh logical test-writer added `tests/integration/admin_init_password_validation_test.go` at original revision `a92986a5d1e4a586d2e2435e58b15670b7b53e97`. Its handoff reports 12 real-process cases for unset versus empty password environment input, planning versus apply, and absent, empty, or initialized destinations. Its required-flag guard prevents unknown-flag refusal from falsely satisfying password validation. Original tests were preserved. See `/tmp/composure-72-stage1-a2/handoff.txt` and its `committed-additive-red.log`, `committed-original-red.log`, `committed-compile.log`, `committed-phase1.log`, and `committed-check-summary.txt`.
- The implementor reported focused and cheap GREEN at original candidate `f0332d47d26c286998b81778b9a704b646ece95f` on base `87914188325291cb6fe38ff5961303bef9b23cef`. See `/tmp/composure-72-stage2/handoff.txt` and its `committed-focused-admin-init.log`, `committed-focused-phase1-init-config-site-account.log`, `committed-build.log`, `committed-vet.log`, `committed-all-go-tests.log`, `committed-python-cheap-suite.log`, and `committed-shared-suite.log`. The shared runner first failed because cgo was disabled for race detection; its documented cgo-enabled invocation passed. This is not an authoritative full-gate pass.
- Main rebased the ticket onto `f5a010dce03d2f4a8fb797029b59c2d1bacfcb1d`. The original writer commit is now `ecd33619a217a26ca020b4094c9236ce4fbf1392`, additive writer commit is `ab54af99064c30208006cc81e9373323bf664e1f`, and implementation candidate is `100bcb496e05c250679fab53fcf00f70af15de3e`. `/tmp/composure-phase2-wave/72-fresh-post-rebase-cheap.log` reports unchanged ticket-owned blobs, passing Go tests, and 66 passing Python checks.
- Independent reviewer round 1 bounced that candidate. `/tmp/composure-phase2-wave/72-review-r1-final-for-handoff.txt` reports that explicitly supplied empty or ASCII-space-only `--admin-email` is accepted, and apply persists an active dual-role account with canonical-empty email. Required correction is rejection before planning, hashing, or writes, while preserving legacy no-flag behavior. Other malformed address syntax is a follow-up requiring an accepted boundary, not permission to invent provider rules.

Reviewer evidence paths are `/tmp/composure-phase2-wave/72-reviewer-r1-cheap.log`, `72-reviewer-r1-process.json`, `72-reviewer-r1-adversarial.log`, and `72-reviewer-r1-snapshot.log`. The reviewer reported passing focused/build/vet/Go/Python checks and disposable adversarial checks but no approval and no authoritative gate.

## Immutable test identities

I verified these against current pre-checkpoint HEAD without editing either file:

| Test file | Original Git blob | SHA-256 of current unchanged file |
| --- | --- | --- |
| `tests/integration/admin_init_test.go` | `96e757e611a5f003563fd43175c9c577eb21c2a1` | `70516372ecd91cc9119d3d40a42e6013c67ea8aa875ff8f64d891cd931225f7c` |
| `tests/integration/admin_init_password_validation_test.go` | `ace86d2fb427b39cc283e5cfff0163878fe5df54` | `d40fa60de74d84a07517f1aa9bb4f870fe8d613b11599166303fc3f382efd87a` |

Both original and rebased writer revisions resolve to these same blobs. This checkpoint must not change them.

## Reconciled pipeline state and next role

`/tmp/composure-phase2-wave/checkpoint.json` still labels #72 as an active reviewer. That flag is stale. The retained round-1 result and main checkpoint instructions establish BOUNCE, no gate, and next action through main to a test-writer for additive canonical-empty-email regressions, then implementor correction. Preserve both existing writer tests and the review count. The next independent review is round 2 of cap 2, not a fresh round 1.

The missing/empty-password omission in my original handoff is resolved by the later additive tests and reported GREEN implementation. It is not the current blocker. Canonical-empty email acceptance is the unresolved must-fix. Broader email syntax remains an unresolved product boundary.

Main retains the integration queue `172 -> 69 -> 72 -> 137`. The cross-role resume handoff reports #172 final approved with its authoritative gate passed exactly once, #69 awaiting review round 2 after correction, and #137 with provisional cheap approval only and its gate withheld. Those are other tickets' results, not approval of #72. See `/tmp/composure-pipeline-resume-handoff.md`; this checkpoint does not implement its separate pipeline-resume tooling request.

Future continuation must reconcile documentation-only checkpoint commits and any changed integration base explicitly. Approval and test evidence remain bound to their recorded original revisions. Do not infer a new source correction from this document commit, reset review counts, or blindly repeat a gate.

## Models, authority, and scope

Writer profile uses `openai-codex/gpt-6.1-sol`; initial reasoning effort was not recorded in my original handoff. The later additive writer handoff explicitly selected Sol/high. Implementor is `openai-codex/gpt-6-luna`, reasoning `high`, per the sole Luna label and CLI-secret/initialization-preservation risk. The fresh reviewer handoff selected Sol/high. Main uses fresh Herdr-native named Pi sessions under Mark's explicit transport override; child agents do not orchestrate or dispatch stages.

Supported work is fresh disposable development-site initialization. No real-site upgrade, reset/recovery command, web account creation, startup DDL, password strength policy, whitespace-password rejection, or mandatory administrator flag for legacy init was authorized. Accepted account/email and exact-byte password contracts remain authoritative. Preserve existing initialization site selection and all Phase 1 tests.

## Durable evidence and limitations

The private local archive is `/home/mark/.pi/agent/runtime/composure-branch-handoffs/2026-10-01/legacy-evidence.tar.gz`. I verified SHA-256 `9116e56f3f9bb57f0f56b909ff41b9ed49766596f7014e3c4074949652f91d9f`.

Its verified listing includes the original writer evidence directory and the wave handoffs, reviewer final report, and coordination artifacts. It does not list the separate `composure-72-stage1-a2` or `composure-72-stage2` directories. Their detailed paths above are current `/tmp` evidence; the retained wave summaries, committed tests, and this checkpoint preserve the stopping point without claiming every later raw log is archived. No raw logs or credentials were copied into this branch document.

No new checks or acceptance claims accompany this documentation-only commit. No issue completion, PR, merge, or cleanup is claimed. Mark/main will handle retirement; this agent stops after committing this exact file.

## Suggested skills

- `subagent-tdd-pipeline` for resumed named-role sequencing and the persisted two-round cap.
- `handoff` and `unslop` for concise, secret-free continuation records. Mark overrides the handoff skill's temporary location for this committed file.
- `retire-worktree` only for Mark/main's authorized cleanup, not this role.
