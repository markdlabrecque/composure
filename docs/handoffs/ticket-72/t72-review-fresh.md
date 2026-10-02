# Ticket #72 reviewer checkpoint

## Stop state

Mark requested this branch-committed documentation checkpoint on 2026-10-01. Live agent `t72-review-fresh` is the independent reviewer, using `openai-codex/gpt-6.1-sol`, HIGH reasoning. Main orchestrates fresh Herdr-native named Pi stages under Mark's explicit override. I dispatched no child agents.

My completed logical review was **BOUNCE, round 1 of cap 2**. No provisional or final approval was granted. The authoritative `bash scripts/test-phase1` gate was expressly withheld for this physical pass and was never run by me. No stage work resumed for this checkpoint.

Historical tested candidate before any checkpoint commits was `100bcb496e05c250679fab53fcf00f70af15de3e`, against base `f5a010dce03d2f4a8fb797029b59c2d1bacfcb1d`. I verified candidate, local/origin base, and live origin base at review entry and exit. Those revisions bind the results below, not this documentation commit.

At checkpoint entry I verified cwd `/home/mark/Projects/worktrees/composure/72`, branch `72`, and HEAD `14d72ecbaa2d74939f63aadfc54d774860d3270c`. The three intervening commits only added sibling checkpoints. All ticket-owned source/test blobs still match my historical candidate. Tracked working tree and index were clean; the sole untracked entry was `.venv-browser`, pointing to `/tmp/composure-browser-env`. That symlink and the ignored pinned axe asset remain untouched.

## My review result

I inspected the real base-to-candidate diff, accepted initialization/account/password contracts, original writer RED artifacts, implementor handoff, and post-rebase cheap evidence. I independently verified that the three production files and both writer tests match prior GREEN candidate `f0332d47d26c286998b81778b9a704b646ece95f` byte-for-byte.

The unresolved must-fix is `internal/site/site.go:67`. Explicitly supplied empty or ASCII-space-only `--admin-email` is accepted. Real CLI planning exits 0; apply exits 0 and persists one active dual-role account with canonical-empty email. The account contract requires an address. Reject canonical-empty email before plan output, hashing, or writes, preserving accepted ASCII canonicalization and legacy no-flag initialization.

The process fixture also accepted `not-an-address`. Broader address syntax is a follow-up requiring an accepted validation boundary, not permission to invent provider alias rules or a new grammar. This is distinct from the canonical-empty must-fix.

## Checks I ran on the historical candidate

Go 1.27.1 was selected through `/home/mark/.local/share/composure-toolchains/go1.27.1/go/bin`, with `CGO_ENABLED=0` for Go checks.

