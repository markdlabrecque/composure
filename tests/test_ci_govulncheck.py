"""Required-job vulnerability checks, using fake tools and no vulnerability DB.

Run: python3 -m unittest discover -s tests -p test_ci_govulncheck.py -v
These wiring checks do not establish a clean real govulncheck result.
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


REPO = Path(__file__).resolve().parents[1]
TOOL = "golang.org/x/vuln/cmd/govulncheck"


def checks_job(source: str) -> str:
    """Bounded reader for the existing CI layout, not a general YAML parser."""
    jobs = source.split("jobs:\n", 1)[1]
    match = re.search(r"^  checks:\n(.*?)(?=^  [\w-]+:|\Z)", jobs,
                      flags=re.MULTILINE | re.DOTALL)
    if match is None:
        raise AssertionError("existing required jobs.checks is missing")
    return match[1]


def field(block: str, name: str, indent: int) -> str | None:
    match = re.search(rf"^{' ' * indent}{re.escape(name)}: *(.*)$", block,
                      flags=re.MULTILINE)
    if match is None:
        return None
    value = match[1].strip()
    if value not in ("|", "|-", ">", ">-"):
        return value.strip("'\"")
    lines = []
    for line in block[match.end():].splitlines()[1:]:
        if line.strip() and len(line) - len(line.lstrip()) <= indent:
            break
        lines.append(line)
    command = textwrap.dedent("\n".join(lines)).strip()
    return " ".join(command.splitlines()) if value.startswith(">") else command


def steps(job: str) -> list[str]:
    return re.split(r"^      - ", job, flags=re.MULTILINE)[1:]


class CIGovulncheckTests(unittest.TestCase):
    def setUp(self) -> None:
        self.source = (REPO / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        self.job = checks_job(self.source)
        self.steps = steps(self.job)

    def scan_steps(self) -> list[str]:
        selected = [step for step in self.steps
                    if re.search(r"\bgovulncheck\b", field(step, "run", 8) or "")]
        self.assertTrue(selected,
                        "required jobs.checks has no executable govulncheck run step")
        return selected

    def assert_required(self, block: str, indent: int) -> None:
        self.assertIn(field(block, "continue-on-error", indent), (None, "false"),
                      "vulnerability failures must not be optional")
        self.assertIn(field(block, "if", indent),
                      (None, "success()", "${{ success() }}"),
                      "the required vulnerability check must not depend on opt-in conditions")

    def run_commands(self, selected: list[str], *, scan_status: int = 0,
                     install_status: int = 0) -> tuple[int, list[dict], str]:
        """Execute only selected inline steps with Go/tool calls intercepted.

        No application scripts or real Go binaries are copied into this fixture.
        The fixture records argv and cwd instead of reading repository packages.
        """
        with tempfile.TemporaryDirectory(prefix="composure vuln CI fixture ") as directory:
            root = Path(directory)
            tools = root / "tools"
            tools.mkdir()
            log = root / "calls.jsonl"
            (root / "go.mod").write_bytes((REPO / "go.mod").read_bytes())
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
                                          "cwd": os.getcwd()}) + "\\n")
                if name == "govulncheck":
                    print("fixture vulnerability scan")
                    raise SystemExit(int(os.environ["SCAN_STATUS"]))
                if args == ["env", "GOPATH"]:
                    print(os.environ["GOPATH"])
                elif args == ["env", "GOBIN"]:
                    print(os.environ["GOBIN"])
                elif len(args) == 2 and args[0] == "install" and args[1].startswith(
                        "golang.org/x/vuln/cmd/govulncheck@"):
                    status = int(os.environ["INSTALL_STATUS"])
                    if status:
                        raise SystemExit(status)
                    target = Path(os.environ["GOBIN"]) / "govulncheck"
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(Path(sys.argv[0]).with_name("govulncheck"), target)
                    target.chmod(0o755)
                else:
                    print("unexpected fake Go invocation", args, file=sys.stderr)
                    raise SystemExit(91)
                ''')
            for name in ("go", "govulncheck"):
                path = tools / name
                path.write_text(fake, encoding="utf-8")
                path.chmod(0o755)
            # Keep the shell fixture off the real Go PATH, including pipeline helpers.
            for name in ("tee", "mkdir", "chmod"):
                executable = shutil.which(name)
                if executable:
                    (tools / name).symlink_to(executable)
            env = {
                "PATH": str(tools), "HOME": str(root), "GOPATH": str(root / "go"),
                "GOBIN": str(root / "go/bin"), "GOTOOLCHAIN": "local",
                "GITHUB_WORKSPACE": str(root), "RUNNER_TEMP": str(root / "tmp"),
                "CALL_LOG": str(log), "SCAN_STATUS": str(scan_status),
                "INSTALL_STATUS": str(install_status),
            }
            output = []
            status = 0
            for step in selected:
                command = field(step, "run", 8)
                self.assertIsNotNone(command)
                self.assertNotIn("${{", command,
                                 "fixture needs explicit support for expression-bearing run commands")
                self.assertIn(field(step, "shell", 8), (None, "bash"),
                              "fixture models the existing Ubuntu Bash run-step shell")
                self.assertIsNone(field(step, "working-directory", 8),
                                  "scan must run from the repository root")
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
                        self.assertNotIn("${{", value, "unsupported fixture environment expression")
                        self.assertNotIn(key, ("PATH", "CALL_LOG", "SCAN_STATUS", "INSTALL_STATUS"),
                                         "step environment must not defeat tool interception")
                        if key in ("GOBIN", "GOPATH"):
                            self.assertTrue(Path(value).is_relative_to(root),
                                            "fixture tool paths must stay inside its temporary root")
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
                self.assertEqual(call["cwd"], str(root), "tool must run at module root")
            return status, calls, "\n".join(output)

    def test_scan_is_in_existing_strict_required_job(self) -> None:
        self.assertEqual(field(self.job, "name", 4), "Composure checks")
        self.assert_required(self.job.split("    steps:\n", 1)[0], 4)
        for step in self.scan_steps():
            self.assert_required(step, 8)

    def test_repository_go_toolchain_is_set_up_before_scan(self) -> None:
        selected = self.scan_steps()
        setup = [index for index, step in enumerate(self.steps)
                 if (field(step, "uses", 8) or "").startswith("actions/setup-go@")]
        self.assertEqual(len(setup), 1)
        self.assertEqual(field(self.steps[setup[0]], "go-version-file", 10), "go.mod")
        self.assertIsNone(field(self.steps[setup[0]], "go-version", 10),
                          "do not override the repository toolchain with another Go version")
        self.assertRegex((REPO / "go.mod").read_text(), r"(?m)^toolchain go1\.27\.1$")
        for step in selected:
            self.assertLess(setup[0], self.steps.index(step))

    def test_pinned_install_and_successful_whole_module_scan(self) -> None:
        status, calls, output = self.run_commands(self.scan_steps())
        self.assertEqual(status, 0, output)
        installs = [call for call in calls
                    if call["tool"] == "go" and call["args"][:1] == ["install"]]
        self.assertEqual(len(installs), 1, "install govulncheck at an explicit immutable version")
        self.assertRegex(installs[0]["args"][1],
                         rf"^{re.escape(TOOL)}@v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$")
        scans = [call for call in calls if call["tool"] == "govulncheck"]
        self.assertEqual(len(scans), 1, "an install, echo or comment is not a vulnerability scan")
        self.assertIn("./...", scans[0]["args"], "scan all repository packages")
        self.assertLess(calls.index(installs[0]), calls.index(scans[0]))

    def test_vulnerability_and_scanner_errors_fail_the_required_step(self) -> None:
        selected = self.scan_steps()
        for failure in (3, 17):
            with self.subTest(exit_code=failure):
                status, calls, output = self.run_commands(selected, scan_status=failure)
                self.assertTrue(any(call["tool"] == "govulncheck" for call in calls), output)
                self.assertNotEqual(status, 0, "scanner failure was swallowed: " + output)

    def test_install_failure_stops_before_scanning(self) -> None:
        status, calls, output = self.run_commands(self.scan_steps(), install_status=23)
        self.assertNotEqual(status, 0, "tool install failure was swallowed: " + output)
        self.assertTrue(any(call["tool"] == "go" and call["args"][:1] == ["install"]
                            for call in calls), output)
        self.assertFalse(any(call["tool"] == "govulncheck" for call in calls),
                         "failed installation must not use a stale scanner")


class GovulncheckFixtureTests(unittest.TestCase):
    def test_fake_tools_execute_and_detect_swallowed_failures_without_network(self) -> None:
        fixture = CIGovulncheckTests()
        block = """name: Fixture only
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@v0.0.0
          govulncheck ./...
