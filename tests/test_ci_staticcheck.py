"""Required-job Staticcheck wiring, using isolated fake tools.

Run: python3 -m unittest discover -s tests -p test_ci_staticcheck.py -v
These tests do not establish a clean real Staticcheck result. Strict-policy
cases use captured former exceptions and synthetic diagnostics. Every finding
must now fail, even when the scanner incorrectly exits zero.
"""

from __future__ import annotations

import json
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest

from test_ci_govulncheck import checks_job, field, steps


REPO = Path(__file__).resolve().parents[1]
# Verified primary sources, no network access from these tests:
# https://github.com/dominikh/go-tools/releases/tag/2026.2.1
# https://staticcheck.dev/changes/2026.2/ explicitly adds Go 1.27 support.
# https://proxy.golang.org/honnef.co/go/tools/@v/v0.8.1.info maps the tag;
# its .mod requires Go 1.26.0. lintcmd/cmd.go shows shared exit code 1.
INSTALL = "honnef.co/go/tools/cmd/staticcheck@v0.8.1"
GOVULN_INSTALL = "golang.org/x/vuln/cmd/govulncheck@v1.3.0"
STRICT_HELPER = "scripts/check-staticcheck-phase2.py"
# Captured from the installed v0.8.1 tool on 8e559980, not invented findings.
# Only the checkout's absolute prefix was removed from location/end.file.
KNOWN_DIAGNOSTICS = (
    {"code": "U1000", "severity": "error",
     "location": {"file": "internal/config/config.go", "line": 47, "column": 6},
     "end": {"file": "", "line": 0, "column": 0}, "message": "func integer is unused"},
    {"code": "U1000", "severity": "error",
     "location": {"file": "internal/config/validate.go", "line": 407, "column": 6},
     "end": {"file": "", "line": 0, "column": 0}, "message": "func isJSONSpace is unused"},
    {"code": "ST1005", "severity": "error",
     "location": {"file": "internal/content/content.go", "line": 25, "column": 27},
     "end": {"file": "internal/content/content.go", "line": 25, "column": 66},
     "message": "error strings should not be capitalized"},
    {"code": "U1000", "severity": "error",
     "location": {"file": "internal/web/web.go", "line": 33, "column": 5},
     "end": {"file": "", "line": 0, "column": 0}, "message": "var savedPageTemplate is unused"},
)


def known_scan_output(prefix: str = "__FIXTURE_ROOT__/") -> str:
    """Reproduce the pinned scanner's JSON lines at a different checkout root."""
    diagnostics = json.loads(json.dumps(KNOWN_DIAGNOSTICS))
    for diagnostic in diagnostics:
        for key in ("location", "end"):
            if diagnostic[key]["file"]:
                diagnostic[key]["file"] = prefix + diagnostic[key]["file"]
    return "".join(json.dumps(diagnostic) + "\n" for diagnostic in diagnostics)


def script_paths(command: str) -> list[str]:
    """Recognize repository shell helpers without imposing a helper filename."""
    return re.findall(r"\bscripts/[\w./-]+", command)


def scanner_steps(job: str) -> list[str]:
    selected = []
    for step in steps(job):
        command = field(step, "run", 8) or ""
        helpers = [REPO / name for name in script_paths(command)]
        if re.search(r"\bstaticcheck\b", command) or any(
            path.is_file() and re.search(r"\bstaticcheck\b", path.read_text())
            for path in helpers
        ):
            selected.append(step)
    return selected


