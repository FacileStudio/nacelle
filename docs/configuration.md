# nacelle — Configuration

Every field the core `Config` reads, then the TUI's own settings layer — a separate thing,
with its own precedence order — and the traps in each.

## `nacelle.Config`

The core reads nothing from the environment or from disk. Every field is passed in the struct
literal a consumer builds.

| Field | Default | What it does |
|---|---|---|
| `Backend` | — required | The model this agent runs on. No default: a package that picks one for you is a package that hides the most consequential decision in the configuration |
| `System` | — required | The system prompt. Empty is refused rather than defaulted — an agent with no system prompt is a general-purpose assistant wearing a product's name |
| `Effort` | the backend's own default | `low`, `medium`, `high`, `xhigh`, `max` |
| `Thinking` | `false` | Stream the model's reasoning as `KindThinking`. Off by default, matching the APIs: the raw chain of thought is never returned and a readable summary is opt-in |
| `MaxTokens` | `DefaultMaxTokens` (32000) | Per-turn output ceiling. Generous on purpose — every request is streamed, so a large ceiling costs nothing in latency, while a small one truncates an answer mid-sentence and buys a retry |
| `MaxIterations` | `0` (no cap) | How many times the model may be asked. A cap is only safe when every tool is read-only and cheap; reaching it ends the run with `Stop: StopIterations`, not a failure |
| `Tools` | `nil` | Tools the model may call in this process |
| `MCP` | `nil` | MCP servers the model may call tools on. Only `anthropic` can reach them — see [architecture.md](architecture.md#capabilities) |
| `Logger` | `slog.Default()` | Receives the few things worth recording that are not events — retry attempts, mainly |

`Backend`, `Thinking`, `Effort` and `MCP` are all checked against the chosen backend's
`Capabilities` at construction; a config asking for something the backend cannot do is refused
with an `*Unsupported` error rather than silently running with less.

## `anthropic.Config`

| Field | Default | What it does |
|---|---|---|
| `Client` | built from the environment | Set to share a client, point at a proxy, or hand a test a stub transport |
| `Model` | `DefaultModel` (`claude-opus-5`) | — |

## `openrouter.Config`

| Field | Default | What it does |
|---|---|---|
| `APIKey` | `OPENROUTER_API_KEY` | — |
| `Model` | — required | An OpenRouter model slug, e.g. `anthropic/claude-opus-5`. Required: OpenRouter fronts hundreds of models and a default would be this package choosing the one thing the caller came here to choose |
| `BaseURL` | `DefaultBaseURL` | Point at a compatible gateway or a recording proxy in tests |
| `Referer`, `Title` | — | Attribution only — `HTTP-Referer` and `X-OpenRouter-Title`. Neither affects the answer; without `Referer` the usage simply does not appear against an app |
| `Provider` | `nil` | OpenRouter's provider-routing object, passed through untouched. Worth setting `require_parameters: true` when tool calling matters — it keeps the request away from providers that would drop the tool schema |
| `Options` | `nil` | Extra request options for the underlying client |

## `tools.Config`

| Field | Default | What it does |
|---|---|---|
| `Root` | — required | The directory every tool is confined to, through `os.Root` |
| `AllowBash` | `false` | Mounts the command tool. Not a limit that can be tuned into safety — running commands is either something this agent may do or it is not |
| `CommandEnv` | `PATH` + `HOME` only | The process environment is never inherited — it is where a service keeps the credentials a model must not read |
| `CommandTimeout` | `DefaultCommandTimeout` (2 min) | A command outliving it is killed with its children |
| `MaxOutputBytes` | `DefaultMaxOutputBytes` (64 KiB) | Caps what one tool call may return — output stays in the context window for the rest of the conversation |
| `MaxReadBytes` | `DefaultMaxReadBytes` (48 KiB) | Caps a single file read, below the output cap so a read that hit the limit still leaves room for the notice saying so |

## The TUI's settings layer

`tui/` is a separate settings problem from the core, and deliberately does not share a
mechanism with it: **the library must never read configuration from disk or environment.** A
`nacelle` package that read `~/.nacelle.yml` would let a file on a *different* machine's disk
change a headless consumer's behaviour, the way Bubble Tea's own config never reaches past the
program that imports it.

### Precedence

**Flag beats environment beats file beats default**, resolved in one function
(`settings()` in `tui/config.go`) and nowhere else. The suite has already paid for the
alternative once: a CLI that read its environment inside one code branch and its file inside
another turned what its README called "overrides" into two mutually exclusive modes nobody
could tell apart.

| Layer | Source | Notes |
|---|---|---|
| Flags | `-backend`, `-model`, `-effort`, `-root`, `-system`, `-bash`, `-thinking`, `-mycelium`, `-project-context`, `-skills`, `-trust-skills`, `-skill-dir`, `-mcp`, `-approve-tools`, `-max-iterations` | Only flags actually **typed** are collected, via `flag.Visit` — Go's `flag` package cannot otherwise tell a flag left alone from one passed its own default value. `-skill-dir` and `-mcp` are repeatable (`-mcp a.json -mcp b.json`); every other flag keeps only its last occurrence |
| Environment | `NACELLE_BACKEND`, `NACELLE_MODEL`, `NACELLE_EFFORT`, `NACELLE_ROOT`, `NACELLE_SYSTEM`, `NACELLE_BASH`, `NACELLE_THINKING`, `NACELLE_MYCELIUM`, `NACELLE_PROJECT_CONTEXT`, `NACELLE_SKILLS`, `NACELLE_TRUST_SKILLS`, `NACELLE_SKILL_DIRS`, `NACELLE_APPROVE_TOOLS`, `NACELLE_MAX_ITERATIONS`, `NACELLE_SEARCH`, `NACELLE_FETCH` | A misspelt boolean (`NACELLE_BASH=yez`) is treated as unmentioned, not as `false`, and falls through to the layer below. `NACELLE_SKILL_DIRS` is colon-separated, the same convention `PATH` itself uses for a list of directories |
| File | `~/.nacelle.yml` | Preferences only, **no credentials** — those already have two homes: the environment, and the Anthropic SDK's own profile. `KnownFields(true)`: an unrecognised key (`max_iteration:`, one letter short) is refused rather than silently ignored |
| Defaults | — | `backend: anthropic`, `root: .`, `bash: false`, `thinking: false`, `mycelium: true`, `project_context: true`, `skills: true`, `trust_skills: false`, `skill_dirs: []`, `mcp: []`, `approve_tools: false`, `max_iterations: 40`, `search: ""` (no web search), `fetch: true` |

`mycelium`, `project_context` and `skills` default **on**, unlike `bash`: each fails soft to
nothing when there is nothing to find — no `mycelium` on `PATH`, no `AGENTS.md`/`CLAUDE.md`
anywhere above `root`, no `~/.agents/skills/` — so a machine without any of them is no worse
off for asking. `trust_skills` and `approve_tools` both default **off**, and for the same
underlying reason: a project's own `.agents/skills/` can carry instructions to run arbitrary
scripts, and asking before every tool call is a decision that changes how the client feels to
use — blanket-trusting a directory, or blanket-interrupting every call, are both decisions only
the person running this should opt into, not defaults sprung on them. See
[Context, skills and mycelium tools](#context-skills-and-mycelium-tools) and
[Tool approval](#tool-approval) below.

### `~/.nacelle.yml`

```yaml
backend: anthropic
model: claude-opus-5
effort: high
root: .
system: You are a terminal coding assistant.
bash: true
thinking: true
mycelium: true
project_context: true
skills: true
trust_skills: false
skill_dirs:
  - ~/.claude/skills
mcp:
  - ~/.claude/.mcp.json
approve_tools: false
max_iterations: 40
search: https://searx.example
fetch: true
```

Every field is optional. A missing file is not an error — most people never write one — but an
unreadable or malformed one is: a config silently ignored is worse than no config, because the
setting carefully written is simply not in effect and nothing says so.

**No per-project `./.nacelle.yml` yet.** A second precedence layer before the first has real
users is a layer nobody has asked for the shape of.

## Context, skills and mycelium tools

Four things the TUI adds beyond flags and the model's own tools, all in `tui/`, none of them
in the core `nacelle` package — the same "the library must never read configuration from disk"
rule above applies to these too, not only to settings.

**This session's own facts** (always on, `tui/environment.go`). Prepended to whatever `System`
says, before context and skills, because it is the most general layer and the only one with no
switch: the absolute working directory, that the file and search tools are confined to it and
strip a leading `/` rather than refusing it, today's date, that the answer is rendered as
markdown, and whether anyone reviews a tool call before it runs. Two clauses are conditional the
way the banner's own lines are — the `run_command` path exception and the paragraph naming
irreversible commands appear only under `-bash`, because explaining a tool the model was not
given teaches it to skim the rest. These are facts the model cannot recover from any tool schema.
Advice about *which* tool to reach for is deliberately not here; it lives in the tool
descriptions, where it travels with the tool that needs it and costs nothing when that tool is
not mounted.

The persona above it (`defaultSystem`, `tui/main.go`) is the one layer `-system` replaces
outright, and it stays deliberately short. Codex ships two prompts for one harness — 6.6&nbsp;KB
for the models post-trained on it, 24&nbsp;KB for general GPT-5 — so prompt size mostly measures
how much the model was *not* trained on your harness, and Claude is well inside the trained case
for this shape of tool. What it carries is only what a terminal changes about answering: brevity,
paths rather than pasted source, and not claiming something works unchecked.

**Project and global context** (`-project-context`, `tui/context.go`). Every `CLAUDE.md` and
`AGENTS.md` found walking up from `-root` to the filesystem root, plus `~/.agents/AGENTS.md` —
the [AGENTS.md standard](https://agentsstandard.com)'s own global-base path, also read by
Codex, Cursor, Copilot, Gemini and pi (at its own equivalent, `~/.pi/agent/AGENTS.md`) — all
appended to the system prompt, most general first. A user's own `~/.claude/CLAUDE.md` is
deliberately **not** read: it assumes Claude Code's own tools, hooks and slash commands, where
an `AGENTS.md` at the standard's own path is written, by the convention's premise, to make
sense to whichever agent finds it. None of this is trust-gated — see
[architecture.md](architecture.md) for why a plain instruction file and a skill are not the
same risk.

**Skills** (`-skills`, `-trust-skills`, `-skill-dir`, `tui/skills.go`), following the
[Agent Skills specification](https://agentskills.io/specification): every `SKILL.md` under
`~/.agents/skills/` (no trust needed, the user's own machine), every one under a **trusted**
`.agents/skills/` found the same way the context walk works, and every one under a directory
named by `-skill-dir` (repeatable), `NACELLE_SKILL_DIRS` (colon-separated) or `skill_dirs` in
`~/.nacelle.yml`. Only a skill's `name` and `description` ever reach the system prompt; the
model reads the rest with `read_file` once it decides a skill applies. `-trust-skills` trusts
every project-local `.agents/skills/` found on that run and remembers the decision in
`~/.nacelle/trust.json`, keyed by canonical directory — run it once per project, not on every
launch.

**MCP servers** (`-mcp`, `tui/mcp.go`). Each `-mcp` names a file in the `mcpServers` format —
`.mcp.json` and its siblings — whose servers are started at launch, their tools handed to the
model like any other. It reads the file other clients already write, so pointing at
`~/.claude/.mcp.json` works without copying anything, which is the same problem `-skill-dir`
solves for skills. Both stdio (`command`) and HTTP (`"type": "http"`) servers work, on **either
backend**: the tools are bridged to `nacelle.Tool`, so `-approve-tools` gates them like every
other tool and `-backend openrouter` gets them too. `${VAR}` and `${VAR:-default}` expand, so a
token stays in the environment rather than in the file.

The flag is repeatable and `mcp:` in `~/.nacelle.yml` **accumulates** with it rather than being
replaced — the one list here that does. `client.Load` merges files by server name with the later
one winning, so a personal list plus a project's layer the way every client in this ecosystem
layers its own scopes. Replacing would mean naming one project server silently switching off the
nine already configured.

A server that will not start **ends the run**, which is the opposite of how skills and project
context fail. Those are discovered, so finding nothing is indistinguishable from there being
nothing to find; this was asked for by name, and a tool that is quietly missing reads as a model
refusing to work rather than as a server that is down.

Nothing is discovered. A `.mcp.json` sitting in the working directory is **not** read, because it
names executables to run — strictly worse than the project-local `.agents/skills/` this client
already gates behind `~/.nacelle/trust.json`, since a skill is text the model may decline to act
on and this is a subprocess started before the model is asked anything. Every server nacelle
opens was named on the command line or in your own `~/.nacelle.yml`.

The spec defines the `SKILL.md` file, not where it has to live on disk — every tool picks its
own directory (Claude Code reads `~/.claude/skills/`, pi reads `~/.agents/skills/` and its own
`~/.pi/agent/skills/`), so `~/.agents/skills/` is nacelle's own choice, not a location any other
tool already reads. `-skill-dir` is how nacelle sees a directory that belongs to one of those,
without moving or copying anything into its own — the same problem pi itself solves with a
`skills` array in `settings.json` pointed at `~/.claude/skills` or `~/.codex/skills`. No trust
gate applies to it, same reasoning as `~/.agents/skills/` above: naming a directory here is
something only the person running nacelle, on their own machine, can do in the first place.

**Mycelium tools** (`-mycelium`, `tools.Mycelium`, [api.md](api.md#tools)). `list_flows`, `run_flow`
and `search_memory`, when the `mycelium` binary is on `PATH`. Narrower and more legible than
reaching the same commands through `run_command`, and available even with `-bash=false`.

**The banner** (`tui/main.go`'s `banner`) is how much of this is actually visible before typing
anything: line one names the backend and model, line two the resolved `-root`, how many skills
loaded (from every source above, combined), how many `CLAUDE.md`/`AGENTS.md` files did, and
whether bash is on. Nothing on it is decorative — each answers a real "is that actually on"
question this client otherwise had no way to check short of a debug build.

`bash off` earns its place because the symptom arrives from the model rather than from this
client: asked to build and run something, it answers that it has no terminal and cannot run a
command. That is true and deliberate — `run_command` is unconfined, so it stays opt-in — but
nothing connected that answer back to a `bash: false` written once in `~/.nacelle.yml` and
forgotten. Turn it on with `-bash`, `NACELLE_BASH=1`, or `bash: true`.

## Web search

`search:` (`NACELLE_SEARCH`, `-search`) is the base URL of a [SearXNG](https://docs.searxng.org)
instance to search the web through. **Empty by default, and there is no instance this client
could pick on your behalf** — nacelle is public, so any default would send your queries to
somebody else's machine and leave them in that operator's logs. Empty means the tool is not
mounted at all: the model is never told search exists, and nothing fails.

It is a URL rather than a toggle plus a URL, so two settings cannot disagree about whether
search is on. The banner says `search on` when one is set, and says nothing when none is —
unlike `bash off`, which is named either way, because search being off has no symptom to
explain.

```yaml
search: https://searx.example
```

Passing `-search ""` (or `NACELLE_SEARCH=`) turns search off for one run without editing the
file, which is why this setting is a pointer internally while the other strings are not: for
everything else empty means "not mentioned", and here it means "not this run".

An endpoint that could never work — no scheme, no host — stops the client at startup rather
than quietly leaving the tool unmounted, because search silently missing looks exactly like a
model that decided not to search, and nothing on screen would connect that to a typo here.

Three failures are named rather than left to read as an outage: an instance answering HTML
means `json` is missing from `search.formats` in its `settings.yml` (off by default in
SearXNG); a 403 is usually its limiter; a 404 usually means this was set to the `/search` page
copied from a browser rather than the instance's base URL.

Why an instance you host rather than the backends' own search: both have it, and neither is
free — $10 per 1,000 searches on Anthropic, no free tier on OpenRouter. A local tool also works
identically on both backends, where server-side search would be wired, and billed, per backend.

## Reading a page

`fetch:` (`NACELLE_FETCH`, `-fetch`) lets the model read one web page by URL. **On by default**,
unlike bash and unlike search, and it is what makes a search result more than a sentence — search
answers with a title and two lines, and this reads the page behind them.

It is on by default because it cannot change anything and cannot reach anything but the public
internet: loopback, private ranges, the cloud metadata endpoint and the special-use ranges are
refused in the dialer, on every redirect hop. Pages come back as text — headings, lists, code
blocks, and links resolved to absolute URLs so the model can follow one by calling the tool
again. `text/markdown` is asked for first, which Cloudflare and Vercel answer by converting at
the edge for roughly 80% fewer tokens.

Turn it off with `fetch: false` for the one risk the guard cannot cover: a fetched page is
written by a stranger and read by the model as instructions, so it can ask for another URL with
something from the conversation in its query string. With `bash: true` that channel already
exists through `run_command`; with bash off, this is the only one. The banner says `fetch off`
when it is off, since a model that cannot read a page it just found needs the reason on screen.

Sites do refuse automated clients. This one identifies itself honestly rather than impersonating
a browser — every current source agrees that negotiated access is what gets an agent through and
that a spoofed user agent is what gets an address banned — so a 403 from an anti-bot layer is
reported as one, with the suggestion to try another source, rather than as the page not existing.

## Tool approval

`-approve-tools` (`NACELLE_APPROVE_TOOLS`, `approve_tools`) asks before every tool call runs.
**Off by default**: a nil `Config.Approve` means the SDK itself runs every call unasked, and
the TUI's own default matches — nobody gets a behaviour change without opting in.

When on, a call blocks in the status line on `y` (allow this one call), `a` (allow this tool
for the rest of the session — remembered in memory only, never written to disk, unlike
`-trust-skills`) or `n` (deny; asked again next time). Asks are serialized to one question at
a time, because a backend can run several tool calls concurrently within a turn and this
client has exactly one place to show a question. A pending question is answered or abandoned
by the same ctrl+c/ctrl+\ that already gets a wedged run's way out — both wait on the run's
own cancellable context, so cancelling one unblocks the other with no separate mechanism.

A denial is reported on `ToolEvent.Refused`, not as a tool failure — the SDK converts it into
a normal tool-result error block for the model to see, the same as any other failed call.

## Slash commands

Typing `/` at the start of a line names one of the client's own commands instead of a
question:

| Command | Does |
|---|---|
| `/clear` | Reset the transcript, the conversation sent to the model, and the running cost total. Same client, new session. Never starts a run. |
| `/help` | List the commands above and the keybindings (esc, ctrl+c/ctrl+\). Never starts a run. |
| `/quit` | Quit. Never starts a run. |
| `/skill:name [what to do]` | Run a loaded skill directly — **does** start a run, unlike the three above. |

An unrecognised command or skill (a typo like `/clera`, or a `/skill:name` that names nothing
loaded) is reported back rather than sent to the model as a literal question — the same
trade-off every peer client with slash commands makes, on the same reasoning: a real question is
far less likely to start with a slash than a mistyped command is.

`/skill:name` sends that skill's own `SKILL.md` content as the question, instead of waiting for
the model to decide on its own that the skill applies and read it with `read_file` — the same
shortcut pi's own `/skill:name` offers, for the same reason: "models don't always" make that
call themselves. Anything typed after the name is appended as `User: <that text>`, the same
convention pi uses, so `/skill:pdf-tools extract the tables` tells the skill what to do with
itself rather than only that it applies. Every loaded skill counts, from `~/.agents/skills/`,
a trusted project `.agents/skills/`, or a `-skill-dir` — one namespace, `skill:`, so a skill
named `clear` or `help` can never collide with the three commands above.

Typing `/` alone opens a dropdown listing every command and skill, each skill with its own
description; more typed narrows it, ranked best match first — a prefix beats a plain substring
beats a fuzzy, order-preserving scatter of the same letters, so `/skill:rev` finds `review`,
`facile-review` **and** `hunk-review`, not only a name that starts with what was typed. While
it's open, `up`/`down` move the selection instead of scrolling the transcript, `tab`/`enter` fill
the prompt with the highlighted entry (plus a trailing space, and without submitting — most
useful with `/skill:name`'s own trailing argument) and `esc` closes it, leaving what was typed
alone. A second, ordinary `enter` is what actually sends it.

### The prompt

The prompt wraps and grows as you type, up to ten rows, then scrolls inside itself. Every row it
gains is a row the transcript gives up, and gets back the moment the question is sent. `alt+enter`
(or `ctrl+j`) starts a new line without sending; `enter` always sends.

It used to be a single-line input, which scrolled sideways instead of wrapping — a question wider
than the terminal slid out of view a character at a time and read as though it were typing over
itself.

`up`/`down` belong to the prompt at every height — there is no transcript here to scroll, so
nothing competes for them and a wrapped question is editable whatever size it has grown to.

### Scrolling

The client renders inline, on the terminal's own screen, so scrolling is the
terminal's job — wheel, scrollbar, `tmux` copy-mode, your terminal's search, all of it, on the
whole session rather than on a window this client owns. There are no scroll keys here because
there is nothing here to scroll.

That is a deliberate reversal. It ran on the alternate screen once, which hands a program a
blank page with no scrollback of its own, so the client had to own scrolling: capture the wheel,
which takes click-drag selection with it, leave copy-mode reaching nothing, and un-draw the whole
session on quit. Those arrived as four separate complaints and were one decision. Giving the page
back answers all of them, and costs one thing — a printed line belongs to the terminal, so a
resize reflows it the terminal's way rather than this client's, and a theme change applies from
then on rather than retroactively.

### While a run is going

The status line spins for as long as the run lasts, and names what it is waiting on — `waiting for
a response` before the model answers, `running read_file` between a tool's call and its result,
`running 3 tools` when the backend runs a turn's tools at once. It is on the one row that is
always drawn, so it stays visible while scrolled back through the transcript.

This replaced a spinner that only covered the gap before the first event. Every gap after it — a
tool running, the model called again with its result — left the screen still, and a client that has
stopped moving reads exactly like one that has stopped working.

**Enter still works while a run is going**: the line is queued, shown above the prompt, and sent
once the run finishes. A queued `/help` is still a command.

**`esc` stops the run**, and that is all it ever does. `ctrl+c` stops one too, but it is also the
key that quits an idle client, so the press that abandons an answer is a press that has to be
thought about first; `esc` is the one that never is. Either way the queue goes with the run rather
than being started by it, and the client says how many it dropped — the alternative is a message
that silently never gets asked. Idle, `esc` does nothing at all, and with the `/` dropdown open it
closes that first: one visible thing per press.

The question is on the screen before either of them is worth pressing. It used to appear only when
the answer did: the client printed what it had said only after running what that message started,
and what a question starts is a wait on the model. The prompt emptied and nothing else happened,
which reads as a client that swallowed it. The `⏺` line naming a tool had the same fault and was
worse for it — it waited on the tool it was announcing, so a six-second command was announced six
seconds late. The CHANGELOG carries the measurements.

### Quitting

`ctrl+c` when nothing is running, `ctrl+\` at any time, and `/quit` all end the session, and the
session simply stays where it is. Nothing is printed on the way out and nothing needs to be: what
was said went into the terminal's scrollback as it happened, so quitting leaves it exactly as
running left it.

That used to need a deliberate dump at exit, because the alternate screen hands the old page back
untouched and quitting un-drew the whole conversation. Inline rendering removed the problem rather
than the workaround.
