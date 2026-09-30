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

Run `go test ./...` for Go tests. Run
`go test ./... -run TestPhase1InitializeAndServe -count=1` for the focused
process tests. These build and start the real CLI with temporary SQLite sites.
For publication regressions, run
`go test ./tests/integration -run 'TestPhase1PublishPage|TestPublishTicket9' -count=1`.
This exercises the real admin/public handlers and SQLite publication
transaction, including rollback after a test-only SQLite trigger failure.
For configuration-driven initialization and generated rendering, run
`go test ./tests/integration ./internal/site -run 'TestPhase1ConfigRoundTrip|TestConfigInitTicket12' -count=1`.

Run `bash scripts/test` from any working directory. The runner finds the
repository from its own location and requires Python 3. While the repository
contains only planning material, it checks the required documents and parses
any JSON examples under `docs/phase1/examples/`.

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

The CI workflow runs this same command for pull requests, pushes to `main`,
and manual dispatch. Configure `Composure checks` as a required status check
for the base branch, `develop`.

For an explicit CI failure demonstration, a maintainer adds the
`ci-failure-probe` label to the pull request. Label changes rerun the same
workflow. Membership of that full label name, compared case-insensitively by
GitHub, sets `COMPOSURE_CI_FAILURE_PROBE=1` and fails
`TestCIFailureProbe` in the real Go suite. Verify that the required check fails
and blocks merging, then remove the label and verify the restored passing run
on the current candidate. Other events and unlabelled pull requests set the
variable to `0`; no failing source revision is needed.
