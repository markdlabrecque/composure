# Repository checks

Use Go 1.27.1 from the repository root. Build the CLI without cgo:

```sh
CGO_ENABLED=0 go build -o /tmp/composure ./cmd/composure
/tmp/composure init --site /tmp/composure-demo --example
/tmp/composure init --site /tmp/composure-demo --example --apply
/tmp/composure serve --site /tmp/composure-demo --addr 127.0.0.1:0
/tmp/composure version
```

Choose an absent or empty site directory. Initialization changes nothing
without `--apply`; `--example` opts into the published demonstration Page.
Phase 1 serves only loopback addresses. Port 0 selects an available port;
use the printed address to open `/example`. Stop the server with Ctrl-C.

For fresh-site first-administrator initialization, add `--admin-email EMAIL`
to the planning and apply commands. Before applying, set
`COMPOSURE_ADMIN_PASSWORD` through your local secret input mechanism; do not
put the password in command arguments or commit it. Apply creates an active
account with administrator and editor roles and a hashed password. An
explicitly empty or ASCII-space-only email, missing or empty apply password,
or existing account is refused. Legacy initialization without the email flag
remains supported.

Run `go test ./tests/integration -run TestAdminInit -count=1` for the real-CLI
administrator initialization, password, and email boundary tests.

Run `go test ./...` for Go tests. Run
`go test ./... -run TestPhase1InitializeAndServe -count=1` for the focused
process tests. These build and start the real CLI with temporary SQLite sites.
For the upload kind detector's fixed seeds and bounded local fuzz campaign,
see [the Phase 2 validator fuzz check](phase2/type-validator-fuzz.md).
For publication regressions, run
`go test ./tests/integration -run 'TestPhase1PublishPage|TestPublishTicket9' -count=1`.
This exercises the real admin/public handlers and SQLite publication
transaction, including rollback after a test-only SQLite trigger failure.
For configuration-driven initialization and generated rendering, run
`go test ./tests/integration ./internal/site -run 'TestPhase1ConfigRoundTrip|TestConfigInitTicket12' -count=1`.

Run `bash scripts/test-phase1` from any working directory for the complete
Phase 1 gate. It requires a valid root Go module, runs `bash scripts/test`
once, then runs `bash scripts/test-browser` once. A failure stops the sequence
and returns a nonzero status. The shared runner finds the repository from its
own location and requires Python 3. While used on a repository containing only
planning material, `bash scripts/test` checks the required documents and
parses any JSON examples under `docs/phase1/examples/`; the Phase 1 entry point
rejects that bootstrap-only mode.

The real Chromium Page journey is a separate focused check. Run
`scripts/install-browser-tests` once to create `.venv-browser`, install the
exact Python dependency pins, download their compatible Chromium build, and
fetch the checksum-verified axe-core asset. This requires Python 3.9 or newer
(Playwright 1.58.0 declares `Requires-Python: >=3.9`), `venv`, pip, and `curl`;
Playwright may also need the host libraries listed in its browser
installation documentation. Then run `scripts/test-browser`. The journey uses
Playwright 1.58.0, which pins Chromium/headless-shell revision 1208
(Chromium 145.0.7632.6); Python dependencies are pinned in
`tests/browser/requirements.txt`. Before the tests run, the focused command
checks Playwright, the launched Chromium, and axe-core 4.11.0 (SHA-256
`e9e5863c33a874f09bc01acd9234b7e3c871479f5eef8802fa582544465e6d01`) against
those pins. It builds the real CLI and starts a fresh SQLite site for each
width. Passing axe checks do not establish WCAG conformance.

When a root `go.mod` is added, the runner switches to application checks. It
first asks the selected Go toolchain for `go list -json ./...` metadata and
requires at least one package with an active internal or external test file.
It then runs `go build ./...`, `go vet ./...`, `go test ./...`, and
`go test -race ./...`. Go source without a valid root module fails instead of
selecting bootstrap checks.

The hosted `Composure checks` workflow runs on pull requests, pushes to `main`,
and manual dispatch. Before the phase gate, it runs govulncheck v1.3.0 and
Staticcheck v0.8.1 / 2026.2.1. Staticcheck must exit zero with empty stdout
and stderr; the temporary findings exception was removed in #172. These
scanner steps are hosted checks, not part of `scripts/test-phase1`.
It installs the pinned browser test environment with
`bash scripts/install-browser-tests`, installs Chromium's Ubuntu system
libraries with `.venv-browser/bin/python -m playwright install-deps chromium`,
then invokes `bash scripts/test-phase1` once. The phase gate runs the shared Go
runner and focused browser journey in sequence. Missing browser prerequisites
or a failed browser assertion fail the same required check. Configure
`Composure checks` as a required status check on every phase integration
branch (`phase-1`, `phase-2`, `phase-3`).

For an explicit CI failure demonstration, a maintainer adds the
`ci-failure-probe` label to the pull request. Label changes rerun the same
workflow. Membership of that full label name, compared case-insensitively by
GitHub, sets `COMPOSURE_BROWSER_FAILURE_PROBE=1` for the phase gate. The real
Page journey then negates its initial visible empty-state assertion, causing a
Playwright assertion failure after the Go suite passes. The Go probe variable
remains `0`. Verify that the required check fails and blocks merging, then
remove the label and verify the restored passing run on the current candidate.
The same failure and restoration can be reproduced locally:

```sh
COMPOSURE_BROWSER_FAILURE_PROBE=1 COMPOSURE_CI_FAILURE_PROBE=0 bash scripts/test-phase1
bash scripts/test-phase1
```

The first command is expected to exit nonzero at the real Playwright assertion;
the second runs with both probe variables unset and must pass. For hosted
evidence, retain the reviewed PR head and base SHAs, the workflow's synthetic
merge checkout SHA, and both failed and restored passing run URLs. Keep logs
showing the Playwright assertion failure and the passing build, vet, Go test,
race, and browser steps. Other events and unlabelled pull requests set the
browser probe to `0`.
