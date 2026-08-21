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
  watching. It renders **inline**, on the terminal's own screen: finished lines are printed once
  and belong to the terminal from then on, so scrolling, selection, search, `tmux` copy-mode and
  the session surviving a quit are all the terminal's own, on the whole conversation. It ran on
  the alternate screen first, and every complaint that arrangement produced — a dead mouse wheel,
  selection needing shift, copy-mode reaching nothing, the session vanishing on exit — was the
  same decision rather than four bugs. The cost is that a printed line is no longer this
  client's: a resize reflows it the terminal's way, and a theme change applies from then on
  rather than retroactively. The status line spins for the whole run and names what it is
  waiting on — a tool by name, or the model. Enter during a run queues the line and sends it
  when the run finishes. The prompt wraps and grows with what is typed, up to ten rows or half
  the window, rather than scrolling sideways; alt+enter starts a new line without sending. The
  banner says whether bash is on, because the symptom of it being off arrives from the model
  ("I have no terminal"), not from anything this client prints. Renders answers as markdown
  (`charm.land/glamour`), picking light or dark from the terminal's own reported background
  rather than assuming one. Reads `~/.nacelle.yml` beneath `NACELLE_*` environment variables
  beneath command-line flags, in that order, and holds no credentials — those already have two
  homes.
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
- **`tools.WebSearch`** — `web_search`, against a [SearXNG](https://docs.searxng.org) instance
  you host. Both backends can search server-side and neither does it for free ($10 per 1,000
  searches on Anthropic; no free tier on OpenRouter), while an instance you already run costs
  nothing per query, keeps the queries on your own machine, and works the same on either
  backend because it is an ordinary local tool. There is no default endpoint and an empty one
  returns no tools and no error, the same as absent mycelium: this package is public, and any
  instance shipped as a default would be somebody else's machine. The endpoint is a
  constructor argument and never a tool argument, so a model cannot name the host a request
  goes to. Configured in `tui/` by `search:` in `~/.nacelle.yml`, `NACELLE_SEARCH`, or
  `-search`; the banner says `search on` when one is set.
- **`tools.WebFetch`** — `web_fetch`, which reads one of the pages search finds. A search result
  is a title and two sentences; this is the page behind it, as text with headings, lists, code
  blocks and links resolved to absolute URLs so the model can follow one. It asks for
  `text/markdown` first, which Cloudflare and Vercel answer by converting at the edge for roughly
  80% fewer tokens, and identifies itself honestly rather than impersonating a browser, so a 403
  from an anti-bot layer is reported as one with somewhere else to try. It is the only tool here
  whose destination the model names, so the address check is in the dialer's `Control` hook —
  after resolution, before connect, every redirect hop — refusing loopback, private, link-local
  including the cloud metadata endpoint, and the special-use ranges `net/netip` has no predicate
  for. In `tui/` it is on by default; `fetch: false` (`NACELLE_FETCH`, `-fetch`) turns it off,
  and the banner says `fetch off` when it is.
- **`golang.org/x/net`**, for `html.Parse`. The first new dependency here in a while, and the
  alternative was hand-writing an HTML tokeniser — which is how the page renderer got its one
  real bug: real pages leave tags unclosed, and only the tree-construction algorithm closes them
  the way a browser does.
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
  same trade-off every peer client with slash commands makes.
- **A two-line banner** (`tui/main.go`) — backend and model first, then the resolved `-root`,
  how many skills loaded and how many `CLAUDE.md`/`AGENTS.md` files did. `projectContext` now
  returns that file count alongside the rendered text (one walk, not a second just to count),
  and `augmentSystem` returns both counts plus the skill list in one `loaded` result rather than
  the two bare return values it had. Root is resolved to an absolute path, not echoed as typed —
  `-root .` reads the same from anywhere and answers nothing on its own.
- **A dropdown menu for `/`** (`tui/menu.go`, `tui/menukeys.go`) replaces the first cut of this
  that just wired `textinput`'s own ghost-text suggestions: typing `/` alone opens a list of
  every command and skill, each with its own description; more typed ranks it — prefix, then
  substring, then a fuzzy order-preserving match, hand-rolled rather than a new dependency, so
  `/skill:rev` finds `review`, `facile-review` **and** `hunk-review`, not only a name starting
  with what was typed. `up`/`down` move the selection while it's open instead of scrolling the
  transcript; `tab`/`enter` fill the prompt (plus a trailing space) without submitting, `esc`
  dismisses. `tui/model.go`'s own `layout()` reserves the dropdown's height out of the
  transcript's, recomputed on every filter change, not only on a real terminal resize.
- **`RetryOptions.Budget`** — a wall clock for a whole run, and the only bound on how long one
  can actually take. `Max` caps this wrapper's own backoff and nothing else: both SDKs sleep on
  `Retry-After` inside an attempt, before the failure is handed up as transient, so three
  attempts over three HTTP retries is nine requests and six sleeps nacelle never sees — roughly
  six minutes under `Retry-After: 60`, against a documented 8s ceiling. Those sleeps are a
  `select` on `ctx.Done()`, so a deadline derived once in `Stream` and passed down is the only
  thing that reaches them. It has no default, because being a deadline it bounds the successful
  attempt too and no number of seconds is right for every run. A run that spends it ends with
  the exported `ErrRetryBudget`, kept tellable apart from the caller's own cancellation — both
  read as `context.DeadlineExceeded`, but one is this policy firing and the other is theirs.
- **`mcp/client`** — MCP servers run as subprocesses over stdio, their tools handed back as
  ordinary `nacelle.Tool` values. Until now `mcp` was configuration only: it wired Anthropic's
  server-side connector to a remote URL, so a local server was unreachable and `openrouter`,
  which refuses `Config.MCP` under the capability rule, got no MCP tools whatever the transport.
  Bridging to `nacelle.Tool` rather than adding a second path through the loop is the whole
  design — a bridged tool passes through `Config.Approve`, emits the same
  `KindToolCall`/`KindToolResult` events, and works on both backends. Built on the official
  `modelcontextprotocol/go-sdk` rather than a hand-rolled JSON-RPC client, for the reason
  `retry.go` gives for not re-implementing HTTP backoff: it would be a second, worse copy of
  something we do not own. It is a sub-package because the root package imports `mcp`, so `mcp`
  cannot import it back, and a bridged tool that is not a `nacelle.Tool` is usable by nothing.
  The environment is never inherited — `PATH`, `HOME` and whatever `Command.Env` names, matching
  what `tools/` already does for the commands it runs, and the argument is stronger here because
  that code is ours and this code is somebody else's. Names are namespaced `<server>_<tool>` and
  refused, never truncated, when they break the `^[a-zA-Z0-9_-]{1,64}$` both provider APIs
  enforce: a truncated name silently collides with another one. A server that fails to start says
  why: the first 8KB it wrote to stderr is kept and hung off the error, because otherwise the
  reason goes to `/dev/null` and the operator is left with `connection closed: calling
  "initialize": EOF` — the protocol step that noticed the silence, not the `FATAL:` line that
  caused it. `Command.Stderr` takes the rest, for a server that started fine and has more to say.
- **`client.Remote`**, the Streamable HTTP transport, which closes the last MCP gap: a hosted
  server was reachable only through Anthropic's own connector, and so not at all from
  `openrouter`. Dialled from here its tools are ordinary local tools, which means the approval
  gate sees them and the tool set does not change shape when someone passes `-backend`. The cost
  is your egress, once per call, so `mcp.Server` stays the better choice for a service pinned to
  Anthropic that would rather not carry the traffic. `Headers` is where a bearer token goes, added
  by a `RoundTripper` rather than once at handshake time, because the transport reconnects a
  dropped stream on its own and a header set once would go missing on exactly the retry that
  needed it. SSE and `ws` are refused by name: MCP replaced SSE with Streamable HTTP, and the
  error says which key to change instead of failing at a session that never establishes.
  `Connect` now takes a sealed `Server` union rather than a `Command`, so a transport is a type
  instead of a mode field two thirds of whose neighbours are wrong for whichever one you picked.
  `Command` still satisfies it, so existing calls compile unchanged.
- **`client.Load`**, which reads the `mcpServers` format Claude Code, Claude Desktop and Cursor
  already write, so an existing `.mcp.json` works as it stands. Reading somebody else's format
  rather than inventing one is the same call `-skill-dir` makes for skills, from the same
  direction: the alternative is a second copy of one list in a nacelle-shaped syntax. Later paths
  override earlier ones by name, matching those clients' own scope precedence. Two deliberate
  departures: keys **inside** a server entry are strict, because `comand` otherwise starts a
  server without the arguments it needed and surfaces as a tool that misbehaves rather than a
  file that is wrong; and an unset `${VAR}` with no default is an error rather than an empty
  string, because `Authorization: Bearer ` buys a 401 that names neither the variable nor the
  file. Keys *beside* `mcpServers` are ignored, since these files are shared with clients that
  add their own. `${VAR}` and `${VAR:-default}` expand in `command`, `args`, `env`, `url` and
  `headers`; only the braced spelling is a reference, so a literal `$` in a password survives.
- **`-mcp` in `tui/`**, repeatable, naming a file in that same format — so the terminal client
  can finally reach the servers the library has been able to talk to. `mcp:` in `~/.nacelle.yml`
  **accumulates** with the flag rather than being replaced, the one list here that does: `Load`
  merges by server name with the later file winning, so a personal list and a project's layer
  the way every client in this ecosystem layers its scopes, where replacing would mean naming
  one project server silently switching off the nine already configured. The banner says how
  many servers connected and how many tools they brought, and says nothing at all when none are
  configured. A server that will not start ends the run — the opposite of how skills and project
  context fail, because those are discovered and this one was asked for by name. Nothing is
  discovered: a `.mcp.json` in the working directory is not read, because it names executables
  to run, which is strictly worse than the project-local skills already gated behind
  `~/.nacelle/trust.json`.

- **`esc` stops a run, and never does anything else.** `ctrl+c` already cancelled one, but
  it is also the key that quits an idle client, so the press that abandons an answer was a
  press that had to be thought about first. `esc` is the one that never is: idle it is not
  the client's key at all and reaches the prompt, and with the `/` dropdown open it closes
  that first, one visible thing per press. Both keys reach the same `abandon` in
  `tui/run.go`, so neither can drift into stopping a run differently — the run is cancelled,
  marked abandoned, and whatever was queued behind it is dropped rather than started. The
  status line offers `ctrl+c`/`ctrl+\` as the way out of a run that will not stop, whichever
  key asked it to.

- **A concurrency contract that is tested rather than asserted.** `Agent` already claimed to be
  safe to share between goroutines and nothing checked it, which is a bad way to hold a promise —
  the bug it rules out is one caller being answered with another's conversation, and no
  single-threaded test can see that. Fifty callers now run through one agent and each is checked
  against its own question; with `Stream`'s copy of the request removed, it fails with crossed
  answers and data races. The same test exists for both tool sets, including twenty callers
  through one MCP session — the only place in nacelle where concurrent runs share something that
  talks. `Tool.Run` and `Approve` now say they may be called from several goroutines at once,
  which they always could: a model can ask for two tools in one turn and a backend runs those
  together, so it happens on a single conversation before anything shares an agent between
  requests.

### Fixed

- **Nothing the client has said waits on what it is about to do.** `Update` sequenced the
  print queue *behind* whatever the routed message returned, and two of the commands it can
  return block until the model sends something — `waitFor`, and the batch `send` wraps it in.
  A sequence does not reach its next command until the one before it is done, so every line
  said on the way into a wait was drawn only when that wait ended. Two visible symptoms, one
  cause: pressing enter emptied the prompt and showed nothing until the first token arrived,
  which reads as a client that swallowed the question; and a tool's own `⏺` call line waited
  on the tool it was announcing. Measured against OpenRouter at 100x30, from the keypress:
  the question echoed at 814ms beside the first token, now 5ms against an answer that began
  at 1312ms; `⏺ run_command` for a six-second command reached the screen at 7451ms, said at
  1467ms, and now lands at 1467ms with the status line that names it. `Update` prints first
  and runs second, which is one place rather than one per blocking command — a third would
  otherwise arrive with no reason to know it had to flush, the way the second sat there while
  the first was being found. `/clear` is the one command that prints from its own `Cmd`, so
  it drains the queue by hand at both ends: the echoed `/clear` above the blank run, the
  fresh banner below it. The same ordering had been costing a queued follow-up its echo and
  the previous answer's commit, both of which waited on the *next* run rather than the one
  that produced them.

- **A committed answer no longer leaves a second, unrendered copy of itself in the
  scrollback.** Scrolling up after an answer showed it twice: once as raw markdown with a
  duplicate prompt beneath it, and once rendered. The duplicate was the live frame itself.
  `tea.Println` makes room by scrolling the screen up by as many rows as it is about to
  write, so once a batch is taller than the rows free above the frame, the frame's own rows
  go into the scrollback and the renderer — which draws relative to where the frame ended
  up — never reaches them again. `layout` now holds half the window back so the frame can
  never stand on every row, and `tui/headroom.go` cuts a batch into pieces that fit the rows
  the frame actually left free, counting a wrapped line as the rows it wraps onto. That
  figure is taken from the frame `View` drew rather than worked out from what the frame is
  allowed to be: emptying a tall prompt hands its rows back before the screen has been
  repainted, and a budget that believes the new layout overruns the old frame by exactly the
  rows it gave back. Measured at a 6-row frame in a 20-row pane: 14 printed rows is clean and
  15 strands exactly one.
- **`/clear` no longer leaves the prompt half-drawn.** Its window of blank lines is exactly
  the size that scrolls the frame away, and because the frame after a clear is identical to
  the frame before one, `flush` saw no change and never repainted the rows the screen had
  lost — the prompt's `> ` and the status line's `ready · ` simply stayed blank until a
  resize. It goes through the same cutting as every other batch now, and a test that parses
  `tui/` fails on a `tea.Println` outside the one function that budgets for it.

- **The terminal client's frame no longer ghosts when it shrinks.** Closing the `/` dropdown
  left its rows painted with a stale status line above them, and redrew the frame eight rows
  below where it belonged; deleting an `alt+enter` line walked the frame down the pane the
  same way. Neither was this client's doing. `ultraviolet` moved `curbuf.Resize` ahead of the
  clear pass in `Render`, so `move()` clamps its remembered cursor row against the frame
  arriving rather than the one on screen — harmless in fullscreen, where the next move can be
  an absolute `CUP`, and fatal inline, where that row is the only record of where the cursor
  physically is. Pinned to `v0.0.0-20260703014108-f5a850f9c2b7`, the commit Bubble Tea v2.0.9
  requires, and held there by a test. The pin costs `lipgloss` v2.0.5 and a combining-mark
  fix: on this commit an ASCII base followed by combining accents renders without them.
  Precomposed characters are unaffected.

### Changed

- **`RetryOptions.Max` now says what it does not bound.** It caps this wrapper's own delay and
  nothing else. Both SDKs default to two HTTP retries and sleep on `Retry-After` before a
  failure is handed up as transient, inside each attempt `Retry` then repeats — nine requests
  and six unseen sleeps, roughly six minutes under `Retry-After: 60` against a documented
  eight seconds. A context deadline is the only real bound, and nothing said so.

[Unreleased]: https://github.com/FacileStudio/nacelle/commits/main
