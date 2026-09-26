# Repository checks

Run `bash scripts/test` from any working directory. The runner finds the
repository from its own location and requires Python 3. While the repository
contains only planning material, it checks the required documents and parses
any JSON examples under `docs/phase1/examples/`.

When a root `go.mod` is added, the runner switches to application checks. It
requires at least one Go test file, then runs `go build ./...`, `go vet ./...`,
`go test ./...`, and `go test -race ./...`. Go source without a valid root
module fails instead of selecting bootstrap checks.

The CI workflow runs this same command for pull requests, pushes to `main`,
and manual dispatch. Configure `Composure checks` as a required status check
for `main`.
