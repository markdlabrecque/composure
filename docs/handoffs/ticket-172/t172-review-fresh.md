# Ticket #172 reviewer checkpoint

## Stopping point

Author: `t172-review-fresh`, independent named Pi reviewer, dispatched fresh `openai-codex/gpt-6.1-sol` with high reasoning through Herdr. Code route. Logical review round 1 of cap 2, with no earlier review round for this ticket. Main remains the orchestrator; this reviewer spawned no child agents.

I personally returned **final APPROVED** after cheap review, provisional approval, candidate/base freeze, and exactly one successful authoritative gate. No must-fix findings or optional follow-ups remained. Next logical stage is reporter, not another review or gate on the historical candidate.

This is external evidence authored under the read-only reviewer profile. I did not write tracked files, commit, push, open a PR, merge, close an issue, rebase, install tools, or retire a worktree. A write-enabled sibling may copy this document to `docs/handoffs/ticket-172/t172-review-fresh.md` and commit only that file under Mark's checkpoint authorization.

## Historical reviewed and tested revision

- Candidate: `dd44df3b23bfb5acbe3c1697d7a0a409a3ad610c`.
- Frozen phase-2 base: `f5a010dce03d2f4a8fb797029b59c2d1bacfcb1d`.
- Candidate tree: `796b4c9acbffb2f2570c3f994023f891e6aaf41c`.
- Prior implementor GREEN candidate: `f17194a90a5bce66657683653f954862c6efd405`.
- Writer revision: `4a1f579dbec112c0da3f632dbe90aba8127c87a2`.

At initial review, freeze, and post-gate verification, local `phase-2`, `origin/phase-2`, and the live remote base all matched the frozen base. HEAD/tree/base/status were unchanged across the authoritative gate. Status contained only `?? .venv-browser`; its target remained `/tmp/composure-browser-env`. The ignored axe asset retained SHA-256 `e9e5863c33a874f09bc01acd9234b7e3c871479f5eef8802fa582544465e6d01`.

Approval and gate evidence belong to the historical candidate/base above, not to a later HEAD by implication.

## Current checkpoint observation, separate from stage work

On this external-document request, I performed read-only inspection in `/home/mark/Projects/worktrees/composure/172`, branch `172`. Observed HEAD was `ca041a491f475d2809d1eec68a004d2758630c28`.

The only committed delta from the tested candidate was two added checkpoint documents:

- `cafec49`, test-writer checkpoint, `docs/handoffs/ticket-172/t172-test-writer.md`.
- `ca041a4`, implementor checkpoint, `docs/handoffs/ticket-172/t172-implementor.md`.

No implementation or test file differed from the historical candidate. Status still showed only `?? .venv-browser`. I ran no tests or gates on this newer HEAD. The upcoming reviewer-document commit will also change HEAD and must remain distinct from the original tested revision.

Main must explicitly reconcile documentation-only commits, current implementation/test blobs, and any changed base before publishing or merging. Preserve review round/cap and the exactly-one historical gate record. Do not blindly repeat the passed gate or restart TDD. Any source/test or relevant base change requires the applicable workflow review and validation decisions.

## Scope and ownership

Mark approved four Go cleanups plus retirement of #171's temporary Staticcheck lease in issue comment `5932215306`, narrowly superseding the original no-CI-change restriction. Source authority: https://github.com/markdlabrecque/composure/issues/172#issuecomment-5932215306.

Approved production files were `internal/config/config.go`, `internal/config/validate.go`, `internal/web/web.go`, `internal/content/content.go`, and `scripts/check-staticcheck-phase2.py`. The three unused declarations were deleted; only the first word of the existing `ErrAlreadyPublished` sentinel's message was lowercased. The strict helper accepts only zero scanner exit with byte-empty stdout and stderr.

No repin, dependency upgrade, workflow edit, production allowlist, suppression, PATH fallback, or new feature was present. CI remains pinned to Staticcheck v0.8.1, release 2026.2.1, using the supplied freshly installed absolute binary. Implementor routing recorded by main was `openai-codex/gpt-6-luna`, medium reasoning. Writer routing was fresh Sol/high.

I verified all eight ticket-owned production/test blobs matched prior GREEN and all three writer files were unchanged from the writer revision. Immutable test Git blob IDs:

- `tests/test_ci_staticcheck.py`: `4c4d4019742a714bdb1b666fce5aa7572262a807`.
- `tests/test_ci_phase1.py`: `2d3e8cbab2e905b84f4f8f259d1483ae65c063f6`.
- `tests/test_staticcheck_cleanup.py`: `df1bd41c3f289667980ca0e4a51a87f2548bdedd`.

## Evidence and personally completed verification

Every test shell used Go 1.27.1 via `/home/mark/.local/share/composure-toolchains/go1.27.1/go/bin` at the front of PATH.

I read the writer and implementor artifacts and inspected the actual candidate diff. Retained writer evidence showed baseline 66 Python tests passing, then 74 updated tests with 16 expected red assertions. I did not author or rerun those historical RED tests. Retained implementor evidence showed committed focused/cheap GREEN. I independently verified the post-rebase blob and GREEN claims.

My cheap checks passed:

