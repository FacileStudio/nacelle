# nacelle

The Go agent SDK for the [Facile Studio](https://facile.studio) suite — the model loop, the
tool registry and the MCP wiring that every Facile agent needs and none of them should be
re-writing.

An agent is a loop around a model with tools attached. That loop is about two hundred lines,
and it gets rewritten in every project that needs one, slightly differently, with a slightly
different bug in the tool-result handling. `nacelle` is the single version.

> **Status: the core works, nothing consumes it yet.** The agent loop, the tool registry, the
> MCP wiring and per-turn usage are implemented and tested. `tools/`, `tui/` and `sandbox/` are
> not written. Untagged — the API moves until Kori has used it.

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
nacelle/                 core: agent loop, events, tools, usage          [built]
  mcp/                   MCP server connections and credentials          [built]
  tools/                 the built-in local tool set (read, write, edit, bash, glob, grep)
  tui/        (submodule) terminal client — Bubble Tea lands here and nowhere else
  sandbox/    (submodule) container and microVM execution for Atelier
```

A backend importing `nacelle` and `nacelle/mcp` gets the loop and nothing else. `tui/` and
`sandbox/` are separate Go modules for the same reason `tronc/migrate` and `tronc/testdb`
are: an import should not cost you a dependency you will never call.

## Core shape

```go
agent, err := nacelle.New(nacelle.Config{
    System: "You are Kori…",
    Effort: nacelle.EffortHigh,
    Tools:  []nacelle.Tool{searchEvents},
    MCP:    []mcp.Server{{Name: "perception", URL: "https://perception.facile.studio/api/mcp"}},
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

Defaults: `claude-opus-5`, adaptive thinking, 32k output per turn. `Config.Client` is injectable
for a shared client, a proxy, or a test transport.

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
| Anthropic only, no provider interface yet | See OpenRouter below — the second backend is a rewrite, not a base URL |
| TUI and sandbox are separate modules | A backend must not inherit Bubble Tea or a container runtime |

### OpenRouter, and what a second backend actually costs

Atelier already runs on OpenRouter, so it is the concrete second backend this design has to
answer for. It is not a base-URL swap. **OpenRouter speaks the OpenAI chat-completions schema
and exposes no `/v1/messages`**, verified against its API reference — so `anthropic-sdk-go`
cannot reach it at all.

That matters more than it sounds, because the two things that make this package small are
Anthropic-API features:

| | Anthropic | OpenRouter |
|---|---|---|
| Tool loop | `toolrunner` ships in the SDK | hand-rolled, per backend |
| Remote MCP servers | the API connects to them itself | needs a real MCP client written here |
| Adaptive thinking, effort | yes | no equivalent |

So an OpenRouter backend is a second implementation of the whole loop, not an adapter. That is
worth doing when Atelier needs to compare models through nacelle — and it is worth **not**
doing before Kori has run on the Anthropic path, because building two backends before either
has a consumer is how the interface ends up fitting neither.

When it lands, the shape should be **capability-aware, not lowest-common-denominator**: a
consumer that needs MCP should fail to start against a backend that has none, rather than
silently losing its tools. Flattening the two into one interface that quietly supports less is
the failure mode to avoid.

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

1. ~~`go mod init`, the suite quality gate, the core loop, `mcp/`.~~ Done.
2. `tools/` — the local tool set, with the path and shell safety checks written once.
3. Consume it from Kori. That is the first real test of the API, and the point at which it
   gets tagged `v0.1.0`. Expect the API to move before then.
4. `tui/` and `sandbox/`, driven by the `pi` replacement and Atelier respectively — and by
   what they actually turn out to need, not by this list.
5. An OpenRouter backend, once Atelier asks for it and Kori has proven the Anthropic path.

## Gate

`sh scripts/check.sh` — gofmt, vet, test. `filet check .` on top, and it is expected to be
silent: the loop was refactored to satisfy it rather than the other way round.