"""
        for scan_status, install_status, expected in ((0, 0, 0), (3, 0, 3),
                                                      (17, 0, 17), (0, 23, 23)):
            with self.subTest(scan_status=scan_status, install_status=install_status):
                status, calls, output = fixture.run_commands(
                    [block], scan_status=scan_status, install_status=install_status,
                )
                self.assertEqual(status, expected, output)
                self.assertEqual([call["tool"] for call in calls],
                                 ["go"] if install_status else ["go", "govulncheck"])
        status, calls, output = fixture.run_commands(
            [block.replace("govulncheck ./...", "govulncheck ./... || true")], scan_status=3,
        )
        self.assertEqual(status, 0, output)
        self.assertEqual(calls[-1]["tool"], "govulncheck")

    def test_reader_does_not_mistake_optional_job_or_comment_for_required_scan(self) -> None:
        source = """jobs:
  checks:
    name: Composure checks
    steps:
      - name: Ordinary checks
        # govulncheck ./... is not executed
        run: echo checks
  optional:
    steps:
      - name: Vulnerabilities
        run: govulncheck ./...
"""
        required = checks_job(source)
        self.assertNotIn("optional:", required)
        self.assertEqual([field(step, "run", 8) for step in steps(required)], ["echo checks"])

    def test_block_and_scalar_commands_are_read_without_neighbor_fields(self) -> None:
        self.assertEqual(field("        run: govulncheck ./...\n", "run", 8), "govulncheck ./...")
        block = """        run: |
          go install golang.org/x/vuln/cmd/govulncheck@v0.0.0
          govulncheck ./...
        env:
          NOT_A_COMMAND: value
"""
        command = field(block, "run", 8)
        self.assertEqual(command.splitlines(), [
            "go install golang.org/x/vuln/cmd/govulncheck@v0.0.0", "govulncheck ./...",
        ])
        self.assertEqual(shlex.split(command.splitlines()[1]), ["govulncheck", "./..."])
