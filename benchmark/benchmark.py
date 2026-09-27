#!/usr/bin/env python3
"""Collect and compare isolated coding-agent benchmark runs."""

from __future__ import annotations

import argparse
import csv
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
from typing import Any, Iterable


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_RUBRIC = Path(__file__).with_name("rubric.json")
SAFE_NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
TOKEN_FIELDS = {
    "input": "input",
    "output": "output",
    "cacheRead": "cache_read",
    "cacheWrite": "cache_write",
    "totalTokens": "total",
}
COST_FIELDS = {
    "input": "input",
    "output": "output",
    "cacheRead": "cache_read",
    "cacheWrite": "cache_write",
    "total": "total",
}


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read JSON from {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a JSON object")
    return value


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def read_events(path: Path) -> list[dict[str, Any]]:
    events: list[dict[str, Any]] = []
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as exc:
        raise ValueError(f"cannot read events from {path}: {exc}") from exc
    for line_number, line in enumerate(lines, start=1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"invalid JSON in {path} at line {line_number}: {exc}") from exc
        if not isinstance(event, dict):
            raise ValueError(f"event in {path} at line {line_number} is not an object")
        events.append(event)
    return events


def summarize_events(events: Iterable[dict[str, Any]]) -> dict[str, Any]:
    tokens = {name: 0 for name in TOKEN_FIELDS.values()}
    cost = {name: 0.0 for name in COST_FIELDS.values()}
    turns = 0
    tool_calls = 0
    tool_errors = 0
    assistant_messages = 0

    for event in events:
        event_type = event.get("type")
        usage: Any = None
        if event_type == "message_end":
            message = event.get("message")
            if isinstance(message, dict) and message.get("role") == "assistant":
                assistant_messages += 1
                usage = message.get("usage")
        elif event_type == "usage":
            usage = event.get("usage")

        if isinstance(usage, dict):
            for source, destination in TOKEN_FIELDS.items():
                value = usage.get(source, 0)
                if isinstance(value, (int, float)) and not isinstance(value, bool):
                    tokens[destination] += int(value)
            usage_cost = usage.get("cost")
            if isinstance(usage_cost, dict):
                for source, destination in COST_FIELDS.items():
                    value = usage_cost.get(source, 0)
                    if isinstance(value, (int, float)) and not isinstance(value, bool):
                        cost[destination] += float(value)

        if event_type == "turn_end":
            turns += 1
        elif event_type == "tool_execution_end":
            tool_calls += 1
            tool_errors += int(event.get("isError") is True)

    return {
        "tokens": tokens,
        "cost": cost,
        "assistant_messages": assistant_messages,
        "turns": turns,
        "tool_calls": tool_calls,
        "tool_errors": tool_errors,
    }


def parse_timestamp(value: Any) -> float | None:
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return float(value) / 1000 if value > 10_000_000_000 else float(value)
    if not isinstance(value, str):
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


def transcript_elapsed(events: Iterable[dict[str, Any]]) -> float | None:
    timestamps: list[float] = []
    for event in events:
        timestamp = parse_timestamp(event.get("timestamp"))
        if timestamp is None and isinstance(event.get("message"), dict):
            timestamp = parse_timestamp(event["message"].get("timestamp"))
        if timestamp is not None:
            timestamps.append(timestamp)
    if len(timestamps) < 2:
        return None
    return round(max(timestamps) - min(timestamps), 3)


def detect_source(events: list[dict[str, Any]]) -> str:
    if any(event.get("type") in {"message_end", "message_update", "agent_settled"} for event in events):
        return "pi-json"
    if any(event.get("type") == "result" for event in events):
        return "claude-stream"
    if any(
        event.get("type") == "assistant"
        and isinstance(event.get("message"), dict)
        and isinstance(event["message"].get("usage"), dict)
        and "input_tokens" in event["message"]["usage"]
        for event in events
    ):
        return "claude-session"
    raise ValueError("cannot detect transcript format; specify --source")


def empty_usage() -> dict[str, Any]:
    return {
        "tokens": {
            "input": 0,
            "output": 0,
            "cache_read": 0,
            "cache_write": 0,
            "reasoning": 0,
            "total": 0,
        },
        "cost": {
            "input": 0.0,
            "output": 0.0,
            "cache_read": 0.0,
            "cache_write": 0.0,
            "total": 0.0,
        },
    }


def claude_tokens(usage: dict[str, Any]) -> dict[str, int]:
    def integer(name: str) -> int:
        value = usage.get(name, 0)
        return int(value) if isinstance(value, (int, float)) and not isinstance(value, bool) else 0

    details = usage.get("output_tokens_details")
    reasoning = details.get("thinking_tokens", 0) if isinstance(details, dict) else 0
    if not isinstance(reasoning, (int, float)) or isinstance(reasoning, bool):
        reasoning = 0
    tokens = {
        "input": integer("input_tokens"),
        "output": integer("output_tokens"),
        "cache_read": integer("cache_read_input_tokens"),
        "cache_write": integer("cache_creation_input_tokens"),
        "reasoning": int(reasoning),
    }
    tokens["total"] = sum(tokens[name] for name in ("input", "output", "cache_read", "cache_write"))
    return tokens


def summarize_claude(events: list[dict[str, Any]], source: str) -> dict[str, Any]:
    summary = empty_usage()
    messages: dict[str, dict[str, Any]] = {}
    for index, event in enumerate(events):
        if event.get("type") != "assistant" or not isinstance(event.get("message"), dict):
            continue
        message = event["message"]
        message_id = message.get("id") or event.get("uuid") or f"entry-{index}"
        messages[str(message_id)] = message

    models: dict[str, int] = {}
    tool_calls = 0
    for message in messages.values():
        usage = message.get("usage")
        if isinstance(usage, dict):
            tokens = claude_tokens(usage)
            for field, value in tokens.items():
                summary["tokens"][field] += value
        model = message.get("model")
        if isinstance(model, str) and model:
            models[model] = models.get(model, 0) + 1
        content = message.get("content")
        if isinstance(content, list):
            tool_calls += sum(
                1 for block in content
                if isinstance(block, dict) and block.get("type") == "tool_use"
            )

    tool_errors = 0
    seen_user_entries: set[str] = set()
    for index, event in enumerate(events):
        if event.get("type") != "user" or not isinstance(event.get("message"), dict):
            continue
        entry_id = str(event.get("uuid") or f"user-{index}")
        if entry_id in seen_user_entries:
            continue
        seen_user_entries.add(entry_id)
        content = event["message"].get("content")
        if isinstance(content, list):
            tool_errors += sum(
                1 for block in content
                if isinstance(block, dict)
                and block.get("type") == "tool_result"
                and block.get("is_error") is True
            )

    cost_source = "unavailable"
    turns = len(messages)
    derived_elapsed = transcript_elapsed(events)
    if source == "claude-stream":
        results = [event for event in events if event.get("type") == "result"]
        if results:
            result = results[-1]
            usage = result.get("usage")
            if isinstance(usage, dict):
                summary["tokens"] = claude_tokens(usage)
            turns_value = result.get("num_turns")
            if isinstance(turns_value, (int, float)) and not isinstance(turns_value, bool):
                turns = int(turns_value)
            cost = result.get("total_cost_usd")
            if isinstance(cost, (int, float)) and not isinstance(cost, bool):
                summary["cost"]["total"] = float(cost)
                cost_source = "claude-reported"
            duration = result.get("duration_ms")
            if isinstance(duration, (int, float)) and not isinstance(duration, bool):
                derived_elapsed = round(float(duration) / 1000, 3)
            model_usage = result.get("modelUsage")
            if isinstance(model_usage, dict) and model_usage:
                models = {str(model): 1 for model in model_usage}

    return {
        **summary,
        "source": source,
        "cost_source": cost_source,
        "models": models,
        "assistant_messages": len(messages),
        "turns": turns,
        "tool_calls": tool_calls,
        "tool_errors": tool_errors,
        "derived_elapsed_seconds": derived_elapsed,
    }


def summarize_transcript(events: list[dict[str, Any]], source: str = "auto") -> dict[str, Any]:
    selected_source = detect_source(events) if source == "auto" else source
    if selected_source == "pi-json":
        summary = summarize_events(events)
        summary["tokens"]["reasoning"] = 0
        summary.update({
            "source": selected_source,
            "cost_source": "pi-reported",
            "models": {},
            "derived_elapsed_seconds": transcript_elapsed(events),
        })
        return summary
    if selected_source in {"claude-session", "claude-stream"}:
        return summarize_claude(events, selected_source)
    raise ValueError(f"unsupported transcript source: {selected_source}")


def run_git(worktree: Path, *arguments: str, env: dict[str, str] | None = None) -> str:
    result = subprocess.run(
        ["git", "-C", str(worktree), *arguments],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise ValueError(f"git {' '.join(arguments)} failed in {worktree}: {detail}")
    return result.stdout


def capture_patch(worktree: Path, baseline: str) -> str:
    """Create a patch against baseline without changing the worktree index.

    A temporary index lets `git add -A` include committed, modified, deleted, and
    untracked files. Ignored files remain excluded.
    """
    worktree = worktree.resolve()
    run_git(worktree, "cat-file", "-e", f"{baseline}^{{commit}}")
    with tempfile.TemporaryDirectory(prefix="benchmark-index-") as directory:
        index = Path(directory) / "index"
        env = dict(os.environ)
        env["GIT_INDEX_FILE"] = str(index)
        run_git(worktree, "read-tree", baseline, env=env)
        run_git(worktree, "add", "-A", env=env)
        return run_git(worktree, "diff", "--cached", "--binary", baseline, "--", env=env)


def parse_check(value: str) -> dict[str, str]:
    if "=" not in value:
        raise ValueError("checks must use NAME=COMMAND")
    name, command = value.split("=", 1)
    if not SAFE_NAME.fullmatch(name) or not command.strip():
        raise ValueError("checks must use a safe non-empty NAME and COMMAND")
    return {"name": name, "command": command}


def run_checks(worktree: Path, checks: list[dict[str, Any]], timeout: float) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    for check in checks:
        name = check.get("name")
        command = check.get("command")
        if not isinstance(name, str) or not isinstance(command, str):
            raise ValueError("each experiment check needs string name and command fields")
        started = time.monotonic()
        timed_out = False
        try:
            result = subprocess.run(
                command,
                cwd=worktree,
                shell=True,
                executable="/bin/bash",
                capture_output=True,
                text=True,
                timeout=timeout,
                check=False,
            )
            exit_code: int | None = result.returncode
            stdout = result.stdout
            stderr = result.stderr
        except subprocess.TimeoutExpired as exc:
            timed_out = True
            exit_code = None
            stdout = exc.stdout.decode() if isinstance(exc.stdout, bytes) else (exc.stdout or "")
            stderr = exc.stderr.decode() if isinstance(exc.stderr, bytes) else (exc.stderr or "")
        results.append({
            "name": name,
            "command": command,
            "passed": exit_code == 0 and not timed_out,
            "exit_code": exit_code,
            "timed_out": timed_out,
            "elapsed_seconds": round(time.monotonic() - started, 3),
            "stdout": stdout,
            "stderr": stderr,
        })
    return results


def score_review(rubric: dict[str, Any], review: dict[str, Any]) -> dict[str, Any]:
    hard_gate_results = review.get("hard_gates")
    criterion_results = review.get("criteria")
    if not isinstance(hard_gate_results, dict) or not isinstance(criterion_results, dict):
        raise ValueError("review must contain hard_gates and criteria objects")

    failed_gates: list[str] = []
    for gate in rubric.get("hard_gates", []):
        gate_id = gate.get("id")
        result = hard_gate_results.get(gate_id)
        if not isinstance(result, dict) or not isinstance(result.get("pass"), bool):
            raise ValueError(f"hard gate {gate_id!r} needs a boolean pass value")
        if not isinstance(result.get("evidence"), str) or not result["evidence"].strip():
            raise ValueError(f"hard gate {gate_id!r} needs evidence")
        if not result["pass"]:
            failed_gates.append(gate_id)

    weighted_score = 0.0
    total_weight = 0.0
    criterion_scores: dict[str, float] = {}
    for criterion in rubric.get("criteria", []):
        criterion_id = criterion.get("id")
        weight = criterion.get("weight")
        result = criterion_results.get(criterion_id)
        if not isinstance(weight, (int, float)) or isinstance(weight, bool) or weight <= 0:
            raise ValueError(f"criterion {criterion_id!r} has an invalid weight")
        if not isinstance(result, dict):
            raise ValueError(f"criterion {criterion_id!r} is missing")
        score = result.get("score")
        if not isinstance(score, (int, float)) or isinstance(score, bool) or not 0 <= score <= 5:
            raise ValueError(f"criterion {criterion_id!r} score must be between 0 and 5")
        if not isinstance(result.get("evidence"), str) or not result["evidence"].strip():
            raise ValueError(f"criterion {criterion_id!r} needs evidence")
        weighted_score += float(score) / 5 * float(weight)
        total_weight += float(weight)
        criterion_scores[criterion_id] = float(score)

    if total_weight <= 0:
        raise ValueError("rubric has no weighted criteria")
    quality_score = weighted_score / total_weight * 100
    return {
        "quality_score": round(quality_score, 2),
        "eligible": not failed_gates,
        "failed_gates": failed_gates,
        "criterion_scores": criterion_scores,
    }


def find_experiment(path: str) -> Path:
    candidate = Path(path)
    if not candidate.is_absolute():
        direct = (Path.cwd() / candidate).resolve()
        under_root = (ROOT / "benchmarks" / candidate).resolve()
        candidate = direct if (direct / "experiment.json").is_file() else under_root
    candidate = candidate.resolve()
    if not (candidate / "experiment.json").is_file():
        raise ValueError(f"experiment not found at {candidate}")
    return candidate


def command_init(args: argparse.Namespace) -> None:
    if not SAFE_NAME.fullmatch(args.name):
        raise ValueError("experiment name may contain letters, numbers, dots, underscores, and hyphens")
    destination = (ROOT / "benchmarks" / args.name).resolve()
    if destination.exists():
        raise ValueError(f"experiment already exists: {destination}")
    ticket = Path(args.ticket).resolve()
    if not ticket.is_file():
        raise ValueError(f"ticket file does not exist: {ticket}")
    baseline = run_git(ROOT, "rev-parse", f"{args.baseline}^{{commit}}").strip()
    checks_by_name = {"repository": parse_check("repository=bash scripts/test")}
    for value in args.check:
        check = parse_check(value)
        checks_by_name[check["name"]] = check
    checks = list(checks_by_name.values())
    destination.mkdir(parents=True)
    shutil.copyfile(ticket, destination / "ticket.md")
    shutil.copyfile(DEFAULT_RUBRIC, destination / "rubric.json")
    write_json(destination / "experiment.json", {
        "schema_version": 1,
        "name": args.name,
        "baseline": baseline,
        "created_at": utc_now(),
        "checks": checks,
    })
    print(destination)


def command_collect(args: argparse.Namespace) -> None:
    experiment = find_experiment(args.experiment)
    if not SAFE_NAME.fullmatch(args.run_id):
        raise ValueError("run ID may contain letters, numbers, dots, underscores, and hyphens")
    run_directory = experiment / "runs" / args.run_id
    if run_directory.exists():
        raise ValueError(f"run already exists: {run_directory}")
    worktree = Path(args.worktree).resolve()
    transcript_path = Path(args.transcript).resolve()
    config = load_json(experiment / "experiment.json")
    baseline = config.get("baseline")
    if not isinstance(baseline, str):
        raise ValueError("experiment baseline is missing")
    events = read_events(transcript_path)
    usage = summarize_transcript(events, args.source)
    elapsed_seconds = args.elapsed_seconds
    elapsed_source = "provided"
    if elapsed_seconds is None:
        elapsed_seconds = usage.get("derived_elapsed_seconds")
        elapsed_source = "transcript"
    if elapsed_seconds is None:
        raise ValueError(
            "transcript has no usable duration; provide --elapsed-seconds"
        )
    patch = capture_patch(worktree, baseline)
    checks = run_checks(worktree, config.get("checks", []), args.check_timeout)
    head = run_git(worktree, "rev-parse", "HEAD").strip()
    merge_base = run_git(worktree, "merge-base", baseline, head).strip()
    if merge_base != baseline:
        raise ValueError(
            f"worktree HEAD {head} does not descend from experiment baseline {baseline}"
        )

    run_directory.mkdir(parents=True)
    shutil.copyfile(transcript_path, run_directory / "transcript.jsonl")
    (run_directory / "solution.patch").write_text(patch, encoding="utf-8")
    write_json(run_directory / "checks.json", checks)
    write_json(run_directory / "run.json", {
        "schema_version": 1,
        "run_id": args.run_id,
        "model": args.model,
        "thinking": args.thinking,
        "baseline": baseline,
        "head": head,
        "baseline_is_ancestor": True,
        "collected_at": utc_now(),
        "agent_elapsed_seconds": elapsed_seconds,
        "elapsed_source": elapsed_source,
        "agent_exit_code": args.exit_code,
        **usage,
    })
    print(run_directory)


def alias_name(index: int) -> str:
    letters = ""
    number = index
    while True:
        number, remainder = divmod(number, 26)
        letters = chr(ord("A") + remainder) + letters
        if number == 0:
            return f"Candidate {letters}"
        number -= 1


def review_template(alias: str, rubric: dict[str, Any]) -> dict[str, Any]:
    return {
        "candidate": alias,
        "reviewer": "",
        "hard_gates": {
            gate["id"]: {"pass": None, "evidence": ""}
            for gate in rubric.get("hard_gates", [])
        },
        "criteria": {
            criterion["id"]: {"score": None, "evidence": ""}
            for criterion in rubric.get("criteria", [])
        },
        "summary": "",
    }


def command_packets(args: argparse.Namespace) -> None:
    experiment = find_experiment(args.experiment)
    runs_directory = experiment / "runs"
    run_directories = sorted(path for path in runs_directory.glob("*") if (path / "run.json").is_file())
    if not run_directories:
        raise ValueError("experiment has no collected runs")
    rubric = load_json(experiment / "rubric.json")
    ticket = (experiment / "ticket.md").read_text(encoding="utf-8")
    packets = experiment / "review-packets"
    packets.mkdir(exist_ok=True)
    alias_path = experiment / "aliases.json"
    aliases = load_json(alias_path) if alias_path.is_file() else {}
    known_runs = {path.name for path in run_directories}
    aliases = {
        alias: run_id for alias, run_id in aliases.items()
        if isinstance(run_id, str) and run_id in known_runs
    }
    run_to_alias = {run_id: alias for alias, run_id in aliases.items()}
    next_alias = 0
    for run_directory in run_directories:
        alias = run_to_alias.get(run_directory.name)
        if alias is None:
            while alias_name(next_alias) in aliases:
                next_alias += 1
            alias = alias_name(next_alias)
            aliases[alias] = run_directory.name
            run_to_alias[run_directory.name] = alias
            next_alias += 1
        try:
            checks = json.loads((run_directory / "checks.json").read_text(encoding="utf-8"))
        except (OSError, UnicodeError, json.JSONDecodeError) as exc:
            raise ValueError(f"cannot read checks for {run_directory.name}: {exc}") from exc
        if not isinstance(checks, list):
            raise ValueError(f"checks for {run_directory.name} must be a JSON array")
        check_lines = []
        for check in checks:
            status = "PASS" if check.get("passed") else "FAIL"
            output = "\n".join(
                part.rstrip() for part in (check.get("stdout", ""), check.get("stderr", ""))
                if isinstance(part, str) and part.strip()
            )
            if len(output) > 12000:
                output = "[earlier output omitted]\n" + output[-12000:]
            check_lines.append(
                f"### {check.get('name')}: {status}\n\n"
                f"Command: `{check.get('command')}`  \n"
                f"Exit: {check.get('exit_code')}  \n"
                f"Elapsed: {check.get('elapsed_seconds')}s\n\n"
                f"```text\n{output}\n```"
            )
        patch = (run_directory / "solution.patch").read_text(encoding="utf-8")
        packet = (
            f"# {alias}\n\n"
            "Review this candidate without looking at the alias mapping, model, cost, or runtime.\n\n"
            "## Ticket\n\n"
            f"{ticket.rstrip()}\n\n"
            "## Automated checks\n\n"
            f"{chr(10).join(check_lines) or 'No checks configured.'}\n\n"
            "## Patch\n\n"
            f"```diff\n{patch.rstrip()}\n```\n"
        )
        (packets / f"{alias.replace(' ', '-').lower()}.md").write_text(packet, encoding="utf-8")
        template_path = packets / f"{alias.replace(' ', '-').lower()}.review.json"
        if not template_path.exists():
            write_json(template_path, review_template(alias, rubric))
    write_json(alias_path, aliases)
    print(packets)


def average_reviews(scored: list[dict[str, Any]]) -> dict[str, Any]:
    eligible = all(item["eligible"] for item in scored)
    failed = sorted({gate for item in scored for gate in item["failed_gates"]})
    criteria = sorted(scored[0]["criterion_scores"])
    return {
        "quality_score": round(sum(item["quality_score"] for item in scored) / len(scored), 2),
        "eligible": eligible,
        "failed_gates": failed,
        "criterion_scores": {
            criterion: round(
                sum(item["criterion_scores"][criterion] for item in scored) / len(scored), 2
            )
            for criterion in criteria
        },
        "review_count": len(scored),
    }


def command_report(args: argparse.Namespace) -> None:
    experiment = find_experiment(args.experiment)
    rubric = load_json(experiment / "rubric.json")
    aliases = load_json(experiment / "aliases.json")
    rows: list[dict[str, Any]] = []
    packets = experiment / "review-packets"
    for alias, run_id in aliases.items():
        run = load_json(experiment / "runs" / run_id / "run.json")
        stem = alias.replace(" ", "-").lower()
        review_paths = sorted(packets.glob(f"{stem}.review*.json"))
        scored_reviews: list[dict[str, Any]] = []
        for review_path in review_paths:
            review = load_json(review_path)
            try:
                scored_reviews.append(score_review(rubric, review))
            except ValueError as exc:
                if args.allow_unreviewed:
                    continue
                raise ValueError(f"invalid review {review_path}: {exc}") from exc
        quality = average_reviews(scored_reviews) if scored_reviews else {
            "quality_score": None,
            "eligible": False,
            "failed_gates": ["unreviewed"],
            "review_count": 0,
        }
        cost_source = run.get("cost_source", "unknown")
        cost_total = run.get("cost", {}).get("total")
        if cost_source == "unavailable":
            cost_total = None
        rows.append({
            "candidate": alias,
            "run_id": run_id,
            "model": run.get("model"),
            "thinking": run.get("thinking"),
            "source": run.get("source", "unknown"),
            "eligible": quality["eligible"],
            "quality_score": quality["quality_score"],
            "reviews": quality["review_count"],
            "failed_gates": ", ".join(quality["failed_gates"]),
            "cost_usd": cost_total,
            "cost_source": cost_source,
            "elapsed_seconds": run.get("agent_elapsed_seconds"),
            "elapsed_source": run.get("elapsed_source", "unknown"),
            "input_tokens": run.get("tokens", {}).get("input"),
            "output_tokens": run.get("tokens", {}).get("output"),
            "cache_read_tokens": run.get("tokens", {}).get("cache_read"),
            "cache_write_tokens": run.get("tokens", {}).get("cache_write"),
            "reasoning_tokens": run.get("tokens", {}).get("reasoning"),
            "total_tokens": run.get("tokens", {}).get("total"),
            "tool_calls": run.get("tool_calls"),
            "tool_errors": run.get("tool_errors"),
            "agent_exit_code": run.get("agent_exit_code"),
        })
    rows.sort(key=lambda row: (
        not row["eligible"],
        -(row["quality_score"] if row["quality_score"] is not None else -1),
        row["cost_usd"] if row["cost_usd"] is not None else float("inf"),
    ))

    headers = list(rows[0]) if rows else []
    with (experiment / "report.csv").open("w", encoding="utf-8", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=headers)
        writer.writeheader()
        writer.writerows(rows)
    markdown_headers = [
        "Candidate", "Model", "Source", "Eligible", "Quality", "Cost USD", "Cost basis", "Time s", "Tokens", "Reviews", "Failed gates"
    ]
    markdown = ["# Benchmark report", "", "| " + " | ".join(markdown_headers) + " |",
                "|" + "|".join(["---"] * len(markdown_headers)) + "|"]
    for row in rows:
        markdown.append("| " + " | ".join(str(value) for value in [
            row["candidate"], row["model"], row["source"], row["eligible"], row["quality_score"],
            row["cost_usd"], row["cost_source"], row["elapsed_seconds"], row["total_tokens"],
            row["reviews"], row["failed_gates"] or "",
        ]) + " |")
    markdown.extend([
        "",
        "Eligible candidates rank before failed or unreviewed candidates. Within each group, quality ranks first and cost breaks ties.",
        "Keep quality, cost, and time visible rather than treating the ordering as a universal winner.",
        "",
    ])
    (experiment / "report.md").write_text("\n".join(markdown), encoding="utf-8")
    print(experiment / "report.md")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)

    init = commands.add_parser("init", help="create an experiment from a ticket")
    init.add_argument("name")
    init.add_argument("--ticket", required=True, help="path to the canonical ticket text")
    init.add_argument("--baseline", default="HEAD", help="Git revision shared by every run")
    init.add_argument(
        "--check",
        action="append",
        default=[],
        help="repeatable NAME=COMMAND evaluator command",
    )
    init.set_defaults(handler=command_init)

    collect = commands.add_parser("collect", help="capture one completed run")
    collect.add_argument("experiment")
    collect.add_argument("--run-id", required=True)
    collect.add_argument("--model", required=True)
    collect.add_argument("--thinking", default="unspecified")
    collect.add_argument("--worktree", required=True)
    collect.add_argument(
        "--events", "--transcript", dest="transcript", required=True,
        help="Pi JSON events or a Claude Code stream/session JSONL file",
    )
    collect.add_argument(
        "--source", choices=("auto", "pi-json", "claude-stream", "claude-session"),
        default="auto", help="transcript format; auto detects by default",
    )
    collect.add_argument(
        "--elapsed-seconds", type=float,
        help="process wall time; otherwise derive it from transcript timestamps or duration",
    )
    collect.add_argument("--exit-code", type=int, default=0)
    collect.add_argument("--check-timeout", type=float, default=900)
    collect.set_defaults(handler=command_collect)

    packets = commands.add_parser("packets", help="create anonymized review packets")
    packets.add_argument("experiment")
    packets.set_defaults(handler=command_packets)

    report = commands.add_parser("report", help="join reviews, quality, usage, cost, and time")
    report.add_argument("experiment")
    report.add_argument("--allow-unreviewed", action="store_true")
    report.set_defaults(handler=command_report)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        args.handler(args)
    except ValueError as exc:
        parser.error(str(exc))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
