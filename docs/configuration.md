# nacelle — Configuration

The exported `Config` structs, package by package. The terminal client's own settings layer
(defaults, `~/.nacelle.yml`, `NACELLE_*` variables, flags) lives in
[FacileStudio/nacelle-tui](https://github.com/FacileStudio/nacelle-tui); this page covers what
the library itself reads.

## `nacelle.Config`

The core reads nothing from the environment or from disk. Every field is passed in the struct
literal a consumer builds.

| Field | Default | What it does |
|---|---|---|
| `Backend` | — required | The model this agent runs on. No default: a package that picks one for you is a package that hides the most consequential decision in the configuration |
| `System` | — required | The system prompt. Empty is refused rather than defaulted — an agent with no system prompt is a general-purpose assistant wearing a product's name |
| `Thinking.Effort` | the backend's own default | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. `none` is the only value that asks for no reasoning, and a model whose reasoning is mandatory answers it with a 400 rather than ignoring it (measured on `stealth/ox-alpha`, 2026-08-23). Empty hands the decision to the provider, which on a model whose reasoning is mandatory means that model's own maximum. Not validated against the model: OpenRouter clamps a level a model does not advertise rather than refusing it |
| `Thinking.Budget` | `0` (no ceiling from here) | Reasoning-token ceiling for one turn. The other spelling of `Effort`. Anthropic takes both at once; OpenRouter answers the pair with a 400 (`Only one of reasoning.effort and reasoning.max_tokens can be specified`, measured 2026-08-23) and its backend resolves that by letting the budget win, since the gateway defines each level as a percentage of the budget anyway. Refused at construction when it cannot work: at or above `MaxTokens` it leaves the turn nothing to answer with, and below a backend's `Capabilities.MinBudget` (1024 on `anthropic`) the API rejects it |
| `Thinking.Show` | `false` | Stream the model's reasoning as `KindThinking`. Off by default, matching the APIs: the raw chain of thought is never returned and a readable summary is opt-in. It changes what a consumer is shown and nothing else. The reasoning always travels back over the wire, because that is what the tool loop replays to keep the model's train of thought intact across a tool call |
| `MaxTokens` | `DefaultMaxTokens` (32000) | Per-turn output ceiling. Generous on purpose — every request is streamed, so a large ceiling costs nothing in latency, while a small one truncates an answer mid-sentence and buys a retry |
| `MaxIterations` | `0` (no cap) | How many times the model may be asked. A cap is only safe when every tool is read-only and cheap; reaching it ends the run with `Stop: StopIterations`, not a failure |
| `Tools` | `nil` | Tools the model may call in this process |
| `MCP` | `nil` | MCP servers the model may call tools on. Only `anthropic` can reach them — see [architecture.md](architecture.md#capabilities) |
| `Logger` | `slog.Default()` | Receives the few things worth recording that are not events — retry attempts, mainly |

`Backend`, `Thinking` and `MCP` are all checked against the chosen backend's
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