class CIStaticcheckTests(unittest.TestCase):
    def setUp(self) -> None:
        self.source = (REPO / ".github/workflows/ci.yml").read_text()
        self.job = checks_job(self.source)
        self.steps = steps(self.job)

    def selected(self) -> list[str]:
        selected = scanner_steps(self.job)
        self.assertTrue(selected,
                        "required jobs.checks has no executable staticcheck run step")
        return selected

    def run_commands(self, selected: list[str], *, scan_status: int = 0,
                     install_status: int = 0, categories: tuple[str, ...] = (),
                     stderr: str = "", stdout: str = "") -> tuple[int, list[dict], str]:
        """Run actual workflow shell/helper text, never real Go or a real scanner.

        JSON diagnostics match lintcmd/format.go at official tag 2026.2.1.
        Exit 1 alone does NOT distinguish lint, compile, config or runtime errors.
        """
        with tempfile.TemporaryDirectory(prefix="composure staticcheck fixture ") as directory:
            root = Path(directory)
            tools = root / "tools"
            tools.mkdir()
            (root / "tmp").mkdir()
            (root / "go.mod").write_bytes((REPO / "go.mod").read_bytes())
            log = root / "calls.jsonl"
            fake = "#!" + sys.executable + "\n" + textwrap.dedent('''\
                import json
                import os
                from pathlib import Path
                import shutil
                import sys

                name = Path(sys.argv[0]).name
                args = sys.argv[1:]
                with open(os.environ["CALL_LOG"], "a") as log:
                    log.write(json.dumps({"tool": name, "args": args,
                                          "cwd": os.getcwd(), "executable": sys.argv[0]}) + "\\n")
                if name == "go":
                    if args == ["env", "GOPATH"]:
                        print(os.environ["GOPATH"])
                    elif len(args) == 2 and args[0] == "install" and args[1] in (
                            os.environ["STATICCHECK_INSTALL"], os.environ["GOVULN_INSTALL"]):
                        scanner = "staticcheck" if args[1] == os.environ["STATICCHECK_INSTALL"] else "govulncheck"
                        status = int(os.environ["INSTALL_STATUS"]) if scanner == "staticcheck" else 0
                        if status:
                            print("fixture tool installation failed", file=sys.stderr)
                            raise SystemExit(status)
                        target = Path(os.environ.get("GOBIN") or os.environ["GOPATH"] + "/bin") / scanner
                        target.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copyfile(Path(sys.argv[0]).with_name(scanner), target)
                        target.chmod(0o755)
                    else:
                        print("unexpected direct Go invocation: " + repr(args), file=sys.stderr)
                        raise SystemExit(91)
                elif name == "govulncheck":
                    if args != ["./..."]:
                        raise SystemExit(92)
                else:
                    if args not in (["./..."], ["-f", "json", "./..."], ["-f=json", "./..."]):
                        print("unexpected scanner invocation: " + repr(args), file=sys.stderr)
                        raise SystemExit(93)
                    print(os.environ["SCAN_STDOUT"], end="")
                    for category in json.loads(os.environ["CATEGORIES"]):
                        message = "fixture-only unexpected diagnostic"
                        location = {"file": "fixture/unknown.go", "line": 7, "column": 2}
                        if "json" in args or "-f=json" in args:
                            print(json.dumps({"code": category, "severity": "error",
                                              "location": location, "end": location,
                                              "message": message}))
                        else:
                            print("fixture/unknown.go:7:2: " + message + " (" + category + ")")
                    if os.environ["SCAN_STDERR"]:
                        print(os.environ["SCAN_STDERR"], file=sys.stderr)
                    raise SystemExit(int(os.environ["SCAN_STATUS"]))
                ''')
            for name in ("go", "staticcheck", "govulncheck"):
                path = tools / name
                path.write_text(fake)
                path.chmod(0o755)
            # Shell/stdlib helpers only. No real Go, scanners, curl, git or gh.
            for name in ("bash", "tee", "mkdir", "chmod", "mktemp", "rm", "diff",
                         "cmp", "sort", "grep", "awk", "cut", "cp", "dirname"):
                executable = shutil.which(name)
                if executable:
                    (tools / name).symlink_to(executable)
            # Record the real helper invocation, then execute its unmodified code.
            python = tools / "python3"
            python.write_text("#!" + sys.executable + "\n" + textwrap.dedent('''\
                import json
                import os
                import sys
                with open(os.environ["CALL_LOG"], "a") as log:
                    log.write(json.dumps({"tool": "python3", "args": sys.argv[1:],
                                          "cwd": os.getcwd()}) + "\\n")
                os.execv(sys.executable, [sys.executable, *sys.argv[1:]])
                '''))
            python.chmod(0o755)
            for step in selected:
                for name in script_paths(field(step, "run", 8) or ""):
                    source = REPO / name
                    self.assertTrue(source.is_file(), "missing workflow helper: " + name)
                    self.assertIn("staticcheck", source.read_text(),
                                  "only the Staticcheck helper belongs in this fixture")
                    target = root / name
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(source.read_bytes())
                    target.chmod(0o755)
            env = {
                "PATH": str(tools), "HOME": str(root), "GOPATH": str(root / "go"),
                "GOBIN": str(root / "go/bin"), "GOTOOLCHAIN": "local",
                "GITHUB_WORKSPACE": str(root), "RUNNER_TEMP": str(root / "tmp"),
                "CALL_LOG": str(log), "STATICCHECK_INSTALL": INSTALL,
                "GOVULN_INSTALL": GOVULN_INSTALL, "SCAN_STATUS": str(scan_status),
                "INSTALL_STATUS": str(install_status), "CATEGORIES": json.dumps(categories),
                "SCAN_STDERR": stderr, "SCAN_STDOUT": stdout.replace("__FIXTURE_ROOT__", str(root)),
            }
            status = 0
            output = []
            for step in selected:
                command = field(step, "run", 8)
                self.assertIsNotNone(command)
                self.assertNotIn("${{", command, "run expressions need explicit fixture support")
                self.assertIn(field(step, "shell", 8), (None, "bash"))
                self.assertIsNone(field(step, "working-directory", 8),
                                  "Staticcheck must run from the repository root")
                step_env = dict(env)
                if field(step, "env", 8) is not None:
                    environment = step.split("        env:", 1)[1]
                    for line in environment.splitlines():
                        if line.strip() and not line.startswith("          "):
                            break
                        if not line.strip():
                            continue
                        key, value = line.strip().split(":", 1)
                        value = value.strip().strip("'\"")
                        value = value.replace("${{ runner.temp }}", str(root / "tmp"))
                        value = value.replace("${{ github.workspace }}", str(root))
                        self.assertNotIn("${{", value)
                        self.assertNotIn(key, ("PATH", "CALL_LOG", "SCAN_STATUS", "INSTALL_STATUS",
                                              "CATEGORIES", "SCAN_STDERR", "SCAN_STDOUT", "STATICCHECK_INSTALL",
                                              "GOVULN_INSTALL"), "do not bypass fixture isolation")
                        if key in ("GOBIN", "GOPATH"):
                            self.assertTrue(Path(value).is_relative_to(root))
                        step_env[key] = value
                result = subprocess.run(
                    ["/bin/bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", command],
                    cwd=root, env=step_env, capture_output=True, text=True, timeout=15,
                )
                output.append(result.stdout + result.stderr)
                status = result.returncode
                if status:
                    break
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            for call in calls:
                self.assertEqual(call["cwd"], str(root), "tools must execute at module root")
            return status, calls, "\n".join(output)

    def test_scan_is_required_and_runs_after_repository_go_setup(self) -> None:
        selected = self.selected()
        self.assertEqual(field(self.job, "name", 4), "Composure checks")
        for block, indent in [(self.job.split("    steps:\n", 1)[0], 4),
                              *((step, 8) for step in selected)]:
            self.assertIn(field(block, "continue-on-error", indent), (None, "false"))
            self.assertIn(field(block, "if", indent), (None, "success()", "${{ success() }}"),
                          "Staticcheck must not be an opt-in or optional check")
        setup = [i for i, step in enumerate(self.steps)
                 if (field(step, "uses", 8) or "").startswith("actions/setup-go@")]
        self.assertEqual(len(setup), 1)
        self.assertEqual(field(self.steps[setup[0]], "go-version-file", 10), "go.mod")
        self.assertIsNone(field(self.steps[setup[0]], "go-version", 10))
        for step in selected:
            self.assertLess(setup[0], self.steps.index(step))

    def test_exact_stable_install_precedes_executed_whole_repository_scan(self) -> None:
        _, calls, output = self.run_commands(self.selected())
        installs = [call for call in calls if call["tool"] == "go"
                    and call["args"] == ["install", INSTALL]]
        self.assertEqual(len(installs), 1, "install the verified Go 1.27-compatible pin: " + output)
        scans = [call for call in calls if call["tool"] == "staticcheck"]
        self.assertEqual(len(scans), 1, "echoing/installing the tool is not a scan: " + output)
        self.assertIn(scans[0]["args"], (["./..."], ["-f", "json", "./..."], ["-f=json", "./..."]),
                      "scan every package and its tests without disabling checks or errors")
        self.assertLess(calls.index(installs[0]), calls.index(scans[0]))

    def test_install_failure_does_not_run_stale_scanner(self) -> None:
        status, calls, output = self.run_commands(self.selected(), install_status=23)
        self.assertNotEqual(status, 0, "installation failure was hidden: " + output)
        self.assertTrue(any(call["args"] == ["install", INSTALL] for call in calls), output)
        self.assertFalse(any(call["tool"] == "staticcheck" for call in calls),
                         "failed installation must not run a stale tool")

    def test_tool_runtime_errors_fail_closed_including_exit_one(self) -> None:
        selected = self.selected()
        for code, stderr in ((1, "fixture cache initialization failed"),
                             (1, ""), (2, "unsupported output format"),
                             (17, "fixture runtime failure"), (127, "command not found")):
            with self.subTest(code=code, stderr=stderr):
                status, calls, output = self.run_commands(selected, scan_status=code, stderr=stderr)
                self.assertTrue(any(call["tool"] == "staticcheck" for call in calls), output)
                self.assertNotEqual(status, 0, "tool failure was treated as allowed lint: " + output)

    def test_unknown_lint_compile_and_config_diagnostics_fail_closed(self) -> None:
        selected = self.selected()
        for categories, stderr in ((("SA1019",), ""), (("compile",), ""),
                                   (("config",), ""), (("SA1019", "compile"), ""),
                                   (("SA1019",), "fixture tool failure alongside diagnostics")):
            with self.subTest(categories=categories, stderr=stderr):
                status, calls, output = self.run_commands(
                    selected, scan_status=1, categories=categories, stderr=stderr,
                )
                self.assertTrue(any(call["tool"] == "staticcheck" for call in calls), output)
                self.assertNotEqual(status, 0, "unknown findings/tool errors were concealed: " + output)

    def test_no_blanket_warning_bypass_or_live_issue_expiration(self) -> None:
        selected = self.selected()
        sources = [field(step, "run", 8) or "" for step in selected]
        for command in list(sources):
            sources.extend((REPO / name).read_text() for name in script_paths(command))
        for source in sources:
            self.assertNotRegex(source, r"\|\|\s*(?:true\b|:(?:\s|$)|exit\s+0\b)",
                                "a blanket bypass cannot distinguish lint from tool failures")
            self.assertNotRegex(source, r"(?:api\.github\.com|gh\s+(?:api|issue)|secrets\.)",
                                "strict checks must run locally without tracker queries/secrets")

    def test_existing_govuln_browser_gate_order_and_job_metadata_are_preserved(self) -> None:
        # Make this an acceptance test for the addition, not an unrelated green test.
        self.selected()
        self.assertEqual(self.source.split("jobs:\n", 1)[0], """name: CI

on:
  pull_request:
    types: [opened, synchronize, reopened, labeled, unlabeled]
  push:
    branches:
      - main
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true

""")
        self.assertEqual(re.findall(r"^  [\w-]+:$", self.source.split("jobs:\n", 1)[1], re.MULTILINE),
                         ["  checks:"])
        self.assertEqual(self.job.split("    steps:\n", 1)[0],
                         "    name: Composure checks\n    runs-on: ubuntu-latest\n    timeout-minutes: 15\n")
        self.assertEqual(re.findall(r"uses: (\S+)", self.job), [
            "actions/checkout@11d5960a326750d5838078e36cf38b85af677262",
            "actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff",
        ])
        retained = [step for step in self.steps if step not in self.selected()]
        self.assertEqual([field(step, "name", 0) for step in retained], [
            "Check out source", "Set up Go from go.mod", "Check Go vulnerabilities",
            "Install pinned browser tests", "Install Chromium system dependencies", "Run Phase 1 gate",
        ])
        vuln = retained[2]
        self.assertEqual(field(vuln, "run", 8),
                         'export GOBIN="$(go env GOPATH)/bin"\n'
                         'go install ' + GOVULN_INSTALL + '\n"$GOBIN/govulncheck" ./...')
        self.assertIn(field(vuln, "continue-on-error", 8), (None, "false"))
        self.assertIn(field(vuln, "if", 8), (None, "success()", "${{ success() }}"))
        self.assertEqual([field(step, "run", 8) for step in retained[3:]], [
            "bash scripts/install-browser-tests",
            ".venv-browser/bin/python -m playwright install-deps chromium",
            "bash scripts/test-phase1",
        ])
        self.assertEqual(field(retained[-1], "COMPOSURE_CI_FAILURE_PROBE", 10), "0")
        self.assertEqual(field(retained[-1], "COMPOSURE_BROWSER_FAILURE_PROBE", 10),
                         "${{ github.event_name == 'pull_request' && contains(github.event.pull_request.labels.*.name, 'ci-failure-probe') && '1' || '0' }}")


