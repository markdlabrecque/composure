# Codex orchestration

Codex is the exclusive agent harness for Composure. This applies to the parent coordinator, each top-level ticket orchestrator and every role subagent. Ticket worktrees are plain Git worktrees; Codex owns agent sessions and role dispatch. This protocol overrides runtime, transport, profile and managed-run requirements in the shared `subagent-tdd-pipeline` skill. Keep its TDD and independent-review policy as specified below. Pi sessions, Pi role dispatch and the Pi pipelines extension are outside this project's workflow.

`AGENTS.md` owns issue selection, implementor routing, integration grants and publication safeguards. `docs/work_plan.md` and the phase plans own dependencies, lane ownership and integration order. This file owns Codex launch, role dispatch, evidence and recovery. Shared skills remain available to other projects; their runtime instructions do not override this protocol. Use `ORCHESTRATOR=none` for this project's worktree creation and provisioning. Session ownership is recorded by Codex IDs, without an external workspace or terminal manager. Ticket sessions run in YOLO mode. In T3 Code verify `full-access` on both the thread and provider session; T3 maps it to Codex approval policy `never` and sandbox `danger-full-access`. Native role children inherit that runtime permission boundary. Existing approval-based threads require an explicitly authorized runtime change, followed by same-session resumption and reconciliation of any interrupted turn. Runtime access does not grant gate or publication authority.

## Start or attach

1. Select an explicit open, actionable issue and verify its prerequisites are merged into the phase branch, summarized and closed. Assign it to `markdlabrecque`. Prepare a handoff naming scope, contracts, file ownership, route, role models and reasoning efforts, commands, review cap and integration authority. Resolve product questions before dispatch.
2. Set the main checkout's `BASE_BRANCH` to the phase integration branch and invoke `create-worktree` with `ORCHESTRATOR=none`. Verify the worktree path and branch. Reuse the matching worktree and owned ticket session when present. Preserve user focus and unrelated work. The shared creation script creates the Git branch/worktree and runs a configured project provision hook, or conditional default provisioning. With no setup hook, `.ddev/`, `composer.json` or Git-hook directory, this project needs no provisioning beyond the checkout. `ORCHESTRATOR=none` creates plain Git worktrees. With `T3_THREAD_CREATE=auto` configured and a T3-hosted caller, the shared creation script also creates or reuses a persisted idle Codex thread for the exact worktree. It sends no agent turn or handoff; `subagent-tdd-pipeline` owns initialization.
3. Keep exactly one independent top-level Codex orchestrator using `gpt-6.1-sol` per ticket. The pipeline selects and records its supported reasoning effort based on the ticket scope and risk, then applies the coordinator selection when sending initialization. Select the launch route from the parent's host:

   - **From T3 Code:** reuse the dedicated top-level thread returned by `create-worktree`, or explicitly run its `create-t3-thread.py` helper for a previously prepared worktree. Verify the exact worktree path, branch and returned thread ID. Thread creation uses a provisional Codex selection because T3 requires one; the pipeline selects the coordinator model and reasoning effort before initialization. Follow the shared pipeline skill's `t3-initialization.md`: write a durable initialization script from the approved handoff, record its hash, and send it through `initialize-t3-thread.py` with the selected model and effort. Verify accepted delivery and the first task action in the same thread. The parent owns dependencies and integration, while native role subagents remain inside the ticket thread. If the official CLI or orchestration API is unavailable, report that blocker; a terminal session or native subagent does not substitute for the worktree's T3 thread.
   - **Outside T3 Code:** launch an independent Codex session directly in the verified ticket worktree using an owned interactive terminal or supported top-level Codex session launcher. The local CLI interface is:

   ```sh
   codex --yolo -C WORKTREE -m MODEL -c 'model_reasoning_effort="EFFORT"' "Read HANDOFF_PATH and CHECKPOINT_PATH before acting."
   ```

   Replace all placeholders with the handoff's values. For the CLI route, record the actual session ID and process or host handle and preserve any existing terminal occupant. For either route, confirm cwd, Codex identity, accepted handoff delivery and the first task action before reporting the ticket started. The ticket orchestrator must have native Codex collaboration tools. If top-level launch or those tools are unavailable, report the blocker rather than substituting another harness. A role subagent is never promoted to ticket orchestrator.
4. Store the handoff, checkpoint, append-only event log and role evidence outside the candidate worktree in a durable ticket-specific directory, for example `~/.codex/runtime/ticket-sessions/composure-42/`. Record the launch host, T3 Code thread ID when applicable, actual Codex session ID, initialization script path/hash, accepted initialization command/message IDs and evidence paths. Historical evidence stays at its recorded location, including older `.pi` paths; these are archives, not launch configuration.
5. Initialize the checkpoint before dispatch. Read existing records and reconcile any prior manual or managed workflow first. Confirm there is no other live coordinator or active role attempt. Existing evidence and review counts survive the harness transition; it grants no additional publication authority.

