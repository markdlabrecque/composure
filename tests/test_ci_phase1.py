"""Bounded CI wiring checks; these do not replace real hosted browser evidence."""

from __future__ import annotations

import ast
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
import textwrap
import unittest
from unittest.mock import patch


REPO = Path(__file__).resolve().parents[1]
PROBE = "COMPOSURE_BROWSER_FAILURE_PROBE"
EXPRESSION = "${{ github.event_name == 'pull_request' && contains(github.event.pull_request.labels.*.name, 'ci-failure-probe') && '1' || '0' }}"
PINNED_SCANNER = r"golang\.org/x/vuln/cmd/govulncheck@v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?"


def scanner_go_arguments(arguments):
    return arguments == ["env", "GOPATH"] or (
        len(arguments) == 2 and arguments[0] == "install"
        and re.fullmatch(PINNED_SCANNER, arguments[1]) is not None
    )


def workflow_steps():
    """Read the existing workflow's scalar/block run steps without a YAML dep.

    actionlint validates YAML separately. Keep this reader scoped to the
    repository's existing six-space step layout.
    """
    source = (REPO / ".github/workflows/ci.yml").read_text()
    steps = []
    for block in re.split(r"^      - ", source, flags=re.MULTILINE)[1:]:
        lines = block.splitlines()
        command = []
        for index, line in enumerate(lines):
            if line.startswith("        run: "):
                scalar = line.partition("run: ")[2]
                if scalar in ("|", "|-", ">", ">-"):
                    for following in lines[index + 1:]:
                        if following and not following.startswith("          "):
                            break
                        command.append(following[10:])
                else:
                    command.append(scalar)
        environment = dict(re.findall(r"^          (COMPOSURE_\w+): (.+)$", block, re.MULTILINE))
        steps.append(("\n".join(command), environment))
    return source, steps


