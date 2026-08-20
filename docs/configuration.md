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
| Flags | `-backend`, `-model`, `-effort`, `-root`, `-system`, `-bash`, `-thinking`, `-jardin`, `-project-context`, `-skills`, `-trust-skills`, `-skill-dir`, `-approve-tools`, `-max-iterations` | Only flags actually **typed** are collected, via `flag.Visit` — Go's `flag` package cannot otherwise tell a flag left alone from one passed its own default value. `-skill-dir` is repeatable (`-skill-dir a -skill-dir b`); every other flag keeps only its last occurrence |
| Environment | `NACELLE_BACKEND`, `NACELLE_MODEL`, `NACELLE_EFFORT`, `NACELLE_ROOT`, `NACELLE_SYSTEM`, `NACELLE_BASH`, `NACELLE_THINKING`, `NACELLE_JARDIN`, `NACELLE_PROJECT_CONTEXT`, `NACELLE_SKILLS`, `NACELLE_TRUST_SKILLS`, `NACELLE_SKILL_DIRS`, `NACELLE_APPROVE_TOOLS`, `NACELLE_MAX_ITERATIONS` | A misspelt boolean (`NACELLE_BASH=yez`) is treated as unmentioned, not as `false`, and falls through to the layer below. `NACELLE_SKILL_DIRS` is colon-separated, the same convention `PATH` itself uses for a list of directories |
| File | `~/.nacelle.yml` | Preferences only, **no credentials** — those already have two homes: the environment, and the Anthropic SDK's own profile. `KnownFields(true)`: an unrecognised key (`max_iteration:`, one letter short) is refused rather than silently ignored |
| Defaults | — | `backend: anthropic`, `root: .`, `bash: false`, `thinking: false`, `jardin: true`, `project_context: true`, `skills: true`, `trust_skills: false`, `skill_dirs: []`, `approve_tools: false`, `max_iterations: 40` |

`jardin`, `project_context` and `skills` default **on**, unlike `bash`: each fails soft to
nothing when there is nothing to find — no `jardin` on `PATH`, no `AGENTS.md`/`CLAUDE.md`
anywhere above `root`, no `~/.agents/skills/` — so a machine without any of them is no worse
off for asking. `trust_skills` and `approve_tools` both default **off**, and for the same
underlying reason: a project's own `.agents/skills/` can carry instructions to run arbitrary
scripts, and asking before every tool call is a decision that changes how the client feels to
use — blanket-trusting a directory, or blanket-interrupting every call, are both decisions only
the person running this should opt into, not defaults sprung on them. See
[Context, skills and jardin tools](#context-skills-and-jardin-tools) and
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
jardin: true
project_context: true
skills: true
trust_skills: false
skill_dirs:
  - ~/.claude/skills
approve_tools: false
max_iterations: 40
```

Every field is optional. A missing file is not an error — most people never write one — but an
unreadable or malformed one is: a config silently ignored is worse than no config, because the
setting carefully written is simply not in effect and nothing says so.

**No per-project `./.nacelle.yml` yet.** A second precedence layer before the first has real
users is a layer nobody has asked for the shape of.

## Context, skills and jardin tools

Three things the TUI reads beyond flags and the model's own tools, all in `tui/`, none of them
in the core `nacelle` package — the same "the library must never read configuration from disk"
rule above applies to these too, not only to settings.

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

The spec defines the `SKILL.md` file, not where it has to live on disk — every tool picks its
own directory (Claude Code reads `~/.claude/skills/`, pi reads `~/.agents/skills/` and its own
`~/.pi/agent/skills/`), so `~/.agents/skills/` is nacelle's own choice, not a location any other
tool already reads. `-skill-dir` is how nacelle sees a directory that belongs to one of those,
without moving or copying anything into its own — the same problem pi itself solves with a
`skills` array in `settings.json` pointed at `~/.claude/skills` or `~/.codex/skills`. No trust
gate applies to it, same reasoning as `~/.agents/skills/` above: naming a directory here is
something only the person running nacelle, on their own machine, can do in the first place.

**Jardin tools** (`-jardin`, `tools.Jardin`, [api.md](api.md#tools)). `list_flows`, `run_flow`
and `search_memory`, when the `jardin` binary is on `PATH`. Narrower and more legible than
reaching the same commands through `run_command`, and available even with `-bash=false`.

**The banner** (`tui/main.go`'s `banner`) is how much of this is actually visible before typing
anything: line one names the backend and model, line two the resolved `-root`, how many skills
loaded (from every source above, combined) and how many `CLAUDE.md`/`AGENTS.md` files did.
Nothing on it is decorative — each answers a real "is that actually on" question this client
otherwise had no way to check short of a debug build.

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
| `/help` | List the commands above and the keybindings (scrolling, ctrl+c/ctrl+\). Never starts a run. |
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

### Scrolling

`up`/`down` move the transcript a line, `pgup`/`pgdown` a page, and the mouse wheel three lines
at a time. The stream is followed only while the transcript is already at its end, so reading
back mid-run keeps the screen where it was put rather than yanking it down on every delta; the
status line says so while it lasts. Sending anything — a question, a command, a `/skill:name` —
returns to the end and starts following again, because an echo that lands off-screen makes
pressing enter look like it did nothing.

The client asks the terminal to report the wheel, and that is not free: it takes over
click-drag selection, which comes back by **holding shift** while dragging. There is no
wheel-only mouse mode to ask for instead, and not asking is worse than it sounds — the client
runs on the alternate screen, which has no scrollback of its own, so a terminal left to handle
the wheel itself has nothing to scroll and the wheel simply does nothing.

### Quitting

`ctrl+c` when nothing is running, `ctrl+\` at any time, and `/quit` all end the session, and all
three print the whole transcript to the terminal on the way out — after the alternate screen has
been handed back, so it lands in the terminal's own scrollback where it can be scrolled and
selected with nothing of this client's still running.

That print is the other half of what the alternate screen costs. A program given a blank page
hands the old one back untouched when it exits, so quitting does not scroll the conversation
away, it un-draws it — and this client keeps no session files to recover it from. Printing it
once is what turns a session that vanished into one the terminal has.

A run nobody asked anything in prints nothing, so launching in the wrong directory and quitting
straight back out leaves no trace.
