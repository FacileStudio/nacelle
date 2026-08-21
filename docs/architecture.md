# nacelle — Architecture

How a run moves through the loop, why the seam sits where it does, and the concrete defects
the design avoids by construction.

## What this is

An agent is a loop around a model with tools attached. That loop is about two hundred lines,
and it gets rewritten in every project that needs one, slightly differently, with a slightly
different bug in the tool-result handling. `nacelle` is the single version — the model loop,
the tool registry and the MCP wiring, embeddable by a headless service, a container-isolated
best-of-N runner, and a terminal client alike.

Four properties make that possible, and none is negotiable:

- **The loop returns events and never prints.** A backend streaming SSE, a terminal UI and a
  test are all consumers of one stream — see [Event stream](#event-stream) below.
- **Usage is reported on every turn**, not only at the end, because comparing runs on cost is
  a reason this package exists.
- **Backends declare what they support**, and an agent that asks for more is refused at
  construction rather than quietly running with less — see [Capabilities](#capabilities).
- **The core knows nothing about any product.** No documents, no repositories, no citations.
  A consumer that needs its own vocabulary here has found a bug in the abstraction, not a
  missing feature.

## The `Backend` seam

```go
type Backend interface {
	Name() string
	Capabilities() Capabilities
	Stream(ctx context.Context, request Request) iter.Seq2[Event, error]
}
```

The seam sits at the whole loop rather than at a single request, because the loop is exactly
what differs between the two backends this package ships. Anthropic's SDK ships a tool-calling
loop of its own and connects to remote MCP servers on its own side of the request; an
OpenAI-schema backend has neither, and drives the conversation itself, turn by turn. An
interface at the request level would have forced the Anthropic path to give up the tested loop
it gets for free, to look symmetrical with a backend that cannot have it.

`Agent.Stream` is the only entry point a consumer calls. It fills in the agent's configured
`System`, `Tools`, `MCP`, `Effort`, `Thinking` and `MaxTokens` around the conversation handed
in, validates the conversation (see [The `Message` union](#the-message-union)), and delegates
to the backend.

## Capabilities

```go
type Capabilities struct {
	MCP      bool // remote MCP servers
	Thinking bool // KindThinking events
	Effort   bool // an Effort level on the request
	Cost     bool // Usage.Cost carries real money
}
```

Every field is a feature a caller can ask for and be refused, on purpose not a vague tier: a
consumer that needs MCP needs to know that specific thing is missing, not that the backend is
"limited". `nacelle.New` checks a `Config` against the chosen backend's `Capabilities` and
returns an `*Unsupported` error rather than silently running with less — losing MCP tools in
silence looks like a model that will not use them, and that is a bad afternoon to debug.

### The two backends

| | `anthropic` | `openrouter` |
|---|---|---|
| MCP | yes — Anthropic connects on its own side | no — no server-side connector to drive |
| Thinking | yes | yes, gated by the request |
| Effort | yes | yes |
| Cost | no — the API returns tokens only | yes — OpenRouter prices every generation |
| Tool loop | the SDK's own `BetaToolRunnerStreaming` | hand-rolled, turn by turn |

`openrouter` fronts hundreds of models behind one OpenAI-compatible endpoint, which is what
makes it worth having — comparing two models is a change to one string rather than a change of
client — at the cost of the row above. What it gains in return is money: `Usage.Cost` is real
there and zero on `anthropic`, which only counts tokens.

## Event stream

```go
type Kind string

const (
	KindText       Kind = "text"       // an answer delta
	KindThinking   Kind = "thinking"   // a reasoning delta, opt-in
	KindToolCall   Kind = "tool_call"  // the model asking for a tool, before it runs
	KindToolResult Kind = "tool_result" // that tool having finished
	KindTurn       Kind = "turn"       // one assistant turn ended
	KindDone       Kind = "done"       // the run ended
)
```

Every `KindToolCall` is closed by a `KindToolResult` carrying the same `Tool.ID` — including a
call that never ran, closed with `Tool.Discarded = true` (see below) rather than left open
forever. `KindTurn` and `KindDone` both carry `Usage`; `KindDone` carries the run's total.

### Why a `Stop` field exists at all

`max_tokens` truncation, a context window exceeded, and a safety refusal all arrive as a
well-formed response with a normal ending. Without a way to say which happened, a truncated
answer is indistinguishable from a finished one — the sharpest contradiction of this package's
own "it fails rather than degrading". `Event.Stop` is that field:

```go
const (
	StopEnd        Stop = "end"        // finished
	StopTools      Stop = "tools"      // more turns follow; never the reason a run ended
	StopMaxTokens  Stop = "max_tokens"
	StopContext    Stop = "context"    // the conversation outgrew the window
	StopRefusal    Stop = "refusal"
	StopIterations Stop = "iterations" // MaxIterations reached, work unfinished
	StopOther      Stop = "other"      // a reason this package does not have a name for
)

func (s Stop) Complete() bool { return s == StopEnd }
```

Only `StopEnd` is safe to present as a finished answer; a consumer that checks `Complete()`
gets the right answer for every other value, named or not.

### `ToolEvent.Discarded`

A call an attempt made and then a retry superseded — Anthropic's fallback path can invalidate
a `tool_use` block mid-stream and never run it — still has to close its `KindToolCall` with a
`KindToolResult`, or a consumer's pending call hangs forever. `Discarded = true` marks that
closure as fictional: the call never ran, and a consumer building the next request from the
event stream (as `tui/` does) must drop it rather than replay it as answered history. An MCP
call a server genuinely failed to answer is the opposite case — it really was issued — and
closes with `Discarded = false`.

## The `Message` union

```go
type Message struct {
	Role  Role // RoleUser or RoleAssistant
	Parts []Part
}
```

`Part` is a closed union — `Text`, `Reasoning`, `ToolCall`, `ToolResult`, `Finish` — rather
than a struct with a kind field and a dozen optional members, which is the shape `Event` uses
and would let a backend read a tool call's arguments off a piece of prose. A conversation that
could only carry a string dropped every tool call it ever made on the next resume, and
cross-call prompt caching could never hit, because a replayed prefix can never byte-match a
request whose tool blocks were thrown away going in.

Each backend converts the union at its own edge, because the two wire schemas do not agree on
where a tool result lives: Anthropic carries it as a block inside the *user* turn; the OpenAI
schema (OpenRouter) gives it a message of its own. Reconciling that once, per backend, is the
job — not a third shape in the core that neither wire format actually is.

`Agent.Stream` refuses a conversation before either backend sees it if a `ToolCall` sits
outside `RoleAssistant` or a `ToolResult` sits outside `RoleUser`. Left unchecked, the two
backends disagree about what a mismatch does — Anthropic sends it and lets the API reject the
request, OpenRouter drops it in silence — and checking centrally is what keeps this package's
"fails rather than degrading" promise for the one part of the union a caller can get wrong.
It is also, deliberately, the *only* place this check lives: maintaining the same union
validation in more than one place is a trap a comparable project's own design notes describe
running into.

## The tool-call loop

```go
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Run(ctx context.Context, input json.RawMessage) (string, error)
}
```

`Tool` is this package's own interface rather than either SDK's, because a tool has to be
callable by every backend — an Anthropic-shaped tool type in the core would mean the
OpenRouter backend converting from a vocabulary that has nothing to do with it. `NewTool`
builds one from a plain Go function, generating its JSON Schema from the input struct's tags.

A tool's error is not fatal to the run: it is reported to the caller through `ToolSink` and
handed back to the model as the tool's result, which is usually better placed than the caller
to decide whether the task can still be finished.

## Retry

`Retry(backend, options)` wraps a `Backend`, and is deliberately not a backoff engine — both
SDKs already retry at the HTTP level (connection failures, 408/409/429/5xx), honouring
`Retry-After`. What no HTTP-level retry can see is a provider that answers 200 and puts the
failure in the body: OpenRouter reports a rate limit as an error object inside the SSE, and an
Anthropic `overloaded_error` can arrive mid-stream on a response whose status was committed
long ago. Both reach a caller as a dead stream carrying a transient failure, and retrying
*those* is what this adds.

It retries only while nothing has been yielded to the consumer. Once a text delta has reached
them, they have already printed it, and no wrapper can un-print it — a failure after the first
event ends the run and is reported as it is.

`RetryOptions.Budget` is the only real bound on a run's duration, and the reason it is a context
deadline rather than a stopwatch is worth stating once. `Max` caps this wrapper's own backoff.
The sleeps that actually cost the time are the SDKs waiting out a `Retry-After` *inside* an
attempt, before the failure is handed up as transient at all — six of them across three attempts
over three HTTP retries. Reading a clock between attempts would notice those six minutes only
once they had been spent. They are a `select` on `ctx.Done()`, so a deadline derived once and
passed down is the one thing that interrupts a sleep already in progress.

## MCP: who dials, and why that is the whole distinction

There are two ways to reach an MCP server here, and the axis that separates them is not local
versus remote. It is **who opens the connection**.

`mcp.Server` is handed to the Claude API, which dials it from its own side of the request. That is
why the `mcp` package implements none of the protocol — there is no client half to write — and why
`Capabilities.MCP` exists: `openrouter` has no equivalent connector, so a config carrying one is
refused rather than run with less.

`mcp/client` dials from here. `Command` starts a subprocess and speaks over its stdin and stdout;
`Remote` opens a Streamable HTTP session to a URL. Either way the tools are bridged to
`nacelle.Tool`, and **the bridge is the design decision**. A bridged tool is an ordinary local
tool, so it passes through `Config.Approve`, emits the same `KindToolCall`/`KindToolResult`
events, and works on both backends. A parallel MCP-shaped path through the loop would have had to
reimplement each of those, slightly differently.

That leaves one genuine overlap: a hosted server can be reached either way on Anthropic. Dialling
it here costs your egress, once per call, and buys a tool set that does not change shape when
someone passes `-backend openrouter`, plus an approval gate over tools the connector would have
run out of reach. Anything that lets a person switch backends should prefer `Remote`; a service
pinned to Anthropic that would rather not carry the traffic should prefer `mcp.Server`.

`mcp/client` is a sub-package for a mechanical reason: the root package imports `mcp`, so `mcp`
cannot import it back, and a bridged tool that is not a `nacelle.Tool` is usable by nothing.
`Server` is a sealed interface rather than one struct with a mode field, because a subprocess and
an endpoint share almost no configuration — folding them together gives every caller a struct
where two thirds of the fields are wrong for the server they are describing.

This does widen what an agent can do, and the widening is worth naming. Running a `Command` is
executing an arbitrary program, chosen by whoever writes the configuration — not by the model,
which is the line that matters. The environment is not inherited (`PATH`, `HOME`, and whatever
`Command.Env` names), for the same reason `tools/` refuses to inherit for commands, only more so:
that code is ours and this code is somebody else's, and it is about to be handed model-chosen
arguments. What confinement is *not* applies here exactly as it does in `tools/` — a subprocess
is not a sandbox, and if the servers are untrusted the isolation has to be a container or a VM.

Which is also why nothing here **discovers** a config file. A `.mcp.json` names executables to
run, so reading one just because it is in the working directory is strictly worse than the
project-local `SKILL.md` case, and that one is already gated behind `~/.nacelle/trust.json`. Every
server this client opens was named on the command line or in the user's own configuration.

## Prompt caching

The `anthropic` backend sets a cache breakpoint only when a second request will actually share
the prefix — local tools present (the SDK's runner resends the whole conversation on every
tool iteration) or a conversation handed in (an earlier run wrote this prefix). A cache write
costs 1.25× a plain input token and a read costs 0.1×, so a breakpoint on a genuine one-shot
call — no tools, no history — would buy a cache entry nothing will ever read. MCP tools
deliberately do not count toward the decision: they run inside a single response on
Anthropic's own side, so a run with only remote tools still makes exactly one request.

`Usage.CacheHitRate()` reports the share of input a run actually read from cache, excluding
write tokens from the denominator — a write is not a hit, and counting it as a miss would
understate the rate on a run's very first turn, before anything written could have been read
back yet.

## What is deliberately not here

No sandbox, no sessions, no auth, no persistence layer. `nacelle` is a flat library, like
`tronc` — no database, no HTTP surface, no UI of its own. See [`ROADMAP.md`](../ROADMAP.md) at
the repo root for what is scoped in and explicitly out.

The one piece of trust-gating in this codebase lives outside it entirely, in `tui/`: a
project's `.agents/skills/` is not read until something has trusted it, because a `SKILL.md`
can instruct the model to run scripts it ships alongside itself. Plain instruction text
(`AGENTS.md`, `CLAUDE.md`, global or project) is not gated the same way — the model reads
arbitrary file content constantly as part of ordinary tool use, and gating one specific text
file while every other one stays ungated would not close a real hole, only add friction. See
[configuration.md](configuration.md#context-skills-and-jardin-tools).