## Dispatch roles

The ticket orchestrator plans, relays handoffs and verifies evidence. Each role is an isolated Codex child with fresh context and explicit ownership. Roles never delegate. Dispatch one active role per ticket through native `spawn_agent`, with `fork_turns="none"`; record the returned agent ID and a separate attempt ID. Include the worktree path, this protocol, `AGENTS.md`, specification, selected route, candidate/base, owned files, prior evidence, commands, integration grant and review round/cap in every handoff. Children verify cwd before writing. Tell writing roles they share the codebase and must preserve others' changes.

The ticket orchestrator uses `gpt-6.1-sol`; it coordinates its own role subagents without implementing their work. Use these role selections, recording model and reasoning effort before dispatch:

| Logical role | Codex dispatch | Model and reasoning | Responsibility |
| --- | --- | --- | --- |
| test-writer | `agent_type="test_writer"` | Built-in `gpt-5.6-sol`, `low` | Initial failing tests only; confirm RED for the specified missing behavior. |
| implementor | `agent_type="worker"`, explicitly assigned only the implementor role | `AGENTS.md` issue routing and selected supported effort, passed explicitly as `model` and `reasoning_effort` | Production implementation or validation deliverable; preserve writer tests and reach focused, compile and cheap GREEN. |
| reviewer | `agent_type="reviewer"` | Built-in `gpt-5.6-sol`, `low` | Independent read-only review and authoritative gate after provisional approval. |
| reporter | `agent_type="reporter"` | Built-in `gpt-5.6-luna`, `low` | Verified tracker reporting and explicitly authorized publication and cleanup. |

The generic worker dispatch for the implementor is deliberate. The built-in Codex implementor fixes model and reasoning defaults, so it cannot honor this project's issue routing. The handoff supplies the implementor duties, boundaries and selected values; the worker performs no other role. If a required model, profile or dispatch option is unavailable, stop before dispatch and report the mismatch. Do not silently substitute defaults. Verify available role metadata on launch rather than assuming it matches this table.

Code tickets use test-writer, implementor, reviewer, then reporter. Documentation, research and verification-only tickets use implementor, independent reviewer, then project-required reporter, with source and document checks instead of invented RED tests.

- Only the writer authors initial tests, and only the implementor writes production code. A writer blocked by a missing production declaration reports it. Disputed tests return to the orchestrator for resolution; preserve their hashes and record any explicitly authorized changes.
- The implementor must pass focused checks, compilation and relevant cheap regressions before review. A relevant failure blocks handoff.
- The reviewer inspects the diff and evidence, runs cheap adversarial checks targeting the handoff's suspected weaknesses, and returns must-fix findings through the orchestrator. No unresolved must-fix finding permits approval. The default cap is two logical review rounds across corrections and replacement agents; further rounds require an explicit recorded override.
- Provisional approval freezes the candidate/base. Obtain the parent's exact integration grant before the authoritative gate. The reviewer records one gate intent and runs the required full gate once on that candidate. A failure returns through correction and renewed review; an unknown outcome requires reconciliation. Neither permits a silent retry. A changed candidate/base invalidates applicable approval, gate evidence and integration grants.
- Writing roles save verdicts and command logs in their assigned evidence directories before returning. The read-only reviewer returns its inspected candidate/base, verdict, actual commands, exit results and output to the orchestrator, which saves that evidence durably before accepting the result. Arrange gate output capture before dispatch; retained output must identify the actual start and exit, not merely a claimed pass. Children never edit the orchestrator checkpoint. An idle agent, successful process exit or prose claim alone proves no stage completed. Verify the artifacts before consuming a result once.
- Independent top-level ticket sessions may run concurrently only with parallel authorization and non-conflicting files and contracts. The parent serializes integration grants, base updates, gates and publication in plan order. Role children run through native Codex collaboration tools.

## Checkpoints and effects

Write an intent before each dispatch, gate or remote mutation. After observing the actual outcome, append its evidence and update the checkpoint. Preserve these fields:

