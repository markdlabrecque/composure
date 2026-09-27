"""Tests for the model benchmark framework."""

from __future__ import annotations

from contextlib import redirect_stdout
import importlib.util
import io
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[1]
MODULE_PATH = REPO / "benchmark" / "benchmark.py"
spec = importlib.util.spec_from_file_location("composure_benchmark", MODULE_PATH)
benchmark = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(benchmark)


class UsageTests(unittest.TestCase):
    def test_summarize_events_counts_only_final_assistant_usage_and_standalone_usage(self) -> None:
        events = [
            {
                "type": "message_update",
                "usage": {
                    "input": 100,
                    "output": 3,
                    "cacheRead": 5,
                    "cacheWrite": 0,
                    "totalTokens": 108,
                    "cost": {"input": 1, "output": 2, "cacheRead": 3, "cacheWrite": 0, "total": 6},
                },
            },
            {
                "type": "message_end",
                "message": {
                    "role": "assistant",
                    "usage": {
                        "input": 100,
                        "output": 8,
                        "cacheRead": 5,
                        "cacheWrite": 2,
                        "totalTokens": 115,
                        "cost": {"input": 0.1, "output": 0.2, "cacheRead": 0.01, "cacheWrite": 0.02, "total": 0.33},
                    },
                },
            },
            {
                "type": "usage",
                "usage": {
                    "input": 10,
                    "output": 1,
                    "cacheRead": 0,
                    "cacheWrite": 0,
                    "totalTokens": 11,
                    "cost": {"input": 0.01, "output": 0.02, "cacheRead": 0, "cacheWrite": 0, "total": 0.03},
                },
            },
            {"type": "turn_end"},
            {"type": "tool_execution_end", "isError": False},
            {"type": "tool_execution_end", "isError": True},
        ]

        summary = benchmark.summarize_events(events)

        self.assertEqual(summary["tokens"], {
            "input": 110,
            "output": 9,
            "cache_read": 5,
            "cache_write": 2,
            "total": 126,
        })
        self.assertAlmostEqual(summary["cost"]["total"], 0.36)
        self.assertEqual(summary["turns"], 1)
        self.assertEqual(summary["tool_calls"], 2)
        self.assertEqual(summary["tool_errors"], 1)

    def test_read_events_reports_the_bad_line(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            path.write_text('{"type":"agent_start"}\nnot json\n', encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "line 2"):
                benchmark.read_events(path)


class QualityTests(unittest.TestCase):
    def test_score_review_uses_weights_and_requires_all_hard_gates(self) -> None:
        rubric = {
            "hard_gates": [{"id": "complete"}, {"id": "safe"}],
            "criteria": [
                {"id": "correctness", "weight": 70},
                {"id": "maintainability", "weight": 30},
            ],
        }
        review = {
            "candidate": "A",
            "hard_gates": {
                "complete": {"pass": True, "evidence": "Acceptance check passed."},
                "safe": {"pass": False, "evidence": "Unsafe path remains."},
            },
            "criteria": {
                "correctness": {"score": 4, "evidence": "Four cases pass."},
                "maintainability": {"score": 3, "evidence": "One duplication issue."},
            },
        }

        result = benchmark.score_review(rubric, review)

        self.assertAlmostEqual(result["quality_score"], 74.0)
        self.assertFalse(result["eligible"])
        self.assertEqual(result["failed_gates"], ["safe"])

    def test_score_review_rejects_missing_evidence(self) -> None:
        rubric = {
            "hard_gates": [{"id": "complete"}],
            "criteria": [{"id": "correctness", "weight": 100}],
        }
        review = {
            "candidate": "A",
            "hard_gates": {"complete": {"pass": True, "evidence": "ok"}},
            "criteria": {"correctness": {"score": 5, "evidence": ""}},
        }
        with self.assertRaisesRegex(ValueError, "evidence"):
            benchmark.score_review(rubric, review)


class WorkflowTests(unittest.TestCase):
    def test_packets_and_report_keep_identity_out_of_review_packet(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            experiment = Path(directory) / "experiment"
            run = experiment / "runs" / "model-run-1"
            run.mkdir(parents=True)
            (experiment / "experiment.json").write_text(
                json.dumps({"schema_version": 1, "baseline": "abc", "checks": []}),
                encoding="utf-8",
            )
            (experiment / "ticket.md").write_text("Implement the behavior.\n", encoding="utf-8")
            rubric = {
                "hard_gates": [{"id": "complete"}],
                "criteria": [{"id": "correctness", "weight": 100}],
            }
            (experiment / "rubric.json").write_text(json.dumps(rubric), encoding="utf-8")
            (run / "run.json").write_text(json.dumps({
                "model": "secret-model-name",
                "thinking": "high",
                "agent_elapsed_seconds": 12,
                "agent_exit_code": 0,
                "tokens": {"total": 100},
                "cost": {"total": 0.25},
                "tool_calls": 2,
                "tool_errors": 0,
            }), encoding="utf-8")
            (run / "checks.json").write_text(json.dumps([{
                "name": "acceptance",
                "command": "test-command",
                "passed": True,
                "exit_code": 0,
                "elapsed_seconds": 1.2,
                "stdout": "passed\n",
                "stderr": "",
            }]), encoding="utf-8")
            (run / "solution.patch").write_text("diff --git a/a b/a\n", encoding="utf-8")

            with redirect_stdout(io.StringIO()):
                benchmark.command_packets(type("Args", (), {"experiment": str(experiment)})())

            packet = (experiment / "review-packets" / "candidate-a.md").read_text(encoding="utf-8")
            self.assertIn("Implement the behavior.", packet)
            self.assertIn("acceptance: PASS", packet)
            self.assertNotIn("secret-model-name", packet)
            self.assertNotIn("model-run-1", packet)

            review_path = experiment / "review-packets" / "candidate-a.review.json"
            review = json.loads(review_path.read_text(encoding="utf-8"))
            review["reviewer"] = "reviewer-1"
            review["hard_gates"]["complete"] = {"pass": True, "evidence": "Checks pass."}
            review["criteria"]["correctness"] = {"score": 4, "evidence": "Patch matches ticket."}
            review_path.write_text(json.dumps(review), encoding="utf-8")

            with redirect_stdout(io.StringIO()):
                benchmark.command_report(type("Args", (), {
                    "experiment": str(experiment),
                    "allow_unreviewed": False,
                })())

            report = (experiment / "report.md").read_text(encoding="utf-8")
            self.assertIn("secret-model-name", report)
            self.assertIn("80.0", report)
            self.assertIn("0.25", report)

            earlier_run = experiment / "runs" / "aaa-new-run"
            shutil.copytree(run, earlier_run)
            with redirect_stdout(io.StringIO()):
                benchmark.command_packets(type("Args", (), {"experiment": str(experiment)})())
            aliases = json.loads((experiment / "aliases.json").read_text(encoding="utf-8"))
            self.assertEqual(aliases["Candidate A"], "model-run-1")
            self.assertEqual(aliases["Candidate B"], "aaa-new-run")


class PatchTests(unittest.TestCase):
    def test_capture_patch_includes_committed_modified_and_untracked_files(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            subprocess.run(["git", "-C", str(root), "config", "user.email", "test@example.com"], check=True)
            subprocess.run(["git", "-C", str(root), "config", "user.name", "Test"], check=True)
            (root / "tracked.txt").write_text("before\n", encoding="utf-8")
            subprocess.run(["git", "-C", str(root), "add", "tracked.txt"], check=True)
            subprocess.run(["git", "-C", str(root), "commit", "-qm", "baseline"], check=True)
            baseline = subprocess.run(
                ["git", "-C", str(root), "rev-parse", "HEAD"],
                check=True,
                capture_output=True,
                text=True,
            ).stdout.strip()
            (root / "tracked.txt").write_text("after\n", encoding="utf-8")
            (root / "new.txt").write_text("new file\n", encoding="utf-8")

            patch = benchmark.capture_patch(root, baseline)

            self.assertIn("tracked.txt", patch)
            self.assertIn("-before", patch)
            self.assertIn("+after", patch)
            self.assertIn("new.txt", patch)
            self.assertIn("+new file", patch)


if __name__ == "__main__":
    unittest.main()
