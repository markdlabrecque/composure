"""Behavioral checks for the shared scripts/test entry point.

Run from the repository root with: python3 -m unittest discover -s tests
"""

from __future__ import annotations

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[1]
RUNNER = REPO / "scripts" / "test"
REQUIRED_DOCS = ("prd.md", "architecture_plan.md", "work_plan.md")


class RunnerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="composure runner fixture ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "repository with spaces"
        self.root.mkdir()

    def make_repo(self, *, examples: dict[str, str] | None = None) -> Path:
        (self.root / "scripts").mkdir(exist_ok=True)
        (self.root / "scripts" / "test").write_bytes(RUNNER.read_bytes())
        (self.root / "scripts" / "test").chmod(0o755)
        docs = self.root / "docs"
        docs.mkdir(exist_ok=True)
        for name in REQUIRED_DOCS:
            shutil.copyfile(REPO / "docs" / name, docs / name)
        if examples is not None:
            for relative, contents in examples.items():
                target = self.root / "docs" / "phase1" / "examples" / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(contents, encoding="utf-8")
        return self.root

    def run_runner(self, root: Path | None = None, *, cwd: Path | None = None,
                   env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
        repo = root or self.root
        return subprocess.run(
            ["/bin/bash", str(repo / "scripts" / "test")],
            cwd=cwd or Path(tempfile.gettempdir()),
            env={**os.environ, **(env or {})},
            text=True,
            capture_output=True,
            check=False,
        )

    @staticmethod
    def output(result: subprocess.CompletedProcess[str]) -> str:
        return result.stdout + result.stderr

    def python_only_path(self, *, go: Path | None = None) -> str:
        """Build a controlled PATH retaining python3 and optionally fake go."""
        bindir = self.root / "tools"
        bindir.mkdir(exist_ok=True)
        python = bindir / "python3"
        if not python.exists():
            python.symlink_to(shutil.which("python3") or os.path.realpath(os.sys.executable))
        if go is not None:
            go_link = bindir / "go"
            if go_link.exists() or go_link.is_symlink():
                go_link.unlink()
            go_link.symlink_to(go)
        return str(bindir)

    def fake_go(self) -> Path:
        bindir = self.root / "fake-bin"
        bindir.mkdir(exist_ok=True)
        go = bindir / "go"
        go.write_text(
            "#!/bin/sh\n"
            "printf '%s\\n' \"$*\" >> \"$GO_CALL_LOG\"\n"
            "if [ \"$*\" = \"$FAIL_GO_COMMAND\" ]; then exit 23; fi\n"
            "if [ \"$*\" = \"list -json ./...\" ]; then\n"
            "  printf '%s\\n' \"$FAKE_GO_LIST_OUTPUT\"\n"
            "fi\n"
            "exit 0\n",
            encoding="utf-8",
        )
        go.chmod(0o755)
        return go

    @staticmethod
    def package_metadata(*, test_go_files: list[str] | None = None,
                         x_test_go_files: list[str] | None = None) -> str:
        import json

        return json.dumps({
            "ImportPath": "example.test/composure",
            "Name": "main",
            "GoFiles": ["main.go"],
            "TestGoFiles": test_go_files or [],
            "XTestGoFiles": x_test_go_files or [],
        })

    def add_go_application(self, *, test_setup: bool = True) -> None:
        (self.root / "go.mod").write_text("module example.test/composure\n\ngo 1.23\n", encoding="utf-8")
        (self.root / "main.go").write_text("package main\nfunc main() {}\n", encoding="utf-8")
        if test_setup:
            (self.root / "main_test.go").write_text(
                "package main\nimport \"testing\"\nfunc TestExample(t *testing.T) {}\n",
                encoding="utf-8",
            )

    def test_bootstrap_checks_documents_and_optional_examples(self) -> None:
        root = self.make_repo(examples={"nested/good.json": '{"name":"Page"}\n'})
        result = self.run_runner(root)
        self.assertEqual(result.returncode, 0, self.output(result))
        self.assertIn("Bootstrap", self.output(result))
        self.assertNotIn("go build", self.output(result))

    def test_bootstrap_allows_no_examples_yet(self) -> None:
        result = self.run_runner(self.make_repo())
        self.assertEqual(result.returncode, 0, self.output(result))
        self.assertIn("Bootstrap", self.output(result))

    def test_runner_finds_repository_from_its_own_path_and_handles_spaces(self) -> None:
        root = self.make_repo()
        unrelated = Path(self.temp.name) / "elsewhere"
        unrelated.mkdir()
        result = self.run_runner(root, cwd=unrelated)
        self.assertEqual(result.returncode, 0, self.output(result))

    def test_missing_or_empty_required_document_fails_with_filename(self) -> None:
        for missing in REQUIRED_DOCS:
            with self.subTest(missing=missing):
                root = self.make_repo()
                (root / "docs" / missing).unlink()
                result = self.run_runner(root)
                output = self.output(result)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(missing, output)
                self.assertNotIn("not implemented", output.lower())

        for empty in REQUIRED_DOCS:
            with self.subTest(empty=empty):
                root = self.make_repo()
                (root / "docs" / empty).write_text("  \n", encoding="utf-8")
                result = self.run_runner(root)
                output = self.output(result)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(empty, output)
                self.assertNotIn("not implemented", output.lower())

    def test_invalid_example_json_fails_with_path(self) -> None:
        root = self.make_repo(examples={"nested/broken.json": '{"name":\n'})
        result = self.run_runner(root)
        output = self.output(result)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("broken.json", output)
        self.assertNotIn("not implemented", output.lower())

    def test_go_source_without_module_is_rejected(self) -> None:
        root = self.make_repo()
        (root / "cmd").mkdir()
        (root / "cmd" / "main.go").write_text("package main\n", encoding="utf-8")
        result = self.run_runner(root)
        output = self.output(result)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("go.mod", output)
        self.assertNotIn("not implemented", output.lower())

    def test_go_module_without_test_setup_is_rejected(self) -> None:
        root = self.make_repo()
        self.add_go_application(test_setup=False)
        go = self.fake_go()
        log = self.root / "go calls.txt"
        result = self.run_runner(root, env={
            "PATH": self.python_only_path(go=go),
            "GO_CALL_LOG": str(log),
            "FAIL_GO_COMMAND": "",
            "FAKE_GO_LIST_OUTPUT": self.package_metadata(),
        })
        output = self.output(result)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("test", output.lower())
        self.assertNotIn("Go application checks", output)
        calls = log.read_text(encoding="utf-8").splitlines() if log.exists() else []
        self.assertEqual(calls, ["list -json ./..."])

    def test_excluded_or_nested_go_tests_do_not_satisfy_root_test_setup(self) -> None:
        excluded_tests = {
            "vendor dependency tests": "vendor/example/dependency_test.go",
            "nested module tests": "nested/dependency_test.go",
            "testdata tests": "testdata/fixture_test.go",
            "dot directory tests": ".hidden/fixture_test.go",
            "underscore directory tests": "_hidden/fixture_test.go",
            "ignored suffix tests": "_ignored_test.go",
            "build-tag-ineligible tests": "tagged_test.go",
        }
        for label, relative_test in excluded_tests.items():
            with self.subTest(location=label):
                root = self.make_repo()
                (root / "go.mod").write_text(
                    "module example.test/composure\n\ngo 1.23\n", encoding="utf-8"
                )
                (root / "main.go").write_text("package main\nfunc main() {}\n", encoding="utf-8")
                test_path = root / relative_test
                test_path.parent.mkdir(parents=True, exist_ok=True)
                test_path.write_text(
                    "package fixture\nimport \"testing\"\n"
                    "func TestFixture(t *testing.T) {}\n",
                    encoding="utf-8",
                )
                if label == "build-tag-ineligible tests":
                    test_path.write_text(
                        "//go:build never_enabled\n\npackage main\n"
                        "import \"testing\"\nfunc TestFixture(t *testing.T) {}\n",
                        encoding="utf-8",
                    )
                if label == "nested module tests":
                    (test_path.parent / "go.mod").write_text(
                        "module example.test/nested\n\ngo 1.23\n", encoding="utf-8"
                    )

                go = self.fake_go()
                log = self.root / "go calls.txt"
                if log.exists():
                    log.unlink()
                env = {
                    "PATH": self.python_only_path(go=go),
                    "GO_CALL_LOG": str(log),
                    "FAIL_GO_COMMAND": "",
                    "FAKE_GO_LIST_OUTPUT": self.package_metadata(),
                }
                result = self.run_runner(root, env=env)
                output = self.output(result)
                self.assertNotEqual(result.returncode, 0, output)
                self.assertIn("test", output.lower())
                self.assertNotIn("Go application checks", output)
                calls = log.read_text(encoding="utf-8").splitlines() if log.exists() else []
                self.assertEqual(
                    calls,
                    ["list -json ./..."],
                    "runner should query package metadata, then reject before build/vet/test/race",
                )

    def test_application_mode_runs_all_go_checks_without_needing_real_go(self) -> None:
        root = self.make_repo()
        self.add_go_application()
        go = self.fake_go()
        log = self.root / "go calls.txt"
        env = {
            "PATH": self.python_only_path(go=go),
            "GO_CALL_LOG": str(log),
            "FAIL_GO_COMMAND": "",
            "FAKE_GO_LIST_OUTPUT": self.package_metadata(test_go_files=["main_test.go"]),
        }
        result = self.run_runner(root, env=env)
        self.assertEqual(result.returncode, 0, self.output(result))
        self.assertIn("Go", self.output(result))
        self.assertEqual(
            log.read_text(encoding="utf-8").splitlines(),
            ["list -json ./...", "build ./...", "vet ./...", "test ./...", "test -race ./..."],
        )

    def test_each_go_check_failure_reaches_caller_and_stops_later_checks(self) -> None:
        expected = ["list -json ./...", "build ./...", "vet ./...", "test ./...", "test -race ./..."]
        for failed in expected:
            with self.subTest(failed=failed):
                root = self.make_repo()
                self.add_go_application()
                go = self.fake_go()
                log = self.root / "go calls.txt"
                if log.exists():
                    log.unlink()
                result = self.run_runner(root, env={
                    "PATH": self.python_only_path(go=go),
                    "GO_CALL_LOG": str(log),
                    "FAIL_GO_COMMAND": failed,
                    "FAKE_GO_LIST_OUTPUT": self.package_metadata(test_go_files=["main_test.go"]),
                })
                self.assertEqual(result.returncode, 23, self.output(result))
                self.assertEqual(log.read_text(encoding="utf-8").splitlines(), expected[:expected.index(failed) + 1])

    def test_missing_go_tool_fails_in_application_mode(self) -> None:
        root = self.make_repo()
        self.add_go_application()
        result = self.run_runner(root, env={"PATH": self.python_only_path()})
        output = self.output(result)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("go", output.lower())
        self.assertNotIn("not implemented", output.lower())

    def test_external_test_package_metadata_satisfies_root_test_setup(self) -> None:
        root = self.make_repo()
        (root / "go.mod").write_text("module example.test/composure\n\ngo 1.23\n", encoding="utf-8")
        (root / "main.go").write_text("package main\nfunc main() {}\n", encoding="utf-8")
        (root / "external_test.go").write_text("package main_test\n", encoding="utf-8")
        go = self.fake_go()
        log = self.root / "go calls.txt"
        result = self.run_runner(root, env={
            "PATH": self.python_only_path(go=go),
            "GO_CALL_LOG": str(log),
            "FAIL_GO_COMMAND": "",
            "FAKE_GO_LIST_OUTPUT": self.package_metadata(x_test_go_files=["external_test.go"]),
        })
        self.assertEqual(result.returncode, 0, self.output(result))
        self.assertIn("Go application checks", self.output(result))
        self.assertEqual(
            log.read_text(encoding="utf-8").splitlines(),
            ["list -json ./...", "build ./...", "vet ./...", "test ./...", "test -race ./..."],
        )

    def test_go_list_failure_propagates_before_build_checks(self) -> None:
        root = self.make_repo()
        self.add_go_application()
        go = self.fake_go()
        log = self.root / "go calls.txt"
        result = self.run_runner(root, env={
            "PATH": self.python_only_path(go=go),
            "GO_CALL_LOG": str(log),
            "FAIL_GO_COMMAND": "list -json ./...",
            "FAKE_GO_LIST_OUTPUT": self.package_metadata(test_go_files=["main_test.go"]),
        })
        self.assertEqual(result.returncode, 23, self.output(result))
        self.assertEqual(log.read_text(encoding="utf-8").splitlines(), ["list -json ./..."])
        self.assertNotIn("Go application checks", self.output(result))

    def test_malformed_go_list_metadata_fails_before_build_checks(self) -> None:
        root = self.make_repo()
        self.add_go_application()
        go = self.fake_go()
        log = self.root / "go calls.txt"
        result = self.run_runner(root, env={
            "PATH": self.python_only_path(go=go),
            "GO_CALL_LOG": str(log),
            "FAIL_GO_COMMAND": "",
            "FAKE_GO_LIST_OUTPUT": "{ malformed metadata",
        })
        output = self.output(result)
        self.assertNotEqual(result.returncode, 0, output)
        self.assertIn("json", output.lower())
        self.assertNotIn("Go application checks", output)
        self.assertEqual(log.read_text(encoding="utf-8").splitlines(), ["list -json ./..."])

    def test_missing_python_tool_fails_in_bootstrap_mode(self) -> None:
        root = self.make_repo()
        result = self.run_runner(root, env={"PATH": str(self.root / "empty-path")})
        output = self.output(result)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("python", output.lower())
        self.assertNotIn("not implemented", output.lower())


if __name__ == "__main__":
    unittest.main()