- `python3 -m unittest discover -s tests -p test_ci_staticcheck.py -v`, 23 tests.
- `python3 -m unittest discover -s tests -p test_staticcheck_cleanup.py -v`, 5 tests.
- `python3 -m unittest discover -s tests -p test_ci_phase1.py -v`, 5 tests.
- `python3 -m unittest discover -s tests -p 'test_*.py' -v`, all 74 tests.
- `go build ./...`, `go vet ./...`, and `go test ./... -count=1`.
- `git diff --check` against the frozen base.
- `/tmp/composure-171-staticcheck-bin/staticcheck -version` and `go version -m` confirmed v0.8.1 / 2026.2.1.
- The real pinned scanner's `-f json ./...` returned exit 0 with zero bytes in both streams; the candidate helper against that real binary also passed.

Independent adversarial verification passed 160 byte/status combinations, accepting only zero exit and empty streams. Invalid CLI arity, relative/wrong/absent/directory paths, permission and exec-format failures, signal termination, actual workflow install failure with a stale binary, and a real pinned-scanner compiler failure were rejected. No stale PATH scanner executed. An extra NUL-argv case stopped in my disposable harness before candidate execution because OS argv cannot transport NUL; I recorded that harness correction and resumed remaining cheap checks. This was not a candidate failure or gate retry.

After stating provisional approval and verifying the freeze, I invoked `bash scripts/test-phase1` exactly once. It exited 0 in 19.95 seconds, including all 74 Python tests, Go build/vet/test/race checks, and six pinned browser tests. Post-gate snapshots and tracked diff confirmed the candidate/base and preserved assets were unchanged. This established final approval, not merely provisional approval.

Key external evidence paths:

- `/tmp/composure-172-stage1/handoff.md`, `red-checks.json`, and retained red/baseline logs.
- `/tmp/composure-172-stage2/`, original committed GREEN logs and candidate revision.
- `/tmp/composure-phase2-wave/172-approved-scope.txt`.
- `/tmp/composure-phase2-wave/172-implementor-current-handoff.txt`.
- `/tmp/composure-phase2-wave/172-fresh-post-rebase-cheap.log`.
- `/tmp/composure-phase2-wave/172-reviewer-r1-scope-immutability.log`.
- `/tmp/composure-phase2-wave/172-reviewer-r1-python-test_ci_staticcheck.log`, `python-test_staticcheck_cleanup.log`, `python-test_ci_phase1.log`, and `python-test_all.log`, with the same reviewer prefix for each basename.
- `/tmp/composure-phase2-wave/172-reviewer-r1-build.log`, `vet.log`, `go-test.log`, and `diff-check.log`, with the same reviewer prefix for each basename.
- `/tmp/composure-phase2-wave/172-reviewer-r1-staticcheck-version.log`, `staticcheck-buildinfo.log`, `real-staticcheck.stdout`, `real-staticcheck.stderr`, and `real-helper.log`, with the same reviewer prefix for each basename.
- `/tmp/composure-phase2-wave/172-reviewer-r1-adversarial.log`.
- `/tmp/composure-phase2-wave/172-reviewer-r1-initial-snapshot.json`, `frozen-snapshot.json`, and `post-gate-snapshot.json`, with the same reviewer prefix for each basename.
- `/tmp/composure-phase2-wave/172-reviewer-r1-fullgate.log` and `fullgate-result.json`, with the same reviewer prefix for the result basename.

Durable private archive, independently SHA-256 verified at checkpoint:

`/home/mark/.pi/agent/runtime/composure-branch-handoffs/2026-10-01/legacy-evidence.tar.gz`

SHA-256: `9116e56f3f9bb57f0f56b909ff41b9ed49766596f7014e3c4074949652f91d9f`.

Use this archive if `/tmp` artifacts disappear. Keep raw logs private; this document includes only safe summaries, paths, and hashes.

## Reconciliation and next role

`/tmp/composure-phase2-wave/checkpoint.json` still labels #172 as an active reviewer with gate authorization. That flag is stale. The completed reviewer result and `172-reviewer-r1-fullgate-result.json` establish final APPROVED, round 1/2, gate exit 0, one invocation. The checkpoint's Luna/medium fields describe implementor routing, not this Sol/high reviewer.

`/tmp/composure-pipeline-resume-handoff.md` supplies cross-role recovery context and separately proposes tooling improvements. Neither that proposal nor sibling checkpoints are additional #172 tests or source corrections. Main's recorded queue is #172 reporter, then #69 corrected review 2/2, #72 writer/implementor correction after review-1 bounce, then #137's withheld integration gate. Those sibling statuses are main-provided context, not findings independently reviewed by me.

Reporter follows only after main reconciles the checkpoint-only HEAD and current base. GitHub remains the system of record; no docs/reports fallback. I claim no push, PR, CI, merge, issue completion, or retirement. Main controls integration serialization and any later retirement.

## Limitations and suggested skills

Passing checks do not establish production security, human security approval, Hetzner acceptance, WCAG conformance, or complete axe contrast coverage. Eighteen axe audits had zero violations but some color-contrast checks were incomplete. The gate also emitted existing Python SQLite ResourceWarnings without failing. No ticket-blocking finding arose from them.

For recovery, read the named reporter profile and invoke `subagent-tdd-pipeline` under the recorded overrides, preserving review counts and evidence. Use `handoff` and `unslop` for checkpoint writing. Invoke `retire-worktree` only when main authorizes the applicable preserved-branch cleanup procedure. Stop here; this reviewer has no further stage work.
