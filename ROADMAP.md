# Roadmap

Written 2026-08-19. A cold-start handoff: everything needed to pick up a track with no prior
conversation. Ordering lives here; the reasoning behind each item lives in the commit that
closes it.

**Done: Tracks A, B and C.** Every track that was planned is closed. What is left is unplanned —
see "Where this is" below for what a next session would actually pick up. Closed items are kept
below rather than deleted, because their reasoning is still the argument for not undoing them.

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
is nothing to find.

No tag; `v0.1.0` stays gated on Kori, which is itself blocked on Perception's MCP server, a
Perception problem. **Do not sequence nacelle work around Kori.**

What is not built, flagged rather than silently dropped: a global `~/.claude/CLAUDE.md` layer
(deliberately out — see `tui/context.go`'s doc comment), slash commands, and a skills-loading
subsystem. All real, all bigger and fuzzier than fit alongside the rest of this round; each needs
its own scoping session before a `###` entry here would mean anything.

## Tracks

**A**, **B** and **C** are all closed. Nothing here is currently ordered — the next work is
whichever of the "not built" items above, or something not yet named, earns a session of its own.

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
- **Tagging `v0.1.0`.** Gated on Kori.
- **Per-call tool approval (allow / allow-for-session / deny) in the TUI.** The pattern most
  comparable harnesses have (Crush's permission dialog, Pi's approval loop) and the one
  candidate from that comparison that would exercise the SDK rather than just decorate the
  client — everything else in that category (sessions, panes, a model picker) fails the same
  test the line below already applies. Not started: it needs a design decision the TUI cannot
  make alone, since on the Anthropic backend the tool-call loop runs inside the vendor SDK's own
  runner (`sdkTool.Execute`, `anthropic/adapt.go`) and a consumer-side pause has to block there,
  not intercept a step nacelle's own loop controls the way OpenRouter's hand-rolled one does.
- **Sessions, profiles, themes, panes in the TUI.** None of them test the API, which is the
  only thing the TUI is for.
- ~~A config file for the TUI. Flags until they hurt.~~ Built anyway, on request, after flags
  hurt once. `~/.nacelle.yml`, preferences only. Still skipped: a per-project `./.nacelle.yml`,
  which is a second precedence layer before the first has users.

## Conventions checked

`log-levels-for-retries` (A0 exists because of it), `facile-docs-standard` (B5),
`project-architecture`, `facile-cli-repo-pattern`, `project-naming` — commits are an
imperative subject stating the consequence, no conventional-commit prefix; the wiki's
stated rule is stale and the repos disagree with it.

Not applicable: migrations, auth/porte, muse, events, cross-repo distribution. nacelle is a
flat library like tronc — no database, no HTTP surface, no UI.