- Ticket URL, objective, route, dependency state and integration order.
- Parent identity, launch host, T3 Code ticket thread ID when applicable, Codex session ID, model/effort, process or host handle, verified cwd, branch, remote and integration branch.
- Handoff, checkpoint, event log and durable evidence paths; initialization script path/hash, selected coordinator model/effort and accepted command/message IDs.
- Candidate/base, protected unrelated changes, writer test hashes, role selections and file ownership.
- Logical role, physical attempt ID, Codex agent ID, observed state and durable verdict/log paths.
- Review rounds consumed/cap, all prior attempts and failures, frozen gate candidate/base, command, owner, start, exit and evidence.
- Parent integration grant naming candidate/base and permitted effects.
- Action IDs, intents, observed outcomes, unknown effects and reconciliation decisions.
- Remote branch/head, PR URL/head/base, required current CI, verified merge commit, completion comment, closure and retirement evidence.
- Blockers, replacement history, next admissible action and pilot verification gaps.

Only the orchestrator updates the checkpoint. Use a same-directory temporary file and atomic rename, with file and directory fsync when recording machine-crash durability. Keep event and evidence history append-only. Update before compaction, a stop or a requested decision.

For work hosted in T3 Code, link every ticket PR to its owning thread through the available `link_pull_request` tool immediately after creating or taking over that PR, and check the thread's PR list before finishing publication work. Report linking failures.

Before push, PR creation, merge, summary posting, issue closure or retirement, inspect actual state for an existing effect. Perform only authorized actions. Follow `AGENTS.md` and the work plan for current review/local gates, required CI, verified merge, completion summary, closure and `retire-worktree`. Dispatch does not imply completion.

## Recovery and pilot

1. Read the handoff, checkpoint, event history and role evidence. Inspect actual Codex sessions, process or host handles and native agent handles. Confirm exclusive ownership and attach to surviving work instead of redispatching it.
2. Compare actual Git HEAD, base, status and writer hashes with retained claims. Inspect remote refs, PRs, current CI, merge state, completion summaries and issue state. Classify unresolved intents as not started, live, completed with evidence, failed or unknown. Consume proven results once; retain unknown outcomes and obtain an explicit decision before a replacement or retry.
3. For a T3 Code ticket, reopen or attach to the exact recorded worktree-bound thread and reconcile its Codex session and role attempts there. Verify its path, branch and thread ID before resuming the handoff. Keep recovery in T3 Code; the CLI command below applies only to tickets launched outside T3 Code. If the thread is unavailable, report that blocker and reconcile any explicitly authorized replacement thread with the same ticket records before dispatch.
4. For a ticket launched outside T3 Code, if the matching Codex orchestrator is still live, attach through its recorded Codex session or host handle and inspect readiness without launching another process. When the previous process has exited and surviving children are reconciled, resume the exact recorded Codex session directly in its ticket worktree:

   ```sh
   codex --yolo -C WORKTREE resume SESSION_ID "Read HANDOFF_PATH and CHECKPOINT_PATH before acting."
   ```

   Replace placeholders with recorded values. Verify cwd, the resumed session ID and the first recovery action before reporting recovery started. Avoid an unrelated latest session. If the recorded session cannot be resumed, record an explicitly reconciled replacement top-level Codex session. Keep prior review counts and evidence, and renew authority for candidate/base drift.
5. Resume legacy tickets only through Codex after this reconciliation. Archived Pi/Orca launch commands and historical handoff next actions carry no current runtime authority. Preserve source evidence and external object IDs; do not relabel historical results as newly tested revisions.
6. Pilot on one explicitly selected new ticket before broad rollout. Demonstrate isolated roles, one top-level session per ticket, one worktree-bound thread per ticket when launched from T3 Code, durable evidence, recovery from an ordinary orchestrator restart at a settled boundary, and verified project completion and cleanup. Report untested crash windows. Broad rollout requires Mark's approval.

Neither Codex session history nor this checkpoint protocol promises process resurrection, child survival, automatic continuation or exactly-once external actions. Recovery requires observed state and explicit resumption. End the ticket session only after roles stop and authorized cleanup and completion evidence are reported.

## Cleanup override

Invoke the shared `retire-worktree` engine only after the project's verified completion and issue-closure requirements. Pass `RETIRE_HOOK` as the absolute path of the main checkout's `scripts/retire-worktree.sh`, including when retiring a legacy worktree that does not contain the hook. Verify the hook exists and is executable before invoking the engine. The hook performs only DDEV teardown and returns control to the engine for Git worktree and branch removal. This explicit hook replaces the shared engine's external workspace-management steps; `ORCHESTRATOR=none` alone configures creation, not retirement.

Keep the shared skill's path, dirty-worktree and merged-state safeguards. The hook retains best-effort DDEV cleanup with a visible warning on failure; inspect that result before claiming complete cleanup. Stop and reconcile any unexpected engine or hook outcome. Preserve the T3 Code thread and external evidence as the ticket record; worktree retirement does not authorize deleting the thread.
