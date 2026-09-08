# Plan: Parallel Sub-Agents (nacelle core)

## Goal
Enable multiple sub-agents to run in parallel, collecting results concurrently. First cut: agents compute independently (no shared file editing). Later: add file ownership boundaries if concurrent edits are needed.

## Why
The current `NewSubAgentTool` blocks the parent until one nested agent finishes. Parallel agents are the natural extension of the existing concurrency contract (tool calls already run in parallel, MCP calls already run in parallel).

## Approach
Each parallel agent runs in its own goroutine on the same backend, with its own conversation tree and result. Results are collected via a JSON map. If tasks involve file editing, each agent owns a non-overlapping subset of files (see "Conflict Prevention" below).

## Conflict Prevention (for file editing scenarios)

### 1. File ownership boundaries (primary)
Before launching parallel agents, decompose the task so each agent owns a distinct file subset:
- Agent A → `apps/api/modules/auth/*.go`
- Agent B → `apps/api/modules/users/*.go`
- Agent C → `apps/api/migrations/*.sql`

This is the single most effective practice. If agents have no overlapping files, there are zero merge conflicts. The parent (or user) must decompose the work into non-overlapping file sets.

### 2. Git worktrees
Each parallel agent gets its own git worktree at spawn time. The agent operates on a frozen copy of the repo state from that moment. The coordinator (parent) waits for terminal status, reads the diff, and decides whether to merge. This is the pattern from the Brandon Redmond interview: "Each task agent gets its own git worktree at spawn time, so it's operating on a frozen copy of the repo state from that moment."

### 3. File reservation / distributed locks
If agents must touch overlapping files, use distributed locks:
- Redis `SET lock:<file> <agent-id> NX EX 300` — try to acquire a 5-minute lock
- If the lock is held, the agent waits and retries
- Release the lock when the agent finishes
- Pattern from the Redmond interview: "The coordinator owns a lock on shared files/tasks, tracks which agent claimed what, and only writes back when the agent reports terminal status."

### 4. Context snapshotting
Each agent receives a snapshot of the codebase state at spawn time (via the system prompt + conversation), not a live-updating view. This prevents "context drift" where Agent 7 operates on a stale view that Agents 1-3 already modified. The coordinator reconciles diffs at completion time instead of inside the agent.

### 5. Plan / Execute split
- **Plan phase (single agent/parent)**: Decompose the task into independent subtasks, assign file ownership, create the task list
- **Execute phase (parallel agents)**: Each agent works on its assigned subtask with its own context window
- **Synthesis phase (parent)**: Collect results, detect conflicts, merge or escalate

### 6. Task claiming from shared list
If using a shared task list:
- Agents claim tasks one at a time; each task carries its own file ownership
- Task claiming uses distributed locks to prevent two agents claiming the same task
- After completing a task, the agent reports results and the task is marked done

### 7. Size tasks appropriately
- Too small: coordination overhead exceeds benefit
- Too large: teammates work too long without check-ins, increasing wasted effort risk
- Just right: self-contained units that produce a clear deliverable (function, test file, review)

**Rule of thumb**: 3-6 tasks per parallel agent; narrow task scope → small diffs → solvable merge.

## Implementation (nacelle core, already done)

### Files created:
- `subagent_parallel.go` — `NewParallelSubAgentTool`, `ParallelSubAgentOptions`, `parallelDelegate`
- `docs/parallel-agents.md` — full implementation plan

### Files modified:
- `subagent_test.go` — 6 new parallel tests

### Key design:
1. **Semaphores** cap concurrency at `MaxConcurrency` (default 4, clamped to 8)
2. **`sync.Mutex`** safely gathers results from concurrent goroutines
3. **Partial failure isolation** — if one task fails, others continue; errors returned in separate `errors` JSON field
4. **Recursion guard** — the parallel tool is stripped from each nested agent's tool set (same pattern as `subagent`)
5. **Result format**: `{"tasks":{"0":"result0",...},"errors":{"1":"error1",...}}`

## Files to Modify (nacelle-tui — pending your agent's work)

1. `internal/tui/model.go` — add `agents map[string]*agentSpec`, `activeAgent string`
2. `internal/tui/inflight.go` — convert to per-agent tracker
3. `internal/tui/run.go` — `send()` fans out for parallel tasks
4. `internal/agent/build.go` — register `ParallelSubAgentTool`
5. `internal/tui/views/parallel_view.go` (new) — tabbed view
6. `internal/tui/commands/parallel_cmd.go` (new) — `/parallel` command parser

## Exit Criteria

### nacelle core (done)
- `go build ./...` ✓
- All existing tests pass ✓
- `TestParallelSubAgentReturnsAllResults`: all 3 results returned ✓
- `TestParallelSubAgentPartialFailure`: others continue on one failure ✓
- `TestParallelSubAgentSharedConfig`: config not mutated ✓
- `TestParallelSubAgentEmptyTasks`: empty list fails ✓
- `TestParallelSubAgentMaxConcurrency`: clamped to 8 ✓
- `TestParallelSubAgentDirectCall`: RunTool works ✓

### nacelle-tui (pending)
- `go build ./...` silent
- `/parallel a, b, c` spawns 3 agents, UI shows results
- File ownership boundaries prevent overlap if agents edit files

## Skip (YAGNI)

- Do NOT change the Backend interface or Agent struct
- Do NOT make the model itself run in parallel
- Do NOT remove the existing single-subagent tool
- Do NOT add generic orchestration framework
- Do NOT add split-pane UI (tabbed first)
- Do NOT add rate-limiting

## Convention flags

- `[migrations]`: N/A
- `[auth/porte]`: N/A
- `[muse]`: N/A (Bubble Tea, not Svelte)
- `[module-path]`: modules stay unchanged
- `[filet]`: plan code to pass silently
- `[events]`: same event contract, no new types
- `[distribute]`: N/A

## Open Questions

1. **Tabbed vs accordion UI**: tabbed minimum, accordion natural next step
2. **Result merging**: `map[string]string` for first cut; richer struct later
3. **Cancellation**: all stop on parent Ctrl-C
4. **`/parallel` syntax**: comma-separated one line
5. **File editing coordination**: if needed, use git worktrees + file ownership boundaries

## Tools

- `gh` / `git` / `rg` / `find` for recon
- `filet` to check current state