# nacelle

The Go agent SDK for the [Facile Studio](https://facile.studio) suite — the model loop, the
tool registry and the MCP wiring that every Facile agent needs and none of them should be
re-writing.

An agent is a loop around a model with tools attached. That loop is about two hundred lines,
and it gets rewritten in every project that needs one, slightly differently, with a slightly
different bug in the tool-result handling. `nacelle` is the single version.

> **Status: two backends and the local tool set work; nothing consumes them yet.** Anthropic,
> OpenRouter, `tools/`, and per-turn usage are implemented and tested. `tui/` and `sandbox/`
> are not written. Untagged — the API moves until Kori has used it.

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
  tui/        (submodule) terminal client — Bubble Tea lands here and nowhere else
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

Defaults: `claude-opus-5`, adaptive thinking, 32k output per turn.

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

1. ~~The gate, the core loop, `mcp/`, `tools/`, and both backends.~~ Done.
2. Consume it from Kori. That is the first real test of the API, and the point at which it
   gets tagged `v0.1.0`. Expect the API to move before then.
3. `tui/` and `sandbox/`, driven by the `pi` replacement and Atelier respectively — and by
   what they actually turn out to need, not by this list.

## Gate

`sh scripts/check.sh` — gofmt, vet, test. `filet check .` on top, and it is expected to be
silent: the loop was refactored to satisfy it rather than the other way round.

## Licence

Apache License 2.0 — see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

Apache rather than MIT for one practical reason: `nacelle` expects to carry code from
Apache-licensed neighbours such as [`charm.land/fantasy`](https://github.com/charmbracelet/fantasy),
and matching licences keeps the repo under one set of terms instead of two. The patent grant is a
welcome extra. Attribution for anything vendored belongs in `NOTICE`.
