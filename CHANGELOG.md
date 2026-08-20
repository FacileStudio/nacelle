# Changelog

All notable changes to `nacelle` are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow semver —
while on `v0`, a breaking change bumps the minor.

## [Unreleased]

Untagged. Everything below is the first day: the core loop, both backends, the local tool
set, the TUI, and the gate, in the order they were built.

### Added

- **The core loop** — `Agent`, `Backend`, `Request`, `Event`. The loop returns events and
  never prints, so a backend streaming SSE, a terminal UI and a test are all consumers of one
  stream. Usage is reported on every turn, not only at the end, because comparing runs on
  cost is a reason this package exists. `Capabilities` gates what a config may ask for — MCP,
  streamed thinking, effort levels — and `nacelle.New` refuses a config the backend cannot
  honour rather than running it with less.
- **`anthropic`** — the full-capability backend. It runs on the Anthropic SDK's own tool
  runner rather than a hand-rolled loop, which is also where its MCP support comes from:
  Anthropic connects to remote MCP servers on its own side of the request, so this backend
  never implements the protocol itself.
- **`openrouter`** — hundreds of models behind one OpenAI-compatible endpoint, at the cost of
  MCP (there is no server-side connector to drive) and of driving the tool loop by hand. In
  return, `Usage.Cost` is real: OpenRouter prices every generation and returns the figure,
  which the Anthropic API does not.
- **Prompt caching**, on the Anthropic backend. A cache breakpoint is set only when a second
  request will actually share the prefix — local tools, or a conversation handed in — because
  a one-shot run with a breakpoint pays 1.25× its input for a cache entry nothing will ever
  read.
- **Retry**, as a `Backend` decorator (`nacelle.Retry`). Both SDKs already retry at the HTTP
  level; what neither can see is a provider that answers 200 and puts the failure in the
  body — OpenRouter's rate limit inside the SSE, an Anthropic `overloaded_error` mid-stream.
  It retries only while nothing has reached the consumer: once a text delta has been printed,
  no wrapper can un-print it.
- **`tools`** — local read, write, edit, find (glob), search (grep) and (opt-in) command execution, confined
  to one directory through `os.Root` so a path resolving outside it is refused by the kernel
  rather than by string comparison, not a symlink, a `..`, or an absolute path escaping the
  tree. The directory is fixed by the host at construction; no tool takes a `path`, `cwd` or
  `root` argument a model could nominate itself.
- **`mcp`** — the server list an Anthropic-backed agent may reach, and nothing else. `Server`
  holds a name, a URL, a bearer token and an optional allow-list; `Validate` refuses two
  servers sharing a name before either backend ever sees the config.
- **`tui/`** — the SDK's first consumer, and the reason several of the entries below exist: a
  terminal exercises every event kind a backend can produce, and does it while someone is
  watching. Scrolls with pgup/pgdown/up/down, following the stream only while already at the
  bottom. Renders answers as markdown (`charm.land/glamour`), picking light or dark from the
  terminal's own reported background rather than assuming one. Shows a spinner for the gap
  between a question and the first token. Reads `~/.nacelle.yml` beneath `NACELLE_*`
  environment variables beneath command-line flags, in that order, and holds no credentials —
  those already have two homes.
- **`Message` widened into a content-part union** — `{Role, Parts []Part}`, where `Part` is a
  closed set of `Text`, `Reasoning`, `ToolCall`, `ToolResult` and `Finish`. Before this, a
  conversation could carry only prose: a resumed transcript dropped every tool call it ever
  made, and cross-call prompt caching could never hit, because a replayed prefix can never
  byte-match a request whose tool blocks were thrown away on the way in. Both backends convert
  the union at their own edge — Anthropic mirrors the shape directly, OpenRouter splits a tool
  result into a message of its own, the way its schema wants it. `Agent.Stream` refuses a
  conversation that puts a part on the wrong side of its role (a `ToolCall` outside
  `RoleAssistant`, a `ToolResult` outside `RoleUser`) before either backend sees the request,
  rather than letting the two backends disagree about what a mismatch does.
- **`Usage.CacheHitRate`** — the share of input a run read from cache rather than paid full
  price for. Excludes cache-write tokens from the denominator: a write is not a hit, and
  counting it as attempted-but-missed would understate the rate on a run's first turn, before
  anything written could have been read back yet.
- **The quality gate**: `scripts/check.sh` runs gofmt, `go vet`, `go test -race` and
  `golangci-lint` across the root module and `tui/`, the latter with `GOWORK=off` so the
  nested module resolves its own `go.mod` requires the way a stranger installing it would,
  rather than silently reading local source through the workspace. `filet.yml` carries an
  `architecture` block forbidding the two backends from importing each other and forbidding
  `tools`/`mcp`/`tui` from importing a provider SDK directly. CI runs the same gate on push
  and pull request and cannot be bypassed the way the pre-push-only hook could.
