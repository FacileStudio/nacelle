# nacelle

The Go agent SDK for the [Facile Studio](https://facile.studio) suite — the model loop, the
tool registry and the MCP wiring that every Facile agent needs and none of them should be
re-writing.

An agent is a loop around a model with tools attached. That loop is about two hundred lines,
and it gets rewritten in every project that needs one, slightly differently, with a slightly
different bug in the tool-result handling. `nacelle` is the single version.

> **Status: design, no code yet.** This README is the contract the first commit implements.
> It is written for whoever picks the work up, including a fresh agent with no context.

## Who it is for

Three consumers exist before a line is written, which is the reason this is a library and not
a package inside one app:

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
nacelle/                 core: agent loop, messages, tools, streaming, usage
  provider/              model backends; anthropic first
  mcp/                   MCP server connections and credentials
  tools/                 the built-in local tool set (read, write, edit, bash, glob, grep)
  tui/        (submodule) terminal client — Bubble Tea lands here and nowhere else
  sandbox/    (submodule) container and microVM execution for Atelier
```

A backend importing `nacelle` and `nacelle/mcp` gets the loop and nothing else. `tui/` and
`sandbox/` are separate Go modules for the same reason `tronc/migrate` and `tronc/testdb`
are: an import should not cost you a dependency you will never call.

## Core shape

```go
agent := nacelle.New(nacelle.Config{
    Model:  provider.Anthropic("claude-opus-5"),
    System: "You are Kori…",
    Tools:  []nacelle.Tool{searchEvents, getEntity},
    MCP:    []mcp.Server{{Name: "perception", URL: "https://perception.facile.studio/api/mcp"}},
})

for event := range agent.Stream(ctx, conversation) {
    // text deltas, tool calls, tool results, usage, done
}
```

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
| A provider interface, with Anthropic implemented first | Atelier's whole point is comparing configs; but see the risk below |
| TUI and sandbox are separate modules | A backend must not inherit Bubble Tea or a container runtime |

### The one real risk: the provider abstraction

Multi-provider is where agent SDKs go to die. Tool-call semantics, thinking blocks, streaming
event shapes and system-prompt handling all differ per provider, and the usual outcome is an
interface that fits the first provider and leaks for every one after it.

Mitigation: **build Anthropic-only first and ship it.** Do not design `provider.Provider`
until a second backend is actually being added for Atelier, and design it against that
concrete second case rather than against an imagined third. A working single-provider SDK is
worth more than a generic one that nobody has run.

## Next steps

1. `go mod init`, the suite quality gate (`scripts/check.sh`, `filet.yml`, `mise.toml`), CI.
2. Core loop against `anthropic-sdk-go` with the SDK tool runner, streaming, usage. No
   provider interface yet.
3. `mcp/` — server config, credentials, the connector's two-halves request shape.
4. `tools/` — the local tool set, with the path and shell safety checks written once.
5. Consume it from Kori. That is the first real test of the API, and the point at which it
   gets tagged `v0.1.0`.
6. `tui/` and `sandbox/`, driven by the `pi` replacement and Atelier respectively — and by
   what they actually turn out to need, not by this list.
