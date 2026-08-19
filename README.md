# nacelle

The Go agent SDK for the [Facile Studio](https://facile.studio) suite — the model loop, the
tool registry and the MCP wiring that every Facile agent needs and none of them should be
re-writing.

An agent is a loop around a model with tools attached. That loop is about two hundred lines,
and it gets rewritten in every project that needs one, slightly differently, with a slightly
different bug in the tool-result handling. `nacelle` is the single version.

> **Status: it has its first consumer.** Both backends, `tools/`, retry, prompt caching and
> per-turn usage are implemented and tested, and `tui/` runs on them. `sandbox/` is not
> written. Untagged — the API moves until Kori has used it too.

## Who it is for

Three consumers, which is the reason this is a library and not a package inside one app:

| Consumer | Shape | What it needs |
|---|---|---|
| **Kori** (Perception) | headless, inside a Go API | core loop, MCP tools, streaming that maps onto SSE, transcripts |
| **Atelier** | agents in isolated container boxes, best-of-N | swappable models, per-run cost and token accounting, a sandbox it can spin and reap |
| **A `pi` replacement** | terminal coding agent | local read/write/edit/bash tools, a TUI, config and profiles |

They want the same core and three different skins. The design brief follows from that: **a
backend must be able to import the core without pulling in a terminal UI or a container
runtime.**

## Layout

The tronc shape — flat packages at the root, one module, heavy dependencies pushed into
separate submodules so they land only on the apps that want them.

```
nacelle/                 core: Agent, Backend seam, events, tools, usage   [built]
  anthropic/             backend: SDK tool runner + server-side MCP        [built]
  openrouter/            backend: hand-rolled loop, 400+ models, real cost [built]
  mcp/                   MCP server connections and credentials            [built]
  tools/                 local tool set: read, write, edit, find, search, run [built]
  tui/        (submodule) terminal client — Bubble Tea lands here and nowhere else [built]
  sandbox/    (submodule) container and microVM execution for Atelier
```

A backend importing `nacelle` and `nacelle/mcp` gets the loop and nothing else. `tui/` and
`sandbox/` are separate Go modules for the same reason `tronc/migrate` and `tronc/testdb`
are: an import should not cost you a dependency you will never call.

## Core shape

```go
agent, err := nacelle.New(nacelle.Config{
    Backend: anthropic.New(anthropic.Config{}),
    System:  "You are Kori…",
    Effort:  nacelle.EffortHigh,
    Tools:   []nacelle.Tool{searchEvents},
    MCP:     []mcp.Server{{Name: "perception", URL: "https://perception.facile.studio/api/mcp"}},
})

for event, err := range agent.Stream(ctx, conversation) {
    if err != nil {
        return err
    }
    switch event.Kind {
    case nacelle.KindText:
        io.WriteString(w, event.Text)
    case nacelle.KindToolCall:
        log.Println("calling", event.Tool.Name, event.Tool.Input)
    case nacelle.KindDone:
        log.Println("spent", event.Usage.Total(), "tokens")
    }
}
```

A tool is a Go function; its schema comes from the struct tags, so a field is described where
it is declared rather than in a JSON literal that drifts from it:

```go
type searchInput struct {
    Query string `json:"query" jsonschema:"required,description=What to look for"`
}

searchEvents, err := nacelle.NewTool("search_events", "Find events matching a question",
    func(ctx context.Context, in searchInput) (string, error) { … })
```

Defaults: `claude-opus-5`, adaptive thinking, 32k output per turn, and prompt
caching always on.

Caching is not a knob because there is no run this package makes where turning it
off pays. A cache write costs 1.25x a plain input token and a read costs 0.1x, so it
is ahead from the second request sharing a prefix — and the tool runner resends the
whole conversation every iteration, so any run that calls a tool has already made
that request. Over ten turns it is the difference between paying for the system
prompt and tool schemas once and paying for them ten times. Tools are sorted by name
for the same reason: they render at the front of the prefix, so leaving them in the
caller's order would make two identically-configured agents miss each other's cache
with nothing in the result to say why.

## Backends, and the capability rule

There is no default backend. A package that picks one for you hides the most consequential
line in the configuration, and this is a multi-model SDK.

| | `anthropic` | `openrouter` |
|---|---|---|
| Tool loop | the SDK's runner | hand-rolled here |
| Remote MCP servers | ✅ the API calls them itself | ❌ no equivalent in the schema |
| Streamed reasoning | ✅ | ✅ `delta.reasoning` |
| Effort | ✅ | ✅ mapped to `reasoning.effort` |
| **Reports cost in dollars** | ❌ tokens only | ✅ `usage.cost` |
| Models | Anthropic's | 400+ behind one slug |

**Asking for something a backend lacks is refused at construction**, not silently dropped:

```go
_, err := nacelle.New(nacelle.Config{Backend: openrouter.New(...), MCP: servers})
// *nacelle.Unsupported: backend "openrouter" does not support MCP servers
```

That is the whole reason `Capabilities` exists. Losing MCP tools quietly looks like a model
that will not use its tools, and you can spend an afternoon on that.

## Retrying

```go
backend := nacelle.Retry(openrouter.New(...), nacelle.RetryOptions{})
```

The zero `RetryOptions` is the recommended policy, not the absence of one: three
attempts, 500ms doubling to a 8s ceiling, with jitter. `Attempts: 1` turns it off.

**This is not a backoff engine, on purpose.** Both SDKs already retry at the HTTP
level — connection failures, 408, 409, 429, 5xx, honouring `Retry-After-Ms` and
`Retry-After` — and that covers establishing a stream, streaming requests included.
Writing another one here would be a worse copy of code we already ship.

What it adds is the case no HTTP retry can see: **a provider that answers 200 and
puts the failure in the body.** OpenRouter reports a rate limit as an error object
inside the SSE; an Anthropic `overloaded_error` can arrive mid-stream on a response
whose status was committed long before. Both reach a caller as a dead stream, and
neither is visible to anything classifying on a status code.

Two details that decide whether this works at all:

- **The in-band code is a number.** OpenRouter sends `"code": 429`, not `"429"`. A
  decoder expecting a string drops the field without failing the parse, so the
  classifier reads an empty code and calls every rate limit permanent — a retry that
  looks implemented and never fires. `TestAnInBandRateLimitIsRetryable` pins it.
- **Retrying stops the moment anything is yielded.** A consumer that has seen a text
  delta has already printed it, and no wrapper can un-print it. A failure after the
  first event ends the run and is reported as it is.

Backends classify their own provider's vocabulary and mark what is worth another go
with `nacelle.Transient`; the core knows only `Retryable(error) bool`. Your own error
type joins the scheme by implementing `Retryable() bool`.

### Settings

```yaml
# ~/.nacelle.yml
backend: openrouter
model: deepseek/deepseek-v4-flash-0731
bash: false
max_iterations: 40
```

**Precedence is flag > `NACELLE_*` env var > file > default, resolved in one
function.** A sibling CLI in this suite read its environment inside a branch that
ignored its config file, which turned what the README called overrides into two
mutually exclusive modes — four copies of a precedence chain are four chances to
disagree.

Two details that are load-bearing rather than fussy:

- **Only the flags you actually typed count.** Go's `flag` package cannot tell a
  flag left alone from one passed its own default afterwards, so `flag.Visit` is
  what stops a default silently outranking the file it is meant to sit beneath.
- **Toggles are pointers.** A layer saying nothing and a layer saying `false` are
  different answers, and a `bool` cannot tell them apart. Strings use empty for
  the same purpose, which is safe because no setting here has a meaningful empty
  value.

**No credentials in it, deliberately.** They already have homes — the environment,
and the Anthropic SDK's own profile from `ant auth login`. A file with a key in it
is a file that can never be committed to a dotfiles repo, which is the only reason
to want one of these on two machines.

## The local tool set

```go
set, err := tools.New(tools.Config{Root: repo, AllowBash: true})
defer set.Close()
local, err := set.Tools()   // or set.ReadOnly() for an agent that only answers questions
```

`read_file`, `write_file`, `edit_file`, `find_files`, `search_files`, and `run_command` when
`AllowBash` is set. Every file operation goes through [`os.Root`], so a path resolving outside
the root is refused by a kernel-backed check rather than a string comparison — which is what
closes symlink escapes, `..` that survives normalisation, and the check-then-use window.

Three rules it is worth knowing before extending it:

- **The root comes from the host, never from a tool argument.** No tool takes a `cwd`, `dir`
  or `root`, and a test enforces it. CVE-2025-59532 is why: Codex CLI accepted a
  model-generated working directory as its sandbox root, so the output being confined was also
  choosing the confinement.
- **`edit_file` requires its target text to match exactly once.** Zero matches means the model
  misremembered the file; more than one means it is about to change places it never looked at.
  Both are caught for free, and the refusal teaches the fix — send more surrounding context.
- **This is not a sandbox and does not pretend to be.** `os.Root` concedes bind mounts, `/proc`
  and device files; `run_command` is not confined at all and no denylist survives contact with
  a shell. `AllowBash` is opt-in so that choice is made on purpose, and real isolation is a
  container's job — which is what `sandbox/` will be for.

Commands run with a scrubbed environment (`PATH` and `HOME`, nothing else), in their own
process group so a timeout kills the children too, with every output capped and truncation
announced rather than silent.

[`os.Root`]: https://pkg.go.dev/os#Root

Three things that are not negotiable, because they are what makes the core embeddable:

- **The loop returns events, it does not print.** Every surface — SSE, a TUI, a log file, a
  test — is a consumer of the same typed event stream. Nothing in the core writes to stdout.
- **Usage is reported per turn, always.** Atelier compares runs on cost; a token count that
  has to be reconstructed afterwards is a token count nobody trusts.
- **The core knows nothing about any product.** No events, no entities, no citations, no
  repositories. If a consumer needs vocabulary in the core, the abstraction is wrong.

## Decisions

| Decision | Why |
|---|---|
| Go, and the suite conventions | Every consumer is a Go service; tronc and porte set the shape |
| The SDK's tool runner, not a hand-rolled loop | `anthropic-sdk-go` ships `toolrunner`; hand-rolling is strictly more code and more bugs |
| The API's MCP connector for remote servers, not an MCP client | `mcp_servers` + `mcp_toolset` calls remote servers server-side; the client half is only needed for stdio servers |
| `claude-opus-5`, adaptive thinking, no `budget_tokens` | `budget_tokens` is a 400 on Opus 5; `output_config.effort` replaces it |
| The backend seam is the whole loop, not one request | Anthropic ships a runner and server-side MCP; a backend without them must drive the loop itself. A request-level seam would have forced the Anthropic path to give up a tested loop to look symmetrical with one that cannot have it |
| Capabilities are named features, not tiers | A consumer needs to know MCP specifically is missing, not that a backend is "limited" |
| `openai-go` for the OpenRouter transport | The SSE parsing is where the bugs live — its comment/blank-line handling is the fix for the exact `: OPENROUTER PROCESSING` crash. Module pruning keeps it to four indirect deps |
| Prompt caching on, with no way off | Break-even is two requests sharing a prefix; a tool-using run always makes them |
| Retry wraps the Backend, and is not a backoff engine | The SDKs already retry HTTP properly; what they cannot see is a failure delivered inside a 200 |
| TUI and sandbox are separate modules | A backend must not inherit Bubble Tea or a container runtime |

### Notes from building the OpenRouter backend

Four things that cost time and are not obvious from the sample code:

- **`stream_options: {include_usage}` and `usage: {include: true}` are deprecated and inert.**
  Usage is always returned now. Most tutorials and most training data still send them.
- **Usage arrives in the final chunk, whose `choices` array is empty.** Indexing it unguarded
  panics on every run, not occasionally.
- **`: OPENROUTER PROCESSING`** is a real SSE keepalive; passing it to a JSON decoder throws.
  The blank line after it is the second half of the same trap, and together they were a
  filed-and-fixed crash in `openai-go` — which is a good argument for not writing your own
  SSE parser.
- **The tool schema must go on every request**, including the one carrying tool results.
  OpenRouter validates it per call, and a follow-up without it is a different conversation.

`reasoning_details` is echoed back untouched on the assistant message, because reasoning
models require the sequence to match what they produced — a tool loop that drops it loses the
model's train of thought exactly when it is waiting on a tool result.

## The terminal client

```sh
go run ./tui -root . -bash
go run ./tui -backend openrouter -model anthropic/claude-opus-5
```

`tui/` is a separate Go module, so importing `nacelle` never costs you Bubble Tea. It is the
SDK's first consumer and that is its job: a terminal exercises every event kind a backend can
produce — text, reasoning, a tool starting, a tool finishing, what a turn cost — while someone
is watching, which is more of the contract than a headless caller touches.

It is deliberately small. Sessions, profiles, themes and panes are what a product grows, and
none of them test the API. The point is to find where the API is annoying before three
consumers depend on it — and it has already found things:

- ~~`openrouter` never emits `KindToolCall`.~~ Fixed. It also turned out that announcing calls
  without reporting a failure for an unknown tool would have left an orphan call, so that is
  closed too.
- ~~`anthropic` emits `KindToolResult` with an empty `Tool.ID`.~~ Fixed. The SDK never hands a
  tool its call id, so the pairing is rebuilt from the stream side; see `anthropic/invocation.go`.
- **`Message` is text and a role, so a tool call cannot be replayed to the model.** Still open,
  and the largest thing left — the client keeps the answer and drops the tool history, because
  there is nowhere to put it. Tracked as A6 in [`ROADMAP.md`](ROADMAP.md).

Three problems for one week-old client, two of them now closed. That is what a first consumer
is for.

## Not a port of `pi`

`pi` ([`earendil-works/pi`](https://github.com/earendil-works/pi), MIT) is the Node coding agent
this suite runs today — Atelier already uses it as a harness class. The obvious shortcut is to
fork it or port it to Go. **We are not doing either.**

Forking keeps us on Node, and Kori has to embed inside a Go API. Porting means months spent
rebuilding provider plumbing and a streaming loop that `anthropic-sdk-go`'s tool runner already
gives away — and it lands us with somebody else's tool surface.

That last part is the actual reason. **We are writing our own harness so it wires into our own
tools.** A ported `pi` knows about files and shells. Ours has to reach Perception's MCP server,
Opus, Sablier, Casier and Antenne as first-class citizens, run inside Atelier's boxes reporting
cost per run, and be embeddable in a Go backend. That is not a fork of a coding agent with
extra tools bolted on; it is a different shape, and building it from the SDK up is the shorter
path to it.

Read pi where it has already solved a problem — its client/protocol split is a good answer to
"how does one core serve a TUI and a backend", and `docs/containerization.md` is written by
someone who put an agent in a box before us. Take ideas, cite them, write our own code.

## Next steps

The ordered version, with exact files and exit criteria, is in [`ROADMAP.md`](ROADMAP.md).


1. ~~The gate, the core loop, `mcp/`, `tools/`, and both backends.~~ Done.
2. Consume it from Kori. That is the first real test of the API, and the point at which it
   gets tagged `v0.1.0`. Expect the API to move before then.
3. ~~`tui/`, at floor scope.~~ Done, and it is already reporting API problems.
4. Widen `Message` so a conversation can carry tool calls, then `sandbox/` for Atelier —
   driven by what they actually turn out to need, not by this list.

## Gate

`sh scripts/check.sh` — gofmt, vet, test. `filet check .` on top, and it is expected to be
silent: the loop was refactored to satisfy it rather than the other way round.

## Licence

Apache License 2.0 — see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

Apache rather than MIT for one practical reason: `nacelle` expects to carry code from
Apache-licensed neighbours such as [`charm.land/fantasy`](https://github.com/charmbracelet/fantasy),
and matching licences keeps the repo under one set of terms instead of two. The patent grant is a
welcome extra. Attribution for anything vendored belongs in `NOTICE`.