- **`docs/`** and this changelog.
- **`Backend.CountTokens`** (`Agent.CountTokens` on top of it) — real on `anthropic`, through
  the SDK's own `count_tokens` endpoint; refused with `*Unsupported` on `openrouter`, which
  has no server-side endpoint and would otherwise mean guessing at a tokenizer for whichever
  of the hundreds of models behind the gateway answered.
- **`Trim`** — a boundary-safe context-truncation primitive. Deliberately not a summarizer:
  deciding what to keep and how to compress it is a product opinion this package holds
  nowhere else, so `Trim` only does the mechanical part safely — it never returns a slice
  that keeps a `ToolResult` message without the `ToolCall` before it, which every provider
  this package talks to would otherwise reject outright.
- **`tools.Mycelium`** — `list_flows`, `run_flow` and `search_memory`, giving a model narrower,
  more legible access to this machine's [mycelium](https://github.com/FacileStudio/mycelium)
  flows and memory search than routing the same commands through `run_command` would. Absent
  mycelium on `PATH` is not an error; it returns no tools, the same as `AllowBash` being off.
- **`tui/` reads `~/.agents/AGENTS.md`**, the [AGENTS.md standard](https://agentsstandard.com)'s
  own global-base path — the same file Codex, Cursor, Copilot, Gemini and pi (at its own
  equivalent) already read. Deliberately not a user's `~/.claude/CLAUDE.md`: that file assumes
  Claude Code's own tools, hooks and slash commands, where an `AGENTS.md` at the standard's
  own path is written, by the convention's premise, to make sense to whichever agent reads it.
- **`tui/` loads skills from `~/.agents/skills/` and trusted `.agents/skills/` directories**,
  following the [Agent Skills specification](https://agentskills.io/specification). Only a
  skill's name and description ever reach the system prompt; the model reads the rest with its
  own `read_file` call once it decides a skill applies, so no new tool was needed to invoke
  one. Global skills load unconditionally, the same reasoning as the global `AGENTS.md` above;
  project-local skills require trust first — a `SKILL.md` can instruct the model to run
  scripts it ships alongside itself, unlike the plain instruction text `AGENTS.md`/`CLAUDE.md`
  carry, and that is the one thing in this package's context loading that is gated on purpose.
  `-trust-skills` trusts every `.agents/skills` directory found under `-root` for one run and
  remembers the decision in `~/.nacelle/trust.json`, keyed by canonical directory — the first
  thing to give `~/.nacelle/` a reason to be a directory rather than the flat `~/.nacelle.yml`
  file it had been.
- **`-skill-dir`** (repeatable), `NACELLE_SKILL_DIRS` (colon-separated) and `skill_dirs` in
  `~/.nacelle.yml` — extra directories to load skills from, alongside `~/.agents/skills/`. The
  Agent Skills specification defines the `SKILL.md` file, not where it lives on disk, so every
  tool picks its own directory; this is how nacelle sees one that belongs to another tool (a
  Claude Code `~/.claude/skills/`, say) without moving or copying anything into its own — the
  same problem pi itself solves with a `skills` array in its own settings. No trust gate,
  same reasoning as the global `~/.agents/skills/` above: naming a directory here is something
  only the person running nacelle, on their own machine, can already do.
- **`/skill:name`** runs a loaded skill directly, instead of waiting on the model to decide on
  its own that it applies — the same shortcut pi's own `/skill:name` offers. Sends the skill's
  `SKILL.md` content as the question; anything typed after the name is appended as
  `User: <text>`, pi's own convention, so `/skill:pdf-tools extract the tables` tells the skill
  what to do rather than only that it applies. Namespaced under `skill:` on purpose — the three
  built-in commands (`/clear`, `/help`, `/quit`) can never collide with a skill of the same
  name. Unlike those three, `/skill:name` **does** start a run.
- **`Config.Approve`** — a per-call tool approval hook (`func(ctx, name, input) bool`), plumbed
  through `Request` and `ToolSink` rather than added as a `RunTool` parameter. Nil, the default
  on both `Config{}` and `tui/`'s `-approve-tools=false`, means every call runs unasked; a
  refusal is reported on `ToolEvent.Refused` rather than as a tool failure. `tui/`'s
  `-approve-tools` prompts y/a/n in the status line, serialized to one question at a time since
  a backend can run tool calls concurrently within a turn, and unblocked by the same
  ctrl+c/ctrl+\ that already gets a wedged run's way out.
- **`tui/` slash commands** — `/clear` (reset the transcript, the conversation and the running
  cost, same client), `/help` (list commands and keybindings) and `/quit`. A leading `/` never
  reaches the model; an unrecognised command is reported rather than sent as a question, the
  same trade-off every peer client with slash commands makes. The prompt suggests and completes
  them as you type, built on `textinput`'s own suggestion list rather than a new component:
  a match ghosts in ahead of the cursor, `tab` accepts it, `ctrl+n`/`ctrl+p` cycle multiple
  matches, and up/down stay bound to scrolling the transcript.

[Unreleased]: https://github.com/FacileStudio/nacelle/commits/main
