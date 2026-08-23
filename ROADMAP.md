# Roadmap

Written 2026-08-19, updated 2026-08-23. A cold-start handoff: everything needed to pick up a
track with no prior conversation. Ordering lives here; the reasoning behind each item lives in
the commit that closes it.

**Done: Tracks A, B and C.** Every track that was planned is closed. **Planned next: Tracks D
through H**, from the 2026-08-23 gap analysis against pi / Claude Code / Codex CLI and a real
session transcript — see "Tracks D–H" below. Closed items are kept below rather than deleted,
because their reasoning is still the argument for not undoing them.

### Shipped 2026-08-23, outside any track

Three changes that came out of the same observation that opened Tracks D–H (the registre
transcript), before any track started:

- **Microcompaction** (`5b45bea`, `bb4a153`, `tui/compact.go`). When a finished turn's measured
  input size (input + cache read + cache creation) passes 100k tokens, tool results older than
  the newest four messages and larger than 1KB are replaced with `[dropped N bytes. Re-run the
  tool if the detail matters.]`, keeping id and name so call/result pairing survives provider
  validation. The status line appends `· N trimmed`. The trim budget is in bytes (`tokens * 4`);
  `/clear` resets the counters. The threshold sits high partly to amortize the prompt-cache bust
  a rewrite causes — do not lower it casually. This is the mechanical half of Track H item 17;
  what remains there is summarization, which needs a real model round trip to validate. pi has
  no compaction at all (badlogic/pi-mono#92); this closes the gap against it.
- **Status line splits input from output tokens** (`389047a`, `tui/` only). The old single
  total merged Input+Output+CacheRead+CacheCreation, which read as a counting bug on every
  agentic session: each turn re-bills the whole conversation as input, so a short chat showed a
  huge number. The count was correct billing, not a bug; the display now reads
  `in 2.6k · out 1.1k · 9.8k cached`. Track E6's footer builds directly on this.
- **`run_command` inherits the launching shell's PATH** (`4430e76`, core, behavior-only).
  `minimalEnv` hardcoded `/usr/local/bin:/usr/bin:/bin`, so `mycelium`, mise shims and anything
  in `~/.local/bin` were invisible even when nacelle was launched from a shell that had them —
  the same failure class Claude Code users fix by hand-editing `~/.zshenv`. The no-inheritance
  rule stays (secrets stay out); PATH is now nacelle's own process PATH with `~/.local/bin`
  and `$HOME/bin` appended if absent. This closes the "three repeated `export PATH=` hacks"
  symptom from the transcript below without touching Track F.

## Tracks D–H: the 2026-08-23 plan (planned, not started)

A live transcript of a real nacelle session (registre deploy task) showed where the harness
actually falls short, and it is not mostly missing features. Observed in one session: full raw
JSON tool inputs printed per call; ten `⤷ … done in 2ms` silent-success lines; reasoning printed
as body prose between tool calls; three repeated `export PATH=` hacks; two `read_file` failures on
paths outside the working directory; a `run_command` call carrying **two `"command"` JSON keys**
accepted silently by decoder-last-wins; and improvised shell pipelines where recorded mycelium flows
existed. Track order follows that evidence.

### Track D — legibility (`tui/` only, no core change)

1. **Tool-call line summarizer** — `tui/view.go` `absorb` replaces the raw-input `say`
   with name + primary argument (`command`, `path`, `pattern`, `query`), truncated via the
   existing `truncate` (mind `len("…")`, 3 bytes).
2. **Silent successes** — a successful tool result prints nothing; duration folds into the call
   line. Failures keep their own loud line; repeated identical failures collapse into one line
   with a count.
3. **Refuse malformed tool-call JSON** — duplicate keys at the trust boundary surface as a
   refused call with a reason, not decoder-last-wins. If the decode is core-side this rides
   Track F's tag instead.
4. **Collapsed thinking** — new `tui/thinking.go`: `KindThinking` renders one dim line while
   running, then `· thought for Ns (ctrl+t to read)`; ctrl+t toggles a viewport over
   `m.run.reasoning`. Buffering already exists in `absorb`.

Exit: transcript tests assert first-printed-line ordering; no raw JSON reaches any capture.

### Track E — Claude-Code-grade organisation (builds on D)

5. **Batching** — consecutive same-kind calls render as one updating line
   (`⏺ 4 commands · mycelium sync · ls docs · …`), flushed on kind change or `settle`. Must grow
   within the frame by re-render, never `tea.Println`: the headroom budget governs, and an
   in-place-updating line sits exactly in the shrink geometry the ultraviolet entry warns about.
   Test under tmux at small pane sizes (80x14), asserting pane geometry first
   (`window-size manual`).
6. **Status footer** — elapsed timer and cumulative cost on the existing status line (usage
   already tracked in `m.run.usage`). Verb variety in `spin.go`.
7. **Turn boundaries** — spacing plus running totals at `KindTurn`.
8. **Recap before quit** — tools run, tokens, cost, two lines, printed after `Run` returns
   beside the existing exit-transcript dump.

Exit: before/after captures of a registre-shaped session at 80x24; measured line-count drop.

### Track F — hooks (core + tui, one tag, two-commit release)

9. **`Config.Hooks []Hook`** invoked in `RunTool` (`toolsink.go`) beside `Config.Approve` —
   the same single seam both backends already pass through for approval. Three points:
   pre-tool (allow/deny/replace), post-tool (observe/redact), on-stop (block until checks pass).
   Synchronous function signature only. Deliberately not an extension ABI: no async hooks, no
   hook-authored UI, no lifecycle-event surface — pi's 3000-line extensions doc is the cautionary
   tale.
10. **Mycelium-flow adapter** — a hook that shells `mycelium flow run <name>` and maps the artifact
    to allow/deny. Trust inherited from flow trust, not reinvented. Check
    `~/.mycelium/memory/conventions/subagent-blast-radius.md` before wiring; deny-by-default for
    on-stop hooks (they can wedge a run — decide v1 vs deferred at implementation time).
11. **Config surface** — `~/.nacelle.yml`: deny patterns, auto-approve rules, and flow
    suggestions injected into context ("a suite-check flow exists; prefer it") so the model
    reaches for recorded procedures instead of improvising pipelines.
12. Release per the standing two-commit procedure: core tag, then `tui/` adoption commit.

Exit: a deny-pattern hook blocks a `run_command` identically on both backends, one test each.

### Track G — headless mode (unblocks CI and Atelier)

13. Extract the settings/precedence chain from `tui/config.go` so a non-TUI entry point reuses
    it [filet: config.go trips ceilings easily; budget the split].
14. `-print "prompt"` / piped-stdin mode: build the agent, iterate `Stream`, text deltas to
    stdout, nonzero exit on error or refusal. No bubbletea on this path. Hooks (F) are what make
    headless safe; sessions (H) make it resumable.

Exit: `echo q | nacelle -print -root .` answers with no TTY.

### Track H — sessions, then compaction

15. **`session/` root package** — append-only JSONL over the event stream, using the A6 message
    union as the record format (its wire-level round-trip tests are the proof). Decide the
    secret-redaction policy *before* writing the first line: tool results can carry command
    output containing credentials. Core stays print-free.
16. **Resume** — `--continue` picks the newest session under `~/.nacelle/sessions/<project>/`;
    `/resume` picker in the TUI. Supersedes the exit-time transcript dump's role as the only
    session memory.
17. **Compaction in `tui/`, not the core** — strategy stays consumer-side per `trim.go`'s own
    doc. **Mechanical half shipped 2026-08-23** (see "Shipped outside any track"): old large
    tool results drop past a measured 100k input threshold, no model call. What remains:
    summarize near the limit when dropping alone cannot keep up — assistant-text-heavy
    sessions, or early turns carrying decisions rather than tool output. Needs a real model
    round trip to validate quality; guard mechanically only. Surface
    `Event.Stop == StopContext` as actionable rather than fatal.

Exit: kill mid-session, `--continue`, ask what we were doing, get a correct answer including
prior tool calls. A conversation crossing the window compacts instead of dying.

### Standing constraints across D–H

- Any core change ships as two commits (core tag, then tui adoption); see "Where this is".
- Filet ceilings: `tui/model.go` sits at exactly 250 lines; budget a split on sight in every
  track.
- Compaction quality is a model-quality question and untestable here; guard mechanically only.
- Not in scope, still: extension ABI, subagents, plan-mode state machine, checkpoints, IDE/LSP,
  profiles/themes, `.mcp.json` discovery, server-side web search, `sandbox/` (until Atelier
  asks).

## Where this is

Both backends, `tools/`, retry, prompt caching and per-turn usage are implemented and
tested. `Message` is a sealed content-part union (A6): a conversation can carry a tool call and
round-trips through both backends at the wire level, with a test proving it per backend. CI runs
the gate — build, vet, `-race` test, `golangci-lint` — on push and PR, and cannot be bypassed with
`--no-verify` the way the old pre-push-only hook could (B3, B4). `CHANGELOG.md` and `docs/` exist
(B5), mirroring `tronc`'s actual structure since the wiki convention page they were meant to
follow no longer exists post-reset.

Everything A6 unblocked is now built, not just unparked: `Backend.CountTokens` (real on Anthropic,
honestly refused on OpenRouter), `Usage.CacheHitRate`, and `Trim` — a boundary-safe truncation
primitive, deliberately not a summarizer; see `message.go` and `trim.go`'s own doc comments for
why compaction *strategy* stays out of this package. Track C shipped as `tools.Mycelium()` — a
nacelle tool package that shells out, the option the roadmap left as "decide first" — rather than
an MCP server added to mycelium, because it needed no change outside this repo.

`tui/` is the SDK's first consumer: rebuilds the real conversation from the event stream instead
of dropping tool history, renders answers as markdown, scrolls, shows a spinner for the gap before
the first token, and (as of this round) discovers `CLAUDE.md`/`AGENTS.md` above `-root` and can
reach mycelium's flows and memory search, both default on and both fail soft to nothing when there
is nothing to find. It also compacts (see "Shipped outside any track"): past a measured 100k
input threshold, old large tool results drop mechanically, so a long agentic session degrades
instead of dying at `StopContext`. The tui module pins the core via go.mod; cut 0.2.3 covers
everything through `bb4a153` except the changelog entry, which lands under Unreleased for 0.2.4.

**Reasoning, added 2026-08-23 and never on this roadmap.** `Config.Effort` and
`Config.Thinking bool` became one `Thinking{Effort, Budget, Show}`, which is the API break
`v0.2.0` is named for. It was not a feature request: `openrouter/turn.go` was assembling a
streamed reasoning block by keeping the last fragment of it and replaying that to the model as its
whole previous thought, and the default configuration was asking every provider to discard the
reasoning entirely, so the merge that would have caught it never ran. `openrouter/reasoning.go`
rejoins the fragments and `docs/configuration.md` carries the settings. The thing worth taking
from it, recorded because this roadmap is where the next person looks: both defects survived a
green gate, because the backend tests run against a fake server that accepts any JSON, and both
were found by making a real request.

A later round added `~/.agents/AGENTS.md` (the AGENTS.md standard's own global-base path,
deliberately not a user's `~/.claude/CLAUDE.md` — see `tui/context.go`'s doc comment) and skill
loading from `~/.agents/skills/` and trusted `.agents/skills/`, following the Agent Skills
specification. Project-local skills are the one thing gated on trust
(`~/.nacelle/trust.json`, `-trust-skills`) — a `SKILL.md` can carry scripts to run, unlike the
plain instruction text `AGENTS.md`/`CLAUDE.md` hold, which stays ungated for the reason in
`docs/architecture.md`'s "What is deliberately not here."

The round after that closed the one item this file had ruled out: **per-call tool approval**.
`Config.Approve` is checked in exactly one place, `RunTool` (`toolsink.go`), so a refusal is the
same event on both backends — a `KindToolResult` carrying `Refused`, not a silent skip. That is
also what answered the design objection recorded below: nothing had to intercept the vendor SDK's
runner, because the callback blocks inside the tool call that runner already makes.
`tui/approve.go` answers allow / allow-for-session / deny and waits on the run's own context, so
ctrl+c rescues a forgotten prompt. Off by default (`-approve-tools`): by default agents yolo.

**Slash commands** shipped in the same stretch — `/clear`, `/help`, `/quit` and `/skill:name`,
with a fuzzy-matching dropdown while typing `/` rather than ghost text — plus `-skill-dir`, which
loads another tool's skills instead of duplicating them.

Tagged since 2026-08-22, correcting what this paragraph said for three days. `v0.1.0` was the
first; the reasoning work took the core to `v0.2.2` the day after. Every core tag is paired with
a `tui/vX.Y.Z`, because `tui/go.mod` requires a *published* core rather than the working tree.

That pairing is the standing cost of the two-module layout, and the shape of it is worth reading
once rather than rediscovering at a push.

`scripts/check.sh` checks `tui/` against **both** cores. The build step runs with `GOWORK=off`, so
`tui/` resolves the core from the proxy at the version its `go.mod` names; `vet`, `test -race` and
the nested lint pass run with the workspace on, against the tree. CI does not do that second
thing: `.github/workflows/ci.yml` sets `GOWORK: "off"` in **both** jobs, so what main is ever
judged on is `tui/` source built against the *published* core.

That difference is the whole procedure. A breaking core change lands as **two commits, never
one**:

1. **Core only**, `tui/` untouched and still pinned to the old core. CI builds old `tui/` source
   against the old published core and is green. Tag `vX.Y.Z` here and push it.
2. **`tui/` adoption**: `go mod edit -require` onto the new tag, plus the source migration. CI
   builds new `tui/` source against the now-published core and is green. Tag `tui/vX.Y.Z`.

Only the local gate objects, and only at step 1, where workspace-on `vet` sees a core the
unmigrated `tui/` cannot compile against. At a release boundary that is a false alarm by
construction, which is what `--core-ahead` is for: it skips the three nested checks that resolve
the core from the tree and runs everything else, where `git push --no-verify` runs nothing.

`8d8a2b9` is what the one-commit version looks like: it changed the core and nine `tui/` files
together, so no published core could satisfy it and CI went red until the next commit. Checked
2026-08-23 by simulating both orderings against the real gate rather than reasoning about them,
which is also how the first two attempts at writing this paragraph were found to be wrong.

What is not built, flagged rather than silently dropped. None is ordered, and none has earned a
`###` entry yet — each needs its own scoping session first. Two of them were built on 2026-08-21
and are struck through below rather than deleted, because the argument for their shape is still
the argument against redoing them differently.

- ~~**A stdio transport in `mcp/`.**~~ Built 2026-08-21 as `mcp/client`, on the official
  `modelcontextprotocol/go-sdk`. It bridges a subprocess server's tools to `nacelle.Tool`, which
  is what makes them work on **both** backends rather than only the one with a server-side
  connector — `openrouter` refuses `Config.MCP` under the capability rule and would otherwise get
  no MCP tools whatever the transport. That is also this repo's answer to "how does a user add a
  tool without forking the binary", the question that raised the item on 2026-08-20 while
  comparing against Crush and pi. A sub-package because the root imports `mcp`, so `mcp` cannot
  import back.
- ~~**The Streamable HTTP transport in `mcp/client`.**~~ Built 2026-08-21 as `client.Remote`, the
  round after the stdio one, and it closes the last gap in this area: a hosted MCP server is now
  reachable from `openrouter` too, where before it needed Anthropic's own connector. `Connect`
  took a sealed `Server` union to hold both without a mode field. SSE and `ws` are **deliberately
  absent** — MCP replaced SSE with Streamable HTTP, so they are refused by name with the key to
  change rather than half-supported. `client.Load` reads the ecosystem's `mcpServers` format for
  the reason `-skill-dir` exists: a person adopting this has those files already.
  What is left here is not a gap so much as a decision nobody has needed yet: **discovering** a
  project-local `.mcp.json`. It stays out because a config file naming executables to run is
  strictly worse than the project-local `SKILL.md` already gated behind `~/.nacelle/trust.json`,
  so doing it means designing that trust gate rather than reusing it by accident.
- **Server-side web search, as a paid alternative to `tools.WebSearch`.** Not ordered, and not a
  gap — `tools.WebSearch` already covers this need for free against an instance you run. This is
  the option for someone who would rather pay than host a service. Both backends have it
  natively: Anthropic's `web_search_20260209` is a request parameter and `anthropic-sdk-go`
  already ships `BetaWebSearchTool20260209Param`, and OpenRouter takes `plugins: [{id: "web"}]`
  through the `option.WithJSONSet` escape hatch `requestOptions` already uses. It would fit
  `Capabilities` the way MCP does.

  What it costs, measured 2026-08-20 and the reason it is the alternative rather than the plan:
  **$10 per 1,000 searches** on Anthropic plus tokens for retrieved content, and no free tier on
  OpenRouter (~$0.007/request on its Exa default). It is also per-backend work twice over, where
  a local tool is written once and runs on both.

  Real work if it is ever picked up: `anthropic/calls.go`'s `isToolUse` does not handle
  `server_tool_use` / `web_search_tool_result` blocks; and `stopOf` parks `pause_turn` in
  `StopOther`, which server-side search makes reachable in practice rather than in theory — a
  long search would end a run as an unnameable stop with a half-finished answer.

  Both sites now say so in their own doc comments, and point at each other, so the trap is
  reachable from the code rather than only from this file (2026-08-23). That matters because the
  two have to move together: whoever adds the request parameter will be reading `calls.go`, not
  a roadmap bullet about pricing.

  This bullet used to call that "the same gap behind the known `mcp_tool_use` orphan". There is
  no such orphan and there has not been for some time: `isToolUse` takes both spellings,
  `calls.go` tags the call `remote` and files it, `mcp.go`'s `remoteResult` closes it on the
  matching `mcp_tool_result`, and `unanswered.go` sweeps whatever is still open when the run
  ends. Corrected 2026-08-21 after the claim sent a session looking for a closed bug.
- ~~**A total-time budget on `RetryOptions`.**~~ Built 2026-08-21 as `RetryOptions.Budget`.
  The larger version turned out to be the only version that works: a stopwatch read between
  attempts would notice the six minutes only after they had been spent, because the sleeps are
  inside the attempt. A deadline derived once in `Stream` and passed down does reach them — both
  SDKs wait out a `Retry-After` in a `select` on `ctx.Done()`, verified in each vendored SDK
  rather than assumed. No default: a deadline bounds the successful attempt too.
- **A global `~/.claude/CLAUDE.md` layer.** Deliberately out, same reasoning as skipping it for
  `AGENTS.md`'s own global path.

## Tracks

**A**, **B** and **C** are all closed. The open work is Tracks D–H at the top of this file —
nothing below this line is ordered.

---

## Track A — make the event contract true, then widen Message

### A0. Say when a run was retried

`retry.go`, `transient.go`, `nacelle.go`

`Retry` logs nothing, so a run that retried twice is indistinguishable from one that did
not. The convention (`~/.mycelium/memory/conventions/log-levels-for-retries.md`) is
explicit: the library marks what it will retry **with the attempt number** and keeps
`Unwrap` intact; the consumer logs `warn` per attempt and `error` only on giving up.
`Unwrap` is already there. The attempt number and the `Config.Logger` call are not.

Smallest item in the track. Good warm-up.

### A1. `openrouter` must emit `KindToolCall`

`openrouter/convert.go`, `openrouter/turn.go`

`deltaEvents` maps text and reasoning only. Calls are reassembled in `finish()` from
`accumulator.Choices[0].Message.ToolCalls` and then executed with no event at all, so on
this backend a tool runs invisibly. Emit each call before running it, carrying `id`,
`name` and `arguments`.

### A2. `anthropic` must carry the tool-call ID

`anthropic/adapt.go`, `anthropic/calls.go`

`adapt.go` passes `""` to `RunTool`, so every `KindToolResult` has an empty `Tool.ID` and
`event.go`'s documented pairing contract is true on neither backend. The SDK's
`BetaTool.Execute` genuinely never receives the `tool_use` id, so correlate through
`callTracker.open`, which already holds ID, name and input.

Fallback if that proves fragile: read `runner.Messages()` after each turn. Budget for it.

**Prerequisite for A5 and A6.**

### A3. Stop reasons must reach the caller

`event.go`, `anthropic/stream.go`, `openrouter/turn.go`

Nothing inspects `stop_reason` or `finish_reason` anywhere. So `max_tokens` truncation,
`model_context_window_exceeded` and a safety `refusal` all end the run as a clean
`KindDone` — a truncated answer is indistinguishable from a finished one. That is the
sharpest contradiction of this package's own "it fails rather than degrading".

Add a `Stop` field to `Event` and populate it on both backends.

### A4. `MaxIterations` must mean the same thing on both backends

`openrouter/stream.go`, `anthropic/stream.go`

Anthropic returns a silent `KindDone`. OpenRouter returns a stream error **and throws away
the accumulated usage total**, which breaks the invariant stated in the package doc: usage
is reported per turn, always. Both should emit `KindDone` carrying the total and the A3
stop reason.

### A5. Tool results must arrive in a stable order

`toolsink.go`

Handlers run concurrently on the SDK's errgroup goroutines and `Drain` returns arrival
order, so two runs over an identical model response emit different event sequences. Any
test asserting on the event stream is flaky by construction. Key the sink by tool-call
index and sort before returning.

### A6. Widen `Message` so a conversation can carry tool calls

`backend.go`, `event.go`, both backends, `tui/model.go`

`Message` is `{Assistant bool; Text string}`. A conversation cannot express a tool call,
which means a resumed transcript drops all tool history, and cross-call prompt caching is
impossible because the replayed prefix can never byte-match what the runner sent.

Replace it with a sealed union of content parts — text, reasoning, tool-call, tool-result,
finish — with `ToolCall.Finished` and a raw-JSON `Input` so a partially-streamed call is
representable. Shape borrowed from Crush's design, which is recorded in
`~/.mycelium/memory/syntheses/crush-core-architecture.md`. **Read for shape, write our own:
Crush is FSL-1.1-MIT and we are the competing product.**

This is a breaking public API change, and doing it now is the point: untagged, one
consumer, all in-repo. It gets cheaper never again.

Known trap, from Crush: they maintain the same union in four places (prompt, response,
persistence, wire) and say so in their own notes. Pick one representation and convert at
the edges.

---

## Track B — give the gate teeth (parallel)

### B1. Fix the data race in `tools`

`tools/command.go`

`go test -race ./tools/` fails on `TestCommandTimesOutAndSaysSo`, and has been failing
since before the retry work. `processGone` polls `cmd.ProcessState` from its own goroutine
while the `cmd.Wait()` goroutine writes it.

The fix is a deletion: `run()` already has a `done` channel that signals reaping, so pass
it to `killGroup` and select on that. `processGone` disappears.

### B2. Run `-race` in the gate

`scripts/check.sh`

B1 survived because nothing ever looked. The sink and the tool handlers are exactly the
shape `-race` exists for.

### B3. There is no CI

`.github/workflows/ci.yml`, `filet.yml`

The entire gate is a pre-push hook, bypassable with `--no-verify`. Copy tronc's two-job
shape: root job plus a `check-modules` matrix with `working-directory`, since `tui/`
declares a higher Go floor than the root.

Also add the `architecture:` block to `filet.yml`. Without it `failOn: error` can only fire
on a Go file that does not parse — the gate is toothless by construction today.

### B4. No linter

`.golangci.yml`, `scripts/check.sh`

Copy tronc's config. Guard the pass on `golangci-lint version` succeeding, not on
`command -v` — a mise shim for an uninstalled version resolves on PATH and then always
fails. And `golangci-lint run ./... ./tui/...` prints "directory prefix does not contain
main module" to stderr and still exits 0, so nested modules need a `cd`.

### B5. No CHANGELOG, no docs

`CHANGELOG.md`, `docs/`

Both are required at `v0.1.0`. Keep-a-Changelog 1.1.0, and while on v0 a breaking change
bumps the minor. tronc had to backfill its link-ref block four releases late — start the
`## [Unreleased]` section now.

`docs/` follows `facile-docs-standard`: kind `lib`, 40-line floor and 200-line ceiling per
page, `api.md` exempt from the ceiling.

---

## Track C — the differentiator (after A)

### C1. mycelium as deterministic tooling

`mycelium/` (new)

Every harness hands the model `run_command` and hopes. We have `mycelium flow` — recorded,
trust-gated procedures that return a structured artifact instead of scraped stdout, and
that report `CHANGED` rather than drifting silently. A tool that runs a named flow is
deterministic where an improvised shell pipeline is not. Same shape for
`mycelium memory search`, which gives an agent the project's own accumulated knowledge.

This is the part no general-purpose harness can have, and the reason for owning the
harness at all.

**Decide first:** a nacelle tool package that shells out, or an MCP server added to mycelium.
MCP would cost nothing here — the wiring already exists — but mycelium has no MCP surface
today, so that is a mycelium change, not a nacelle one.

**Check `~/.mycelium/memory/conventions/subagent-blast-radius.md` before wiring it.** Letting
a model execute recorded procedures widens what an agent can do; per-machine trust gating
helps but does not settle it.

---

## Exit criteria

- `sh scripts/check.sh` green **with `-race`**, every module.
- `filet check .` silent.
- A conversation containing a tool call round-trips through `Stream` and the model sees it.
  This is the one that proves A6.
- Both backends emit `KindToolCall` and `KindToolResult` with matching non-empty IDs, with
  a test per backend.
- A truncated run is distinguishable from a finished one by `Event.Stop` alone.
- Two runs over one recorded response emit byte-identical event sequences.
- CI runs on push and cannot be bypassed.

## Not in scope, deliberately

- **A provider interface beyond the two backends.** Settled; revisit when a third is real.
- **`sandbox/`.** Atelier drives it, and Atelier has not asked.
- ~~**Tagging `v0.1.0`.**~~ No longer parked on another repo — see "Where this is" above.
- ~~**Per-call tool approval (allow / allow-for-session / deny) in the TUI.**~~ Built, on
  request. The design decision it was waiting on turned out to have an answer: a consumer-side
  pause does have to block inside the vendor SDK's runner (`sdkTool.Execute`,
  `anthropic/adapt.go`), and it does — `RunTool` is inside the call that runner already makes, so
  one gate there serves both backends and nothing intercepts the runner itself. Everything else
  in that comparison (sessions, panes, a model picker) still fails the test the line below
  applies.
- **Sessions, profiles, themes, panes in the TUI.** None of them test the API, which is the
  only thing the TUI is for.
- ~~A config file for the TUI. Flags until they hurt.~~ Built anyway, on request, after flags
  hurt once. `~/.nacelle.yml`, preferences only. Still skipped: a per-project `./.nacelle.yml`,
  which is a second precedence layer before the first has users.

## Conventions checked

`log-levels-for-retries` (A0 exists because of it), `facile-docs-standard` (B5),
`project-architecture`, `facile-cli-repo-pattern`, `project-naming`.

The line that used to sit here, that commits take an imperative subject with no
conventional-commit prefix, was true when it was written and is not any more. `2f6b3a9` replaced
the tracked githooks with `lefthook.yml`, which pulls `github.com/FacileStudio/hooks` at `v1` and
installs a `commit-msg` hook that validates Conventional Commits and refuses anything else; the
hook derives the gitmoji from the type, so writing one by hand is also wrong. `git log` disagrees
with itself either side of that commit, which is exactly why this is written down rather than
inferred from the history.

Not applicable: migrations, auth/porte, muse, events, cross-repo distribution. nacelle is a
flat library like tronc — no database, no HTTP surface, no UI.