class StaticcheckFixtureTests(unittest.TestCase):
    def test_fake_tools_model_success_shared_exit_one_and_install_failure(self) -> None:
        fixture = CIStaticcheckTests()
        block = """name: Fixture only
        run: |
          go install honnef.co/go/tools/cmd/staticcheck@v0.8.1
          staticcheck -f json ./...
"""
        for scan_status, install_status, categories, stderr, expected in (
            (0, 0, (), "", 0),
            (1, 0, ("SA1019",), "", 1),
            (1, 0, ("compile",), "", 1),
            (1, 0, ("config",), "", 1),
            (1, 0, (), "runtime error", 1),
            (2, 0, (), "invalid format", 2),
            (0, 23, (), "", 23),
        ):
            with self.subTest(scan_status=scan_status, categories=categories,
                              install_status=install_status, stderr=stderr):
                status, calls, output = fixture.run_commands(
                    [block], scan_status=scan_status, install_status=install_status,
                    categories=categories, stderr=stderr,
                )
                self.assertEqual(status, expected, output)
                self.assertEqual([call["tool"] for call in calls],
                                 ["go"] if install_status else ["go", "staticcheck"])
                if categories:
                    diagnostic = json.loads(output)
                    self.assertEqual(diagnostic["code"], categories[0])
                    self.assertEqual(diagnostic["location"]["file"], "fixture/unknown.go")
        status, _, _ = fixture.run_commands(
            [block.replace("staticcheck -f json ./...", "staticcheck -f json ./... || true")],
            scan_status=1, categories=("compile",),
        )
        self.assertEqual(status, 0, "fixture must expose, not repair, swallowed failures")

    def test_fake_go_rejects_suite_commands_mutable_and_unrelated_installs(self) -> None:
        fixture = CIStaticcheckTests()
        for args in (["test", "./..."], ["test", "-race", "./..."], ["build", "./..."],
                     ["vet", "./..."], ["install", "honnef.co/go/tools/cmd/staticcheck@latest"],
                     ["install", "example.test/tool@v1.2.3"]):
            with self.subTest(args=args):
                status, calls, output = fixture.run_commands([
                    "name: Forbidden fixture command\n        run: go " + shlex.join(args) + "\n",
                ])
                self.assertEqual(status, 91, output)
                self.assertEqual(calls[0]["args"], args)
                self.assertIn("unexpected direct Go invocation", output)

    def test_optional_job_comment_or_echo_cannot_satisfy_executed_scan(self) -> None:
        source = """jobs:
  checks:
    name: Composure checks
    steps:
      - name: Comment
        # staticcheck ./... is not executed
        run: echo ordinary-checks
  optional:
    steps:
      - name: Lint
        run: staticcheck ./...
"""
        self.assertEqual(scanner_steps(checks_job(source)), [])
        fixture = CIStaticcheckTests()
        status, calls, _ = fixture.run_commands([
            "name: Echo only\n        run: echo staticcheck ./...\n",
        ])
        self.assertEqual(status, 0)
        self.assertEqual(calls, [])


