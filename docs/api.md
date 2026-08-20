# nacelle — API

The complete exported surface, package by package. Five packages importable under
`github.com/FacileStudio/nacelle/<package>`, plus `tui`, which is a separate module
(`github.com/FacileStudio/nacelle/tui`) with its own `go.mod` and Go floor. Behaviour and
rationale live in [architecture.md](architecture.md); this page is the reference.

## `nacelle`

### Agent

```go
type Agent struct { /* unexported */ }

func New(cfg Config) (*Agent, error)
func (a *Agent) Stream(ctx context.Context, conversation []Message) iter.Seq2[Event, error]
func (a *Agent) CountTokens(ctx context.Context, conversation []Message) (int64, error)
func (a *Agent) Backend() Backend
```

`New` fails rather than degrading: a nil `Backend`, an empty `System`, a duplicate tool name, a
duplicate MCP server name, or a `Config` asking for a capability the backend lacks all return
an error instead of a half-configured agent. `Stream` refuses a conversation with a part on
the wrong side of its role before the backend ever sees it — see
[The `Message` union](architecture.md#the-message-union). `CountTokens` builds the same
request `Stream` would — system prompt, tools, MCP servers, not just the bare conversation —
and asks the backend how many tokens it would use; a backend that cannot answer (see
`Capabilities.TokenCounting`) returns an `*Unsupported` error.

```go
func Trim(conversation []Message, keep int) (kept []Message, dropped int)
```

Drops the oldest messages, keeping at most `keep` of the most recent — a hard ceiling, useful
for trimming to a token budget. Truncation, not summarization: deciding what to preserve and
how to compress it is a product opinion this package holds nowhere else. Never returns a slice
starting on a `ToolResult` message without the `ToolCall` before it; when the requested
boundary would land inside that pair, it drops the whole pair rather than leaving a
conversation every provider here would reject.

```go
var (
	ErrNoBackend      = errors.New("nacelle: a backend is required")
	ErrNoSystemPrompt = errors.New("nacelle: a system prompt is required")
)

type Unsupported struct {
	Backend string
	Feature string
}
func (e *Unsupported) Error() string
```

```go
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

const DefaultMaxTokens = 32000
```

### Config

```go
type Config struct {
	Backend       Backend       // required
	System        string        // required
	Effort        Effort        // defaults to the backend's own
	Thinking      bool          // default false
	MaxTokens     int64         // default DefaultMaxTokens
	MaxIterations int           // default 0, no cap
	Tools         []Tool
	MCP           []mcp.Server
	Logger        *slog.Logger  // default slog.Default()
}
```

Full field-by-field description: [configuration.md](configuration.md#nacelleconfig).

### Backend

```go
type Backend interface {
	Name() string
	Capabilities() Capabilities
	Stream(ctx context.Context, request Request) iter.Seq2[Event, error]
	CountTokens(ctx context.Context, request Request) (int64, error)
}

type Capabilities struct {
	MCP           bool
	Thinking      bool
	Effort        bool
	Cost          bool
	TokenCounting bool
}

type Request struct {
	System        string
	Messages      []Message
	Tools         []Tool
	MCP           []mcp.Server
	Effort        Effort
	Thinking      bool
	MaxTokens     int64
	MaxIterations int
}
```

### Message and Part

```go
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role  Role
	Parts []Part
}

type Part interface{ /* sealed to this package */ }

type Text struct{ Text string }
type Reasoning struct{ Text string }

type ToolCall struct {
	ID       string
	Name     string
	Input    json.RawMessage
	Finished bool // false for a call still streaming when the run ended
}

type ToolResult struct {
	ID     string
	Name   string
	Result string
	Failed bool
}

type Finish struct{ Stop Stop }

func UserText(text string) Message      // Message{RoleUser, []Part{Text{text}}}
func AssistantText(text string) Message // Message{RoleAssistant, []Part{Text{text}}}
```

### Event

```go
type Kind string

const (
	KindText       Kind = "text"
	KindThinking   Kind = "thinking"
	KindToolCall   Kind = "tool_call"
	KindToolResult Kind = "tool_result"
	KindTurn       Kind = "turn"
	KindDone       Kind = "done"
)

type Stop string

const (
	StopEnd        Stop = "end"
	StopTools      Stop = "tools"
	StopMaxTokens  Stop = "max_tokens"
	StopContext    Stop = "context"
	StopRefusal    Stop = "refusal"
	StopIterations Stop = "iterations"
	StopOther      Stop = "other"
)
func (s Stop) Complete() bool // true only for StopEnd

type Event struct {
	Kind  Kind
	Text  string    // delta, for KindText and KindThinking
	Tool  *ToolEvent // for KindToolCall and KindToolResult
	Usage Usage      // for KindTurn (this turn) and KindDone (run total)
	Stop  Stop       // for KindTurn and KindDone
}

type ToolEvent struct {
	ID        string
	Index     int
	Name      string
	Input     string // raw JSON the model produced
	Result    string // KindToolResult only
	Err       error
	Duration  time.Duration
	Discarded bool // true when this call never ran — see architecture.md
}
```

### Usage

```go
type Usage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	Cost                float64 // US dollars; only when Capabilities.Cost
}

func (u Usage) Add(other Usage) Usage
func (u Usage) Total() int64          // every token billed
func (u Usage) CacheHitRate() float64 // 0–1, excludes write tokens from the denominator
```

### Tool

```go
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Run(ctx context.Context, input json.RawMessage) (string, error)
}

func NewTool[In any](name, description string, run func(ctx context.Context, in In) (string, error)) (Tool, error)
```

`In` must be a struct — a model calls a tool by naming arguments, and a bare string or slice
has no names to give. The schema is generated from `In`'s `json` and `jsonschema` struct tags.

### ToolSink, Invocation, RunTool

For backend implementors; not otherwise interesting to a consumer.

```go
type ToolSink struct{ /* unexported */ }
func (s *ToolSink) Report(event Event)
func (s *ToolSink) Drain() []Event // sorted by ToolEvent.Index, since the last call

type Invocation struct {
	ID    string
	Index int
}

func RunTool(ctx context.Context, tool Tool, call Invocation, input json.RawMessage, sink *ToolSink) (string, error)
func ToolsByName(tools []Tool) map[string]Tool
```

### Retry

```go
const (
	DefaultRetryAttempts = 3
	DefaultRetryBase     = 500 * time.Millisecond
	DefaultRetryMax      = 8 * time.Second
)

type RetryOptions struct {
	Attempts int           // default DefaultRetryAttempts; 1 disables retrying
	Base     time.Duration // default DefaultRetryBase; doubles per attempt
	Max      time.Duration // default DefaultRetryMax
	Logger   *slog.Logger  // default slog.Default()
}

func Retry(backend Backend, options RetryOptions) Backend
```

Every zero field in `RetryOptions` takes its default, so the zero value is the recommended
policy rather than no policy.

```go
func Transient(err error) error // marks err as worth retrying, keeps Unwrap intact
func Retryable(err error) bool  // true for a Transient-marked error not yet exhausted
func Attempt(err error) int     // which attempt an exhausted error gave up on, 0 otherwise
```

### mcp (sub-package `github.com/FacileStudio/nacelle/mcp`)

```go
type Server struct {
	Name         string // must be unique in one agent
	URL          string
	Token        string   // bearer credential; leave empty for a server that needs none
	AllowedTools []string // empty allows every tool the server exposes
}

func Validate(servers []Server) error
```

## `anthropic`

```go
const DefaultModel = "claude-opus-5"

type Config struct {
	Client *sdk.Client // *anthropic-sdk-go client; nil builds one from the environment
	Model  string      // defaults to DefaultModel
}

type Backend struct{ /* unexported */ }

func New(cfg Config) *Backend
func (b *Backend) Name() string             // "anthropic"
func (b *Backend) Capabilities() nacelle.Capabilities // {MCP: true, Thinking: true, Effort: true}
func (b *Backend) Model() string
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error]
```

The full-capability backend: it runs on the Anthropic SDK's own `BetaToolRunnerStreaming`
rather than a hand-rolled loop, which is also where MCP support comes from. `Capabilities.Cost`
is false — the API returns tokens and nothing else; the caller prices them.

## `openrouter`

```go
const DefaultBaseURL = "https://openrouter.ai/api/v1"

type Config struct {
	APIKey   string            // defaults to OPENROUTER_API_KEY
	Model    string            // required — an OpenRouter model slug
	BaseURL  string            // defaults to DefaultBaseURL
	Referer  string            // sent as HTTP-Referer, attribution only
	Title    string            // sent as X-OpenRouter-Title, attribution only
	Provider map[string]any    // OpenRouter's provider-routing object, passed through
	Options  []option.RequestOption
}

type Backend struct{ /* unexported */ }

func New(cfg Config) (*Backend, error)
func (b *Backend) Name() string             // "openrouter"
func (b *Backend) Capabilities() nacelle.Capabilities // {MCP: false, Thinking: true, Effort: true, Cost: true}
func (b *Backend) Model() string
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error]
```

`New` fails rather than degrading: a missing model or API key returns an error immediately
rather than a 401 on the first turn. Drives the tool-call loop itself, turn by turn — there is
no `/v1/messages` and no server-side MCP connector behind the OpenAI-compatible schema.

## `tools`

```go
const (
	DefaultMaxOutputBytes = 64 * 1024
	DefaultMaxReadBytes   = 48 * 1024
	DefaultCommandTimeout = 2 * time.Minute
)

type Config struct {
	Root           string        // required — every tool is confined here via os.Root
	AllowBash      bool          // default false; mounts the command tool
	CommandEnv     []string      // default PATH + HOME only, not the process environment
	CommandTimeout time.Duration // default DefaultCommandTimeout
	MaxOutputBytes int           // default DefaultMaxOutputBytes
	MaxReadBytes   int           // default DefaultMaxReadBytes
}

type Set struct{ /* unexported */ }

func New(cfg Config) (*Set, error)
func (s *Set) Close() error
func (s *Set) Dir() string
func (s *Set) Tools() ([]nacelle.Tool, error)    // everything, honouring Config.AllowBash
func (s *Set) ReadOnly() ([]nacelle.Tool, error) // read, find, search — nothing that can write

func Jardin() ([]nacelle.Tool, error)
func WebSearch(endpoint string) ([]nacelle.Tool, error)
func WebFetch() ([]nacelle.Tool, error)
```

`Tools()` returns: `read_file`, `write_file`, `edit_file` (exact-match replace, refuses an
ambiguous or absent match), `list_directory` (one directory, subdirectories marked), `find_files`
(glob, files only), `search_content` (grep), and — only when `AllowBash` — `run_command`. No tool takes a `path`, `cwd` or `root` argument the model could
nominate itself; see [architecture.md](architecture.md#the-tool-call-loop) for why that
specific restriction is load-bearing (CVE-2025-59532).

`Jardin()` is independent of `Set` — no root, no config — and returns `list_flows`, `run_flow`
and `search_memory` when the `jardin` binary is on `PATH`, or `nil, nil` when it is not. Each
shells out via `exec.CommandContext` with argv arguments, never a shell string, so nothing the
model puts in a flow name or a search query reaches a shell to be interpreted.

`WebSearch(endpoint)` is independent of `Set` in the same way, and returns one tool,
`web_search`, against a [SearXNG](https://docs.searxng.org) instance. An empty endpoint returns
`nil, nil` — the `Jardin()` convention for "not configured here" — while an endpoint that could
never work (no scheme, no host) is an error at construction rather than a tool that fails on
first use. There is deliberately no default: this module is public, so a default would send a
stranger's queries to one operator's host.

The endpoint is a constructor argument and never a tool argument, for the reason `Root` is:
a model able to name the host a request goes to has been handed the boundary along with the
thing inside it. The instance needs `json` in `search.formats` in its `settings.yml`, which is
off by default — the error says so by name, as does the one for the limiter's 403. Results are
capped at `DefaultSearchResults` and are untrusted text from strangers, which is worth weighing
when deciding what else to mount beside it.

`WebFetch()` returns one tool, `web_fetch`, which reads a single page by URL: `text/markdown`
first in the `Accept` header (Cloudflare and Vercel convert at the edge when asked, for roughly
80% fewer tokens), HTML otherwise, rendered to text keeping headings, lists, code blocks and
links resolved against the page's own URL. Non-text types are refused by name rather than
returned as bytes, and a page that renders itself with JavaScript is reported as such instead of
coming back empty.

It is the only tool in this package whose destination the model names, so the address check is
in the dialer's `Control` hook — after resolution, before connect, on every redirect hop, which
is the only placement that is neither a hostname check nor a race against the second DNS lookup.
It refuses loopback, private, link-local (including `169.254.169.254`), unspecified, multicast
and the special-use ranges `net/netip` has no predicate for, among them `198.18.0.0/15`, which
has been a real bypass elsewhere. Pages are parsed with `html.Parse` rather than tokenised,
because real pages leave tags unclosed and only the tree-construction algorithm closes them the
way a browser does.

## `tui`

A `main` package, not a library — nothing here is importable. `go build -o` it directly; see
[development.md](development.md) and the root [`README.md`](../README.md#the-terminal-client)
for the flags, environment variables and config file it reads.
