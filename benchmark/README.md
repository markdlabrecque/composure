# Model benchmark framework

This framework compares coding-agent runs made from the same Git revision. It does not launch agents or create worktrees. It captures their patches and Pi JSON events, runs independent checks, creates blind review packets, and joins quality reviews with cost and elapsed time.

## Quality policy

Treat quality as a gate followed by a score.

A candidate is eligible only when it completes the ticket and has no known critical security, authorization, privacy, data-loss, corruption, or severe regression defect. Eligible candidates receive a weighted score:

- Correctness: 50%
- Regression safety: 15%
- Design and maintainability: 15%
- Tests: 10%
- Scope discipline: 10%

The reviewer must cite evidence for every gate and score. Automated checks are evidence, not the whole judgment. Tests written by the candidate can miss the same misunderstanding as its implementation.

Review patches without model, cost, or timing information. Reveal those fields only in the final report. For close or consequential comparisons, use two reviewers. Copy `candidate-a.review.json` to `candidate-a.review.second-reviewer.json`; the report averages scores and requires every reviewer to pass the hard gates.

Do not collapse the result to quality alone. First discard candidates that fail a hard gate. Compare the remaining candidates by quality, cost per successful run, and elapsed time. Repeat each model at least three times before making a model-level claim.

## Workflow

### 1. Create an experiment

Save the exact ticket text in a file, then run:

```bash
scripts/benchmark init issue-42 \
  --ticket /path/to/issue-42.md \
  --baseline main \
  --check 'acceptance=/path/to/hidden-tests' \
  --check 'repository=bash scripts/test'
```

`init` resolves the baseline to a commit. The default repository check is `bash scripts/test`. A supplied check named `repository` replaces that default; other names add checks.

Keep acceptance tests outside agent worktrees when they must remain hidden. Evaluator commands run only after the agent has stopped.

### 2. Run each agent

Start every run from the experiment's recorded baseline. Give each model the unchanged `ticket.md`, equivalent tools, a fresh session, and the same timeout. Capture Pi's structured output and wall time. One possible runner is:

```bash
start=$(date +%s)
timeout 30m pi --mode json \
  --model "$MODEL" \
  --thinking "$THINKING" \
  "$(cat benchmarks/issue-42/ticket.md)" \
  > "$TMPDIR/events.jsonl" 2> "$TMPDIR/stderr.log"
agent_status=$?
elapsed=$(($(date +%s) - start))
```

Keep wall time external to Pi so it includes provider latency, tool execution, retries, and queueing.

### 3. Collect each result

The worktree may contain committed, staged, modified, deleted, or untracked files. Collection captures all non-ignored changes without altering its real Git index.

```bash
scripts/benchmark collect issue-42 \
  --run-id sonnet-1 \
  --model anthropic/claude-sonnet \
  --thinking high \
  --worktree /path/to/worktree \
  --events "$TMPDIR/events.jsonl" \
  --elapsed-seconds "$elapsed" \
  --exit-code "$agent_status"
```

Usage excludes cumulative streaming snapshots. It sums final assistant-message usage and standalone Pi usage events, including cache activity.

### 4. Judge blind packets

```bash
scripts/benchmark packets issue-42
```

Give reviewers only files under `benchmarks/issue-42/review-packets/`. Keep `aliases.json`, run directories, terminal logs, and model names from them. Fill each review JSON file with scores from 0 through 5 and concrete evidence.

A patch can leak model identity through comments or generated attribution. Remove such attribution before review if it appears.

### 5. Produce the report

```bash
scripts/benchmark report issue-42
```

This writes `report.md` and `report.csv`. Incomplete review templates fail closed. Use `--allow-unreviewed` only for a progress report.

## What the measurements mean

- `cost.total` is Pi's provider-reported cost. Subscription-backed models may report zero. Record a separate list-price estimate if needed rather than overwriting the provider value.
- `elapsed_seconds` is end-to-end agent wall time supplied during collection.
- Token counts are diagnostic. They are not a fair budget across different tokenizers.
- Automated check time is stored separately and excluded from agent elapsed time.
- Parallel runs measure throughput under contention. Randomized sequential runs give cleaner provider-latency comparisons.

Generated run data and reports are ignored by Git. Experiment definitions, ticket text, and rubric files remain trackable.
