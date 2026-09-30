"""Phase 1 orchestration contracts; fixtures do not prove browser acceptance.

Run with: python3 -m unittest discover -s tests -p test_phase1_runner.py -v
"""

from __future__ import annotations

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[1]
PHASE1 = REPO / "scripts" / "test-phase1"
SHARED = REPO / "scripts" / "test"


class Phase1RunnerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="composure phase1 fixture ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "repository with spaces"
        self.scripts = self.root / "scripts"
        self.scripts.mkdir(parents=True)
        # Before the entry point lands, exercise the existing shared runner as
        # a behavioral baseline, rather than failing only on a missing file.
        self.entry = self.scripts / "test-phase1"
        self.entry.write_bytes((PHASE1 if PHASE1.is_file() else SHARED).read_bytes())
        docs = self.root / "docs"
        docs.mkdir()
        for name in ("prd.md", "architecture_plan.md", "work_plan.md"):
            (docs / name).write_text("fixture document\n", encoding="utf-8")
        (self.root / "go.mod").write_text(
            "module example.test/composure\n\ngo 1.27.1\n", encoding="utf-8"
        )
        self.log = self.root / "child calls.txt"
        self.finished = self.root / "shared completed"
        self.write_child("test", """
printf 'shared:start\\n' >> "$CALL_LOG"
sleep 0.05
if [[ "$SHARED_STATUS" != 0 ]]; then exit "$SHARED_STATUS"; fi
printf 'complete\\n' > "$SHARED_FINISHED"
printf 'shared:end\\n' >> "$CALL_LOG"
""")
        self.write_child("test-browser", """
printf 'browser\\n' >> "$CALL_LOG"
if [[ ! -f "$SHARED_FINISHED" ]]; then exit 91; fi
exit "$BROWSER_STATUS"
""")
        tools = self.root / "tools"
        tools.mkdir()
        go = tools / "go"
        go.write_text(
            "#!/bin/bash\n"
            'printf "go:%s\\n" "$*" >> "$CALL_LOG"\n'
            'if [[ "$*" == "$FAIL_GO_COMMAND" ]]; then exit 23; fi\n'
            'if [[ "$*" == "list -json ./..." ]]; then\n'
            '  printf "%s\\n" "$GO_METADATA"\n'
            "fi\n",
            encoding="utf-8",
        )
        go.chmod(0o755)
        self.env = {
            **os.environ,
            "PATH": str(tools) + os.pathsep + os.environ.get("PATH", ""),
            "CALL_LOG": str(self.log),
            "SHARED_FINISHED": str(self.finished),
            "SHARED_STATUS": "0",
            "BROWSER_STATUS": "0",
            "FAIL_GO_COMMAND": "",
            "GO_METADATA": json.dumps({
                "ImportPath": "example.test/composure",
                "TestGoFiles": ["main_test.go"],
            }),
        }

    def write_child(self, name: str, body: str) -> None:
        path = self.scripts / name
        path.write_text("#!/bin/bash\nset -euo pipefail\n" + body, encoding="utf-8")
        # The documented commands use bash, so children need not be executable.
        path.chmod(0o644)

    def run_phase1(self, **env: str) -> subprocess.CompletedProcess[str]:
        unrelated = Path(self.temp.name) / "unrelated working directory"
        unrelated.mkdir(exist_ok=True)
        return subprocess.run(
            ["/bin/bash", str(self.entry)], cwd=unrelated,
            env={**self.env, **env}, text=True, capture_output=True,
            check=False, timeout=15,
        )

    def calls(self) -> list[str]:
        return self.log.read_text(encoding="utf-8").splitlines() if self.log.exists() else []

    @staticmethod
    def output(result: subprocess.CompletedProcess[str]) -> str:
        return result.stdout + result.stderr

    def test_success_runs_shared_then_browser_once_from_unrelated_cwd(self) -> None:
        result = self.run_phase1()
        self.assertEqual(result.returncode, 0, self.output(result))
        self.assertEqual(self.calls(), ["shared:start", "shared:end", "browser"])

    def test_shared_failure_propagates_and_never_starts_browser(self) -> None:
        result = self.run_phase1(SHARED_STATUS="23")
        self.assertEqual(result.returncode, 23, self.output(result))
        self.assertEqual(self.calls(), ["shared:start"])

    def test_browser_failure_propagates_after_shared_success(self) -> None:
        result = self.run_phase1(BROWSER_STATUS="37")
        self.assertEqual(result.returncode, 37, self.output(result))
        self.assertEqual(self.calls(), ["shared:start", "shared:end", "browser"])

    def test_missing_root_module_cannot_pass_as_bootstrap(self) -> None:
        (self.root / "go.mod").unlink()
        result = self.run_phase1()
        self.assertNotEqual(result.returncode, 0, self.output(result))
        self.assertNotIn("browser", self.calls())

    def test_invalid_root_module_cannot_pass_even_if_shared_child_succeeds(self) -> None:
        for contents in ("", "go 1.27.1\n", 'module "unterminated\n', "module\n"):
            with self.subTest(contents=contents):
                (self.root / "go.mod").write_text(contents, encoding="utf-8")
                if self.log.exists():
                    self.log.unlink()
                result = self.run_phase1()
                self.assertNotEqual(result.returncode, 0, self.output(result))
                self.assertNotIn("browser", self.calls())

    def test_missing_shared_runner_fails_without_starting_browser(self) -> None:
        (self.scripts / "test").unlink()
        result = self.run_phase1()
        self.assertNotEqual(result.returncode, 0, self.output(result))
        self.assertNotIn("browser", self.calls())

    def test_missing_browser_runner_fails(self) -> None:
        (self.scripts / "test-browser").unlink()
        result = self.run_phase1()
        self.assertNotEqual(result.returncode, 0, self.output(result))

    def test_real_browser_runner_missing_pinned_python_fails(self) -> None:
        (self.scripts / "test-browser").write_bytes(
            (REPO / "scripts" / "test-browser").read_bytes()
        )
        result = self.run_phase1()
        self.assertNotEqual(result.returncode, 0, self.output(result))
        self.assertIn("install-browser-tests", self.output(result))

    def test_real_browser_runner_missing_axe_fails(self) -> None:
        (self.scripts / "test-browser").write_bytes(
            (REPO / "scripts" / "test-browser").read_bytes()
        )
        python = self.root / ".venv-browser" / "bin" / "python"
        python.parent.mkdir(parents=True)
        python.symlink_to(shutil.which("python3") or os.sys.executable)
        result = self.run_phase1()
        self.assertNotEqual(result.returncode, 0, self.output(result))
        self.assertIn("axe", self.output(result).lower())

    def test_real_shared_go_failure_stops_before_browser(self) -> None:
        (self.scripts / "test").write_bytes(SHARED.read_bytes())
        for command in ("list -json ./...", "build ./...", "vet ./...",
                        "test ./...", "test -race ./..."):
            with self.subTest(command=command):
                if self.log.exists():
                    self.log.unlink()
                result = self.run_phase1(FAIL_GO_COMMAND=command)
                self.assertEqual(result.returncode, 23, self.output(result))
                self.assertEqual(self.calls()[-1], "go:" + command)
                self.assertNotIn("browser", self.calls())

    def test_real_shared_requires_active_go_tests_before_browser(self) -> None:
        (self.scripts / "test").write_bytes(SHARED.read_bytes())
        result = self.run_phase1(GO_METADATA=json.dumps({
            "ImportPath": "example.test/composure", "TestGoFiles": [],
        }))
        self.assertNotEqual(result.returncode, 0, self.output(result))
        self.assertEqual(self.calls(), ["go:list -json ./..."])
        self.assertNotIn("browser", self.calls())