class StaticcheckCapturedFixtureTests(unittest.TestCase):
    def test_fake_scanner_reproduces_real_json_and_shared_exit_one(self) -> None:
        fixture = CIStaticcheckTests()
        block = "name: Raw captured scan\n        run: staticcheck -f json ./...\n"
        status, calls, output = fixture.run_commands(
            [block], scan_status=1, stdout=known_scan_output(),
        )
        self.assertEqual(status, 1)
        self.assertEqual(calls[0]["args"], ["-f", "json", "./..."])
        root = calls[0]["cwd"]
        self.assertEqual(output, known_scan_output(root + "/"))
        self.assertEqual(len(output.splitlines()), 4)
        status, _, output = fixture.run_commands(
            [block], scan_status=1, stdout="not JSON\n", stderr="tool failed",
        )
        self.assertEqual(status, 1)
        self.assertIn("not JSON", output)
        self.assertIn("tool failed", output)


class CIStaticcheckStrictPolicyTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fixture = CIStaticcheckTests()
        self.fixture.setUp()

    def selected(self) -> list[str]:
        # Fail first for missing CI wiring, not a failed attempt to import a helper.
        selected = self.fixture.selected()
        commands = "\n".join(field(step, "run", 8) or "" for step in selected)
        self.assertIn(STRICT_HELPER, script_paths(commands),
                      "CI must invoke the strict Phase 2 Staticcheck helper")
        self.assertTrue((REPO / STRICT_HELPER).is_file(),
                        "required strict-scanner API is missing: " + STRICT_HELPER)
        return selected

    def assert_scan(self, calls: list[dict], output: str) -> None:
        installs = [call for call in calls if call["tool"] == "go"
                    and call["args"] == ["install", INSTALL]]
        scans = [call for call in calls if call["tool"] == "staticcheck"]
        helpers = [call for call in calls if call["tool"] == "python3"
                   and call["args"][:1] == [STRICT_HELPER]]
        self.assertEqual(len(installs), 1, output)
        self.assertEqual(len(helpers), 1, "execute the helper, do not merely mention it: " + output)
        self.assertEqual(len(helpers[0]["args"]), 2,
                         "helper API: python3 scripts/check-staticcheck-phase2.py SCANNER")
        self.assertEqual(len(scans), 1, output)
        self.assertIn(scans[0]["args"], (["-f", "json", "./..."], ["-f=json", "./..."]))
        self.assertLess(calls.index(installs[0]), calls.index(helpers[0]))
        self.assertLess(calls.index(helpers[0]), calls.index(scans[0]))
        # The supplied scanner must be the freshly installed binary, not PATH's stale copy.
        supplied = Path(helpers[0]["args"][1])
        self.assertTrue(supplied.is_absolute(), output)
        self.assertEqual(supplied.name, "staticcheck", output)
        self.assertTrue(supplied.is_relative_to(Path(helpers[0]["cwd"])), output)
        self.assertNotEqual(supplied.parent.name, "tools", output)
        self.assertEqual(Path(scans[0]["executable"]), supplied,
                         "helper must run its supplied freshly installed scanner")

    def test_exact_four_former_exceptions_fail_required_step_in_either_order(self) -> None:
        selected = self.selected()
        for stdout in (known_scan_output(),
                       "".join(reversed(known_scan_output().splitlines(keepends=True)))):
            with self.subTest(order=stdout):
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=1, stdout=stdout,
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "the four former exceptions must now fail: " + output)

    def test_helper_cli_itself_rejects_the_captured_scan(self) -> None:
        self.selected()
        # Invoke the unchanged production CLI directly as well as through CI.
        block = ("name: Helper API\n        run: |\n"
                 "          export GOBIN=\"$(go env GOPATH)/bin\"\n"
                 "          go install " + INSTALL + "\n"
                 "          python3 " + STRICT_HELPER + " \"$GOBIN/staticcheck\"\n")
        status, calls, output = self.fixture.run_commands(
            [block], scan_status=1, stdout=known_scan_output(),
        )
        self.assert_scan(calls, output)
        self.assertNotEqual(status, 0, "direct helper invocation must reject former exceptions: " + output)

    def test_partial_cleanup_still_fails_on_remaining_findings(self) -> None:
        selected = self.selected()
        lines = known_scan_output().splitlines(keepends=True)
        for index in range(4):
            with self.subTest(removed=KNOWN_DIAGNOSTICS[index]):
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=1, stdout="".join(lines[:index] + lines[index + 1:]),
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "remaining findings must fail after partial cleanup: " + output)

    def test_clean_zero_exit_with_empty_stdout_and_stderr_passes(self) -> None:
        status, calls, output = self.fixture.run_commands(self.selected(), scan_status=0)
        self.assert_scan(calls, output)
        self.assertEqual(status, 0, "a clean scan must pass after retiring the exception: " + output)

    def test_extra_unknown_duplicate_compile_or_config_findings_fail_closed(self) -> None:
        selected = self.selected()
        extras = [KNOWN_DIAGNOSTICS[0],
                  {**KNOWN_DIAGNOSTICS[0], "code": "SA1019"},
                  {**KNOWN_DIAGNOSTICS[0], "code": "compile"},
                  {**KNOWN_DIAGNOSTICS[0], "code": "config"},
                  {**KNOWN_DIAGNOSTICS[0], "code": "U1000", "message": "func newUnused is unused"}]
        related = json.loads(json.dumps(KNOWN_DIAGNOSTICS))
        related[0]["related"] = [{"location": related[0]["location"],
                                   "end": related[0]["end"], "message": "new related finding"}]
        status, calls, output = self.fixture.run_commands(
            selected, scan_status=1,
            stdout="".join(json.dumps(item) + "\n" for item in related),
        )
        self.assert_scan(calls, output)
        self.assertNotEqual(status, 0, "related diagnostics must fail: " + output)
        for extra in extras:
            with self.subTest(extra=extra):
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=1,
                    stdout=known_scan_output() + json.dumps(extra) + "\n",
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "extra findings must fail: " + output)

    def test_exact_codes_messages_severity_and_locations_cannot_be_normalized_away(self) -> None:
        selected = self.selected()
        mutations = [("code", None, "ST1005"), ("severity", None, "warning"),
                     ("message", None, "func integer is unused "),
                     ("message", None, "FUNC integer is unused"),
                     ("location", "file", "other/internal/config/config.go"),
                     ("location", "line", 48), ("location", "column", 7),
                     ("end", "file", "internal/config/config.go"),
                     ("end", "line", 1), ("end", "column", 1)]
        for key, nested, value in mutations:
            with self.subTest(key=key, nested=nested, value=value):
                diagnostics = json.loads(json.dumps(KNOWN_DIAGNOSTICS))
                if nested:
                    diagnostics[0][key][nested] = value
                else:
                    diagnostics[0][key] = value
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=1,
                    stdout="".join(json.dumps(item) + "\n" for item in diagnostics),
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "changed findings must still fail: " + output)
        # ST1005 has a real nonempty end position, which must also match exactly.
        for key, value in (("file", "internal/config/config.go"), ("line", 26), ("column", 67)):
            with self.subTest(st1005_end=key):
                diagnostics = json.loads(json.dumps(KNOWN_DIAGNOSTICS))
                diagnostics[2]["end"][key] = value
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=1,
                    stdout="".join(json.dumps(item) + "\n" for item in diagnostics),
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, output)

    def test_malformed_json_or_diagnostic_schema_fails_closed(self) -> None:
        selected = self.selected()
        raw = known_scan_output()
        invalid = ["not JSON\n", raw + "tool crashed\n", raw[:-3],
                   "[]\n", "null\n", "42\n", "{}\n", raw + "{}\n"]
        for key in ("code", "severity", "location", "end", "message"):
            diagnostics = json.loads(json.dumps(KNOWN_DIAGNOSTICS))
            del diagnostics[0][key]
            invalid.append("".join(json.dumps(item) + "\n" for item in diagnostics))
        for key, value in (("line", "47"), ("line", True), ("column", None)):
            diagnostics = json.loads(json.dumps(KNOWN_DIAGNOSTICS))
            diagnostics[0]["location"][key] = value
            invalid.append("".join(json.dumps(item) + "\n" for item in diagnostics))
        for stdout in invalid:
            with self.subTest(stdout=stdout):
                status, calls, output = self.fixture.run_commands(selected, scan_status=1, stdout=stdout)
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "malformed scan output was accepted: " + output)

    def test_known_json_does_not_hide_tool_errors_or_inconsistent_exit_status(self) -> None:
        selected = self.selected()
        for code, stderr in ((1, "cache initialization failed"), (1, "invalid staticcheck.conf"),
                             (0, ""), (2, ""), (17, "runtime failure"), (127, "command not found")):
            with self.subTest(code=code, stderr=stderr):
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=code, stdout=known_scan_output(), stderr=stderr,
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "known JSON cannot excuse tool failure: " + output)

    def test_zero_exit_with_any_stdout_fails_even_without_stderr(self) -> None:
        selected = self.selected()
        for stdout in (known_scan_output(), "not JSON\n", "{}\n", "[]\n", "null\n",
                       "42\n", "\n", " \t\n"):
            with self.subTest(stdout=stdout):
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=0, stdout=stdout,
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "zero exit cannot excuse nonempty stdout: " + output)
        for category in ("SA1019", "U1000", "ST1005", "compile", "config"):
            with self.subTest(category=category):
                status, calls, output = self.fixture.run_commands(
                    selected, scan_status=0, categories=(category,),
                )
                self.assert_scan(calls, output)
                self.assertNotEqual(status, 0, "zero exit cannot excuse unknown diagnostics: " + output)

    def test_any_stderr_fails_even_for_zero_exit_and_empty_stdout(self) -> None:
        selected = self.selected()
        for code in (0, 1):
            for stdout in ("", known_scan_output()):
                for stderr in ("cache initialization failed", "invalid staticcheck.conf", " ", "\t", "\n"):
                    with self.subTest(code=code, stdout=stdout, stderr=stderr):
                        status, calls, output = self.fixture.run_commands(
                            selected, scan_status=code, stdout=stdout, stderr=stderr,
                        )
                        self.assert_scan(calls, output)
                        self.assertNotEqual(status, 0, "any stderr must fail: " + output)

    def test_helper_rejects_invalid_cli_and_never_falls_back_to_path(self) -> None:
        self.selected()
        for argument in ("", "staticcheck", "/missing/govulncheck", "/missing/staticcheck",
                         "/missing/staticcheck extra"):
            with self.subTest(argument=argument):
                status, calls, output = self.fixture.run_commands([
                    "name: Invalid helper CLI\n        run: python3 " + STRICT_HELPER + " " + argument + "\n",
                ])
                self.assertNotEqual(status, 0, "invalid/missing supplied scanner must fail: " + output)
                self.assertTrue(any(call["tool"] == "python3" for call in calls), output)
                self.assertFalse(any(call["tool"] == "staticcheck" for call in calls),
                                 "a missing supplied scanner must not run PATH's stale tool")

    def test_install_failure_cannot_accept_known_json_from_a_stale_binary(self) -> None:
        status, calls, output = self.fixture.run_commands(
            self.selected(), install_status=23, scan_status=1, stdout=known_scan_output(),
        )
        self.assertNotEqual(status, 0, output)
        self.assertTrue(any(call["args"] == ["install", INSTALL] for call in calls), output)
        self.assertFalse(any(call["tool"] == "staticcheck" for call in calls), output)