class CIPhase1Tests(unittest.TestCase):
    def test_one_phase_gate_after_pinned_browser_and_ubuntu_libraries(self):
        _, steps = workflow_steps()
        commands = [shlex.split(line) for command, _ in steps
                    for line in command.splitlines() if line.strip()]
        normalized = [tokens[1:] if tokens[:1] == ["bash"] else tokens
                      for tokens in commands]
        self.assertEqual(normalized.count(["scripts/test-phase1"]), 1,
                         "existing required check must invoke the complete phase gate once")
        install = normalized.index(["scripts/install-browser-tests"])
        libraries = normalized.index([".venv-browser/bin/python", "-m", "playwright",
                                      "install-deps", "chromium"])
        gate = normalized.index(["scripts/test-phase1"])
        self.assertLess(install, libraries)
        self.assertLess(libraries, gate)
        self.assertNotIn(["scripts/test"], normalized)
        self.assertNotIn(["scripts/test-browser"], normalized)
        for tokens in normalized:
            if tokens[:1] == ["go"]:
                self.assertTrue(scanner_go_arguments(tokens[1:]),
                                "only pinned scanner installation/GOPATH lookup may bypass the phase gate")

    def test_label_opt_in_reaches_browser_instead_of_go(self):
        _, steps = workflow_steps()
        gates = [environment for command, environment in steps
                 if "scripts/test-phase1" in command]
        self.assertEqual(len(gates), 1, "missing phase gate step")
        self.assertEqual(gates[0].get(PROBE), EXPRESSION,
                         "retain full-label membership and default zero for non-PR events")
        for _, environment in steps:
            self.assertIn(environment.get("COMPOSURE_CI_FAILURE_PROBE", "0"), ("0", "'0'", '"0"'),
                          "Go probe must not intercept the browser failure demonstration")

    def test_existing_required_check_protections_remain(self):
        source, _ = workflow_steps()
        for contract in (
            "types: [opened, synchronize, reopened, labeled, unlabeled]",
            "    branches:\n      - main", "  workflow_dispatch:",
            "permissions:\n  contents: read", "    name: Composure checks",
            "    runs-on: ubuntu-latest", "    timeout-minutes: 15",
            "  group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}",
            "  cancel-in-progress: true",
        ):
            self.assertIn(contract, source)
        actions = re.findall(r"uses: (\S+)", source)
        self.assertEqual(actions, [
            "actions/checkout@11d5960a326750d5838078e36cf38b85af677262",
            "actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff",
        ])
        self.assertEqual(len(re.findall(r"^  \w+:$", source.split("jobs:\n", 1)[1], re.MULTILINE)), 1)

    def test_workflow_commands_propagate_browser_probe_and_failure(self):
        _, steps = workflow_steps()
        with tempfile.TemporaryDirectory(prefix="composure CI fixture ") as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / ".venv-browser/bin").mkdir(parents=True)
            (root / "go.mod").write_text("module example.test/ci\n")
            (root / "scripts/test-phase1").write_bytes((REPO / "scripts/test-phase1").read_bytes())
            log = root / "calls"
            tools = root / "tools"
            tools.mkdir()
            # No real Go or scanner is reachable through the fixture PATH.
            (tools / "bash").symlink_to("/bin/bash")
            (tools / "python3").symlink_to(sys.executable)
            fake = "#!" + sys.executable + "\n" + textwrap.dedent('''\
                import os
                from pathlib import Path
                import re
                import shutil
                import sys

                name = Path(sys.argv[0]).name
                args = sys.argv[1:]
                if name == "go":
                    pinned = os.environ["PINNED_SCANNER"]
                    if args != ["env", "GOPATH"] and not (
                        len(args) == 2 and args[0] == "install"
                        and re.fullmatch(pinned, args[1])
                    ):
                        raise SystemExit("unexpected direct Go invocation: " + repr(args))
                    with open(os.environ["CALL_LOG"], "a") as log:
                        log.write("scanner-go:" + " ".join(args) + "\\n")
                    if args == ["env", "GOPATH"]:
                        print(os.environ["GOPATH"])
                    else:
                        target = Path(os.environ["GOPATH"]) / "bin/govulncheck"
                        target.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copyfile(Path(sys.argv[0]).with_name("govulncheck"), target)
                        target.chmod(0o755)
                else:
                    if args != ["./..."]:
                        raise SystemExit("unexpected scanner invocation: " + repr(args))
                    with open(os.environ["CALL_LOG"], "a") as log:
                        log.write("scanner:./...\\n")
                ''')
            for name in ("go", "govulncheck"):
                path = tools / name
                path.write_text(fake)
                path.chmod(0o755)
            stubs = {
                "scripts/install-browser-tests": 'printf "install\\n" >> "$CALL_LOG"\n',
                ".venv-browser/bin/python": 'printf "deps:%s\\n" "$*" >> "$CALL_LOG"\n',
                "scripts/test": 'printf "go:%s\\n" "${COMPOSURE_CI_FAILURE_PROBE:-0}" >> "$CALL_LOG"\n'
                                'test "${COMPOSURE_CI_FAILURE_PROBE:-0}" != 1\n',
                "scripts/test-browser": 'printf "browser:%s\\n" "${COMPOSURE_BROWSER_FAILURE_PROBE:-0}" >> "$CALL_LOG"\n'
                                        'if [[ "${COMPOSURE_BROWSER_FAILURE_PROBE:-0}" == 1 ]]; then exit 37; fi\n',
            }
            for name, body in stubs.items():
                path = root / name
                path.write_text("#!/bin/bash\nset -euo pipefail\n" + body)
                path.chmod(0o755)
            # Also exercise the new interception before production adds a scanner.
            install = "go install golang.org/x/vuln/cmd/govulncheck@v0.0.0"
            variants = {
                "workflow": steps,
                "fake scanner on PATH": [(install + "\ngovulncheck ./...", {})] + steps,
                "fake installed scanner": [(install, {}),
                                           ('"$(go env GOPATH)/bin/govulncheck" ./...', {})] + steps,
            }
            for variant, commands in variants.items():
                for probe in ("0", "1"):
                    with self.subTest(variant=variant, probe=probe):
                        log.unlink(missing_ok=True)
                        status = 0
                        for command, environment in commands:
                            if not command:
                                continue
                            env = {"PATH": str(tools), "HOME": str(root),
                                   "GOPATH": str(root / "go"), "PINNED_SCANNER": PINNED_SCANNER,
                                   "CALL_LOG": str(log), PROBE: "0", "COMPOSURE_CI_FAILURE_PROBE": "0"}
                            for key, value in environment.items():
                                # Exact expression semantics are checked separately above.
                                env[key] = probe if value == EXPRESSION else value.strip("'\"")
                            result = subprocess.run(["/bin/bash", "-e", "-o", "pipefail", "-c", command],
                                                    cwd=root, env=env, capture_output=True, text=True, timeout=15)
                            status = result.returncode
                            if status:
                                break
                        self.assertEqual(status, 37 if probe == "1" else 0,
                                         result.stdout + result.stderr)
                        calls = log.read_text().splitlines()
                        core = []
                        for call in calls:
                            if call.startswith("scanner-go:"):
                                self.assertTrue(scanner_go_arguments(shlex.split(call.partition(":")[2])), call)
                            elif call == "scanner:./...":
                                continue
                            else:
                                core.append(call)
                        self.assertEqual(core, [
                            "install", "deps:-m playwright install-deps chromium", "go:0", "browser:" + probe,
                        ])
                        if variant != "workflow":
                            self.assertIn("scanner-go:" + install.removeprefix("go "), calls)
                            self.assertIn("scanner:./...", calls)

            # Unauthorized suite commands and mutable/unrelated installs stay rejected.
            for arguments in (["test", "./..."], ["test", "-race", "./..."],
                              ["build", "./..."], ["vet", "./..."], ["env", "GOBIN"],
                              ["install", "golang.org/x/vuln/cmd/govulncheck@latest"],
                              ["install", "example.test/tool@v1.2.3"]):
                with self.subTest(forbidden_go=arguments):
                    self.assertFalse(scanner_go_arguments(arguments))
                    result = subprocess.run([str(tools / "go"), *arguments], cwd=root,
                                            env=env, capture_output=True, text=True, timeout=15)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("unexpected direct Go invocation", result.stderr)


