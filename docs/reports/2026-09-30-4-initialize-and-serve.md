# Issue #4: Initialize local sites and serve published Pages

## Delivered

Added the Go 1.27.1 module and `composure` CLI with `version`, `init`, and `serve`. Initialization previews without mutation, permits an absent or empty target, refuses nonempty targets, and can apply schema v1, default config, and an optional published example with stable UUIDv7 identities. SQLite schema and access stay in `internal/store`; initialization commits to a temporary database and publishes without replacing an existing destination. Startup validates site identity, integrity, and supported schema/config versions without migration or database creation. The public server binds only to loopback, checks the request Host, serves stored published snapshots through `html/template`, and returns the documented health, 404, and method responses. Added the process/integration fixture, Go CI execution, and concise local build/start/test guidance.

No auth, admin UI, config CLI/import, migrations, or runtime DDL were added. The exhaustive compatibility/corruption matrix remains issue #5. Issue #20 retains ownership of config-validation documentation and negative fixtures; this work did not take those files or tests.

## Acceptance evidence

1. **Preview and explicit apply:** `TestInitPlanChangesNothing` covers absent and existing-empty directories without mutation; `TestInitAppliesToExistingEmptyDirectory` covers apply to an empty directory. `TestPhase1InitializeAndServe` is the real init-to-HTTP tracer.
2. **Schema, configuration, example, and IDs:** `TestPhase1InitializeAndServe` checks the applied SQLite seed, metadata, IDs, and snapshot/route linkage. `TestInitWithoutExample` checks ordinary initialization; `TestInitializedSnapshotsAreImmutable` checks update/delete rejection.
3. **Stored rendering and HTTP boundary:** `TestPhase1InitializeAndServe` checks the stored example over HTTP; `TestStoredPublishedTextIsEscaped` checks escaped stored text and newline handling. `TestPublicBoundary` checks health, unexpected Host, canonical/unknown paths, and method behavior. `TestNoncanonicalRawPublicPathsReturn404` checks encoded and path-cleaning aliases.
4. **Restart stability:** `TestPhase1InitializeAndServe` restarts the real process and checks the public content and IDs; `TestStoredPublishedTextIsEscaped` checks its fixture after restart.
5. **No overwrite and isolation:** `TestInitRefusesNonEmptySite` checks refusal/preservation; `TestInitIOFailureLeavesParentUntouched` checks failed-init preservation; `TestSeparateSitesKeepIndependentContent` checks independent initialized sites; `TestPublishedFixtureDoesNotCrossSites` checks that a published route added to site A does not resolve on site B.
6. **Local-only server and startup checks:** `TestServeRejectsMissingSiteAndNonLoopback` checks missing-site and nonloopback refusal. `TestServeRejectsRepresentativeIncompatibleAndForeignDatabase` checks representative startup rejection and preservation; the complete matrix is issue #5. Local-only limits are documented in `docs/testing.md`.
7. **Guidance and release build:** `docs/testing.md` documents a CGO-free CLI build, init plan/apply, serve, version, focused tests, and `bash scripts/test`. The current-revision gate below exercised the full script; the CLI smoke commands and CGO-free build are separately recorded below. Go tests/build introduce no Node runtime or runtime DDL.
8. **CI failure evidence:** `TestCIFailureProbe` is opt-in and was demonstrated locally to fail under `COMPOSURE_CI_FAILURE_PROBE=1`. The workflow runs the same required Composure check and only enables that probe for a labeled PR. The hosted deliberate failure, proof the required check blocks merge, and restored green hosted run have not yet been recorded; they remain pending before merge/closure.

Round-one behavioral red was retained after a compilable skeleton: `go test ./... -count=1` failed 13 behavioral tests, while compilation succeeded (`/tmp/composure-4-full-red.log`). Three separately written review regressions also failed against the frozen candidate (`/tmp/composure-4-review-red.log`): relative site URI, noncanonical raw path aliases, and version without a site. The final suite passes these cases. This excerpt preserves the behavioral red/green lineage if temporary logs are later removed.

## Verification

Candidate manifest SHA-256: `e03b4806c0af9fe65a25025a3668b75d0997be9d40c7414394f5023017c926ba` (`/tmp/composure-4-candidate.sha256`). Base and then-current `origin/develop`: `aab8afd35ed7822e5a99c784a4c6554a893ec406`.

On the reviewed candidate, all exited 0:

- `go test ./tests/integration -run '^TestPhase1InitializeAndServe$' -count=1`
- `go test ./tests/integration -run '^(TestRelativeSiteCLI|TestNoncanonicalRawPublicPathsReturn404|TestVersionWithoutSite)$' -count=1`
- `go vet ./...`
- `bash scripts/test` (22 Python runner tests plus Go build, vet, plain tests, and race tests; integration tests passed)
- `CGO_ENABLED=0 go build -o /tmp/composure ./cmd/composure`
- Actual binary `version`, plan, apply, and ephemeral loopback serve/HTTP smoke probe
- `git diff --check`

Independent reviewer (gpt-6.1-sol) approved in round two after confirming all three round-one findings fixed and the frozen candidate manifest unchanged. No must-fix finding remains. Review directly inspected publication/cleanup and startup/render safeguards. It did not exercise injected mid-transaction failures or an exact concurrent replacement at the instant before publication; those timing cases have direct code-review evidence only, not fault-injection test evidence.

Atomic publication uses `os.Link` for no-replace publication of the already-checkpointed, closed, and fsynced database, followed by unlinking the owned temporary name. This avoids clobbering an existing final database. It requires hard-link support on the site's filesystem; unsupported filesystems fail safely before publication. Lock and temporary cleanup only remove paths created by this initialization. A failure after publication leaves the valid database in place. No test injects a mid-transaction failure or exact prepublication race.

## Pending

- Hosted CI deliberate failing-test run, required-check merge-block evidence, and restored passing run on the relevant candidate.
- Hosted CI and merge, followed by the issue completion summary and closure, remain pending. Independent review is approved in round two; no hosted CI, merge, or issue closure is claimed here.
- Human security review remains a PRD requirement and has not been claimed as completed.

No reviewer-deferred defect or additional ticket was identified in round two. Later-scope work remains with its planned owners, including #5's detailed startup rejection matrix and #20's config validation documentation/negative fixtures.
