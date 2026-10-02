# Ticket #137 test-writer checkpoint

## Role and stopping point

Live agent `t137-test-writer`, logical test-writer, code route. Main remains the orchestrator. My stage assignment was Sol/high, using the named Pi test-writer profile. Herdr-native named Pi dispatch is Mark's explicit transport override. Implementor assignment was `openai-codex/gpt-6-luna`, high, selected by the sole Luna label. The later fresh reviewer handoff specifies Sol/high.

Mark authorized this documentation-only checkpoint on the current branch, overriding the handoff skill's temporary-directory default and this role's normal test-only write restriction. This checkpoint does not resume stage work or grant implementation, review, reporting, or retirement authority.

Verified checkout `/home/mark/Projects/worktrees/composure/137`, branch `137`. Before any checkpoint commits, the current historical candidate is `eb1f9f6e35c7c5b6027ff60d7942125a41aa9c74`, against phase-2 base `f5a010dce03d2f4a8fb797029b59c2d1bacfcb1d`. Reviewer round is 1, cap 2. Do not reset that count on recovery.

## Work I personally completed

I read the task, repository workflow and upload contract, confirmed the pre-test baseline, resolved acceptance questions with Mark, and authored only `internal/upload/kind_test.go`. I committed the original writer result at `cc5e6a7a08c38de6d1e3416100e7e80d95aaf649` on original base `0a6c93616be0dbcde4b629de54ec9a785926237c`.

Original checks used Go 1.27.1 from `/home/mark/.local/share/composure-toolchains/go1.27.1/go/bin`:

- `CGO_ENABLED=0 go test ./...` passed before adding tests.
- `CGO_ENABLED=0 go test ./internal/upload -run TestDetectKind -count=1` failed with exit 1 at the committed writer revision. This was deliberately missing-API compile RED, not a behavioral failure.
- `CGO_ENABLED=0 go test -gcflags=-e ./internal/upload -run TestDetectKind -count=1` showed only absent `DetectKind`, `Kind`, and the five kind constants.
- Formatting and whitespace checks passed. I did not implement production code or run the authoritative gate.

Evidence directory `/tmp/composure-137-test-writer-p56GFj/` contains `baseline.log`, `focused-red.log`, `focused-red-expanded.log`, `focused-red-committed.log`, and `revision.log`.

The writer commit was subsequently rebased as `24112d7`. I verified the original writer revision and current candidate contain identical test bytes:

- Git blob `d3db0bbbd9d3606787e052595106101a8584693d`.
- SHA-256 `0dca45e177869d6be3c0c86187587bf4aaa69a1c3cdc420a3c92550f2a280948`.

The implementation blob at the historical candidate is `8e85f599bd4eb63d9183dc0fe5eac3d0ed9a588c`, SHA-256 `6ebc57b42bb2e687a4e174af397c10034ac1ba873878fcd43e0feaac193f85fc`. This is sibling work, not my implementation.

## Scope decisions

Refer to the committed tests and issue #137 rather than rebuilding the spec. The agreed API is `DetectKind(filename string, data []byte) (Kind, error)`, with `JPEG`, `PNG`, `WebP`, `PDF`, and `DOCX` constants.

Mark accepted case-insensitive matching extensions, including both `.jpg` and `.jpeg`, rejection of missing extensions and trailing whitespace, and signature-only recognition. Minimum signatures are JPEG 3 bytes, PNG 8, RIFF/WebP 12 with variable RIFF size bytes, PDF 5, and ZIP 4. ZIP magic plus `.docx` is a provisional DOCX classification only. These tests cover disguises, extension mismatches, unknown contents, corrupted signatures and every shorter signature prefix. They do not prove complete-file validity or safety.

Size, dimensions, decoding, re-encoding, and full DOCX/OOXML checks are outside #137. Mark also requested configurable field-level size and dimension restrictions with nothing global. That override still conflicts with `docs/phase2/uploads.md` and must be reconciled by main in the later contract and validator tickets #138/#139. No guessed DOCX expansion limits belong here; #140 owns archive validation after approved policies.

## Later sibling work and reconciled state

I read the later stage evidence during this checkpoint; I did not rerun or independently perform those stages.

- Implementor GREEN at prior candidate `6ec75c3f0bdff51549bd2f2a69e2f4a2c3d7d078` is recorded in `/tmp/composure-137-stage2/handoff-results.txt` and `candidate-verification.txt`. Focused tests, race with cgo enabled, build, vet, Go suite and 66 Python checks passed. One initial race invocation with cgo disabled was not runnable; the cgo-enabled check passed. No full gate was run.
- Current rebased candidate/base and matching blobs are recorded in `/tmp/composure-phase2-wave/137-reviewer-r1-freeze.log`.
- Reviewer cheap checks are recorded in `/tmp/composure-phase2-wave/137-reviewer-r1-commands.log` and the corresponding `focused`, `race`, `build`, `vet`, `go-test`, and `python` logs. All recorded exits are 0.
- `/tmp/composure-phase2-wave/137-reviewer-r1-adversarial.log` records 95,335 deterministic permutations and passing concurrent-purity checks. `137-reviewer-r1-fuzz.log` records a passing five-second run with 616,064 executions.
- Main's checkpoint and `/tmp/composure-pipeline-resume-handoff.md` identify the result as round-1 provisional cheap approval, no must-fix findings. It is not final approval. The authoritative `bash scripts/test-phase1` gate was expressly withheld pending integration queue #172, then #69, then #72.

`/tmp/composure-phase2-wave/checkpoint.json` still says the fresh reviewer is active. Treat that flag as stale. The reconciled stopping point is cheap review complete, provisionally approved on the historical candidate/base, waiting for main's integration sequencing. I did not edit that shared state file.

## Durable evidence and recovery

The private durable archive is `/home/mark/.pi/agent/runtime/composure-branch-handoffs/2026-10-01/legacy-evidence.tar.gz`. I verified SHA-256 `9116e56f3f9bb57f0f56b909ff41b9ed49766596f7014e3c4074949652f91d9f`. It contains the original writer directory, stage-2 evidence, and the reviewer/coordination artifacts named above. Use it if `/tmp` is lost; do not commit raw logs or secrets.

Next logical action belongs to main, followed by reviewer continuation after the integration queue and any necessary rebase. Explicitly reconcile documentation-only checkpoint commits and any changed base with the original review evidence. Preserve round 1/cap 2 and immutable writer tests. Applicable changed-base cheap review and provisional freeze must be current before the reviewer-owned full gate. This ticket has no recorded full-gate invocation to repeat and no final approval yet. Reporter comes only after required review and gates pass.

No push, PR, hosted CI, merge, issue closure, human security review, or deployment acceptance is claimed by this checkpoint. No #137 acceptance ambiguity remains; the field-only policy follow-up and integration/gate hold remain open.

At inspection the only untracked item was the expected `.venv-browser` symlink to `/tmp/composure-browser-env`; the index was empty. Preserve that symlink and ignored `tests/browser/vendor/axe.min.js`. This checkpoint changes only this document, not implementation/test blobs. Main owns any later worktree retirement.

## Suggested skills

- `subagent-tdd-pipeline` for main's role sequencing and revision-bound review/gate reconciliation.
- `handoff` and `unslop` for checkpoint continuation, retaining Mark's committed-location override.
- `retire-worktree` only for main's separately authorized cleanup procedure.