class BrowserProbeContractTests(unittest.TestCase):
    def test_exact_opt_in_inverts_existing_real_journey_expectation(self):
        tree = ast.parse((REPO / "tests/browser/test_page_journey.py").read_text())
        journey = next(node for node in ast.walk(tree)
                       if isinstance(node, ast.FunctionDef) and node.name == "journey")
        guards = [node for node in ast.walk(journey) if isinstance(node, ast.If)
                  and any(isinstance(child, ast.Constant) and child.value == PROBE
                          for child in ast.walk(node.test))]
        self.assertEqual(len(guards), 1, "probe must invert a real DOM expectation inside the existing journey")
        guard = guards[0]
        condition = compile(ast.Expression(guard.test), "browser probe guard", "eval")
        for value in (None, "", "0", "1", "true", "01", "2"):
            with self.subTest(value=value), patch.dict(os.environ, {}, clear=True):
                if value is not None:
                    os.environ[PROBE] = value
                self.assertEqual(bool(eval(condition, {"os": os})), value == "1")
        branches = []
        for branch in (guard.body, guard.orelse):
            expectations = [node for statement in branch for node in ast.walk(statement)
                            if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
                            and isinstance(node.func.value, ast.Call)
                            and isinstance(node.func.value.func, ast.Attribute)
                            and node.func.value.func.attr == "expect"]
            self.assertEqual(len(expectations), 1, "both paths need the same real Playwright DOM expectation")
            branches.append(expectations[0])
            self.assertFalse(any(isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
                                 and node.func.attr == "fail" for statement in branch for node in ast.walk(statement)))
        inverted, normal = branches
        self.assertEqual(ast.dump(inverted.func.value), ast.dump(normal.func.value))
        self.assertEqual([ast.dump(arg) for arg in inverted.args], [ast.dump(arg) for arg in normal.args])
        self.assertEqual(inverted.func.attr, "not_" + normal.func.attr,
                         "opt-in must negate the preserved normal success assertion")