- Focused `go test ./tests/integration ./internal/site ./internal/store -run ... -count=1 -v` passed. The exact expression in the cheap log selects `TestAdminInit`, existing Phase 1 init/config tests, `TestConfigInitTicket12`, and `TestAccounts`.
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`, and `git diff --check f5a010dc HEAD` passed.
- `python3 -B -m unittest discover -s tests -p 'test_*.py' -v` passed all 66 tests.
- A real CLI built with `go build -o /tmp/composure-phase2-wave/72-reviewer-r1-cli ./cmd/composure` reproduced the empty-email defect. Invalid configuration refused without creating a directory; legacy no-admin initialization created no account; valid email canonicalization passed. Harmless fixture credentials were substituted rather than inherited, and credential output checks passed.
- `go test -overlay /tmp/composure-phase2-wave/72-reviewer-r1-overlay/overlay.json ./internal/site ./internal/store -run '^TestReview72' -count=1 -v` passed. Disposable tests covered schema/config/site-identity rollback after account-insert failure, cancellation cleanup, concurrent-file preservation, final-name collision, opaque password bytes, planning without hashing, and entropy failure before writes.

Existing suites were cheap GREEN, but the adversarial empty-email observations were RED against the required-address contract. GREEN did not resolve that defect or grant approval. I changed no source or tracked tests; all extra fixtures and overlays were outside the checkout.

## Evidence and immutable tests

My exact commands/results and observations are retained at:

- `/tmp/composure-phase2-wave/72-reviewer-r1-cheap.log`
- `/tmp/composure-phase2-wave/72-reviewer-r1-process.json`
- `/tmp/composure-phase2-wave/72-reviewer-r1-adversarial.log`
- `/tmp/composure-phase2-wave/72-reviewer-r1-snapshot.log`
- `/tmp/composure-phase2-wave/72-reviewer-r1-overlay/overlay.json` and its disposable test files

Main retained the final verdict at `/tmp/composure-phase2-wave/72-review-r1-final-for-handoff.txt`. Original-stage sources are `/tmp/composure-72-test-writer-evidence/`, `/tmp/composure-72-stage1-a2/handoff.txt`, `/tmp/composure-72-stage2/handoff.txt`, `/tmp/composure-phase2-wave/72-implementor-handoff.txt`, and `/tmp/composure-phase2-wave/72-fresh-post-rebase-cheap.log`.

I verified these immutable Git blobs during review and again at checkpoint entry:

- `tests/integration/admin_init_test.go`: `96e757e611a5f003563fd43175c9c577eb21c2a1`, matching original writer revision `067e129fb8fc4b79ec221f068a1d8c5f128769cc`.
- `tests/integration/admin_init_password_validation_test.go`: `ace86d2fb427b39cc283e5cfff0163878fe5df54`, matching additive writer revision `a92986a5d1e4a586d2e2435e58b15670b7b53e97`.

Initial RED was the unknown `--admin-email` flag, which masked downstream password validation. It was not evidence of implemented password refusal. See the sibling writer checkpoints for their original observations, and `docs/handoffs/ticket-72/t72-implementor.md` for Luna/HIGH implementation evidence. Those are other roles' results, not additional reviewer checks.

The durable private archive is `/home/mark/.pi/agent/runtime/composure-branch-handoffs/2026-10-01/legacy-evidence.tar.gz`. I verified SHA-256 `9116e56f3f9bb57f0f56b909ff41b9ed49766596f7014e3c4074949652f91d9f`. It retains the shared wave evidence and original writer artifacts; do not assume every separate later stage directory is archived. No raw credential-bearing logs are committed here. GitHub remains the delivery system of record; this is Mark's authorized checkpoint, not a completion-report fallback.

## Reconciled next step and limits

The #72 entry in `/tmp/composure-phase2-wave/checkpoint.json` still says reviewer active. That is stale. My completed verdict and main's latest checkpoint instructions establish round 1/2 BOUNCE, no gate, and the next logical role: fresh additive test-writer for canonical-empty-email regressions, then the same logical implementor for correction. Preserve both existing tests. The next independent review is round 2/2; replacement physical agents do not reset the cap.

Main's cross-role context is `/tmp/composure-pipeline-resume-handoff.md`. It records #172 final approval with exactly one passed authoritative gate, #69 corrected and awaiting review 2, and #137 provisional cheap approval with its gate withheld. These are main-provided sibling results, not my independent reviews. The integration queue remains `172 -> 69 -> 72 -> 137`; do not repeat #172's gate or infer approval for #72. Separate pipeline-resume tooling is outside this checkpoint.

Mark clarified missing/empty password refusal before any writes. Optional admin setup, canonical email, exact opaque password bytes, fixed-profile fresh-salt hashing, atomic fresh-site account/schema/config/example initialization, no-change planning, and data-preserving refusal remain the bounded scope. No mandatory admin flag for legacy init, password strength/weak-confirmation policy, upgrades, startup DDL, web account creation, or recovery behavior was authorized. SQL/driver remain owned by `internal/store`.

Future continuation must reconcile all documentation-only checkpoint commits and any changed base explicitly. Historical results stay revision-bound; no approval exists to carry forward for #72. Source changes require applicable cheap review again. Only after no must-fix remains, provisional approval, and candidate/base freeze may the reviewer run one authoritative gate under main's serialization. A failed gate is a bounce, not permission to retry.

This checkpoint is authorized only to write and commit this file. No push, PR, issue mutation, merge, closure, rebase, install, gate, or cleanup was performed. No production security, human security signoff, axe contrast completeness, or Hetzner acceptance is claimed. #72 remains unfinished. Main owns any retirement; I stop after the exact-file commit.

## Suggested skills

- `subagent-tdd-pipeline` for named-role continuation and persistent review counts.
- `handoff` and `unslop` for continuation records. Mark overrides the temporary handoff location for this branch checkpoint.
- `retire-worktree` only for main's authorized cleanup, not this agent.
