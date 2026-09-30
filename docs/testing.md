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

Run `bash scripts/test` from any working directory. The runner finds the
repository from its own location and requires Python 3. While the repository
contains only planning material, it checks the required documents and parses
any JSON examples under `docs/phase1/examples/`.

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
