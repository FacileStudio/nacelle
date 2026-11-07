package nacelle

import "time"

// Kind identifies what an Event carries. Switch on it before reading any other
// field: every field but Kind is meaningful for some kinds and zero for the
// rest.
type Kind string

const (
	// KindText is a fragment of the answer. Text holds the delta, not the
	// whole answer so far — a consumer that wants the total accumulates.
	KindText Kind = "text"

	// KindThinking is a fragment of Claude's reasoning, and arrives only
	// when the request asked for a visible summary. The raw chain of
	// thought is never returned by the API under any setting.
	KindThinking Kind = "thinking"

	// KindToolCall is the model deciding to use a tool. It is emitted
	// before the tool runs, so a consumer can show the intent while the
	// work happens.
	KindToolCall Kind = "tool_call"

	// KindToolOutput is a fragment of a tool's output, streamed while an
	// output-streaming tool runs. Text holds the delta. It is display-only:
	// never replayed into the conversation and never counted in usage. The
	// tool's complete return value still arrives whole on the matching
	// KindToolResult, so a consumer can ignore these and keep the one-event
	// behaviour.
	KindToolOutput Kind = "tool_output"

	// KindToolResult is that tool having finished, successfully or not.
	KindToolResult Kind = "tool_result"

	// KindTurn ends one assistant turn and carries what that turn cost. A
	// turn that used tools is followed by more turns; the last one is
	// followed by KindDone.
	KindTurn Kind = "turn"

	// KindMessage carries a message that is now part of the transcript: the
	// assistant turn once it has ended, and the user turn of tool results
	// once every call in the round has answered. It arrives after the events
	// that built it, and only once nothing can take it back, so a consumer
	// that appends every Message has the conversation the model saw, with
	// reasoning, tool images and each turn's Finish included.
	KindMessage Kind = "message"

	// KindDone ends the run and carries the total cost of every turn in it.
	KindDone Kind = "done"
)

// Stop is why a turn or a run ended.
//
// It exists because the alternative is silence: a run truncated by the output
// ceiling, one that outgrew the context window, and one the model refused all
// arrive as a well-formed response with a normal ending, and a consumer that
// does not read this cannot tell any of them from a finished answer.
type Stop string

const (
	// StopEnd is the model having finished what it was asked.
	StopEnd Stop = "end"

	// StopTools ends a turn the model wants tools run for. More turns
	// follow, so it is never the reason a run ended.
	StopTools Stop = "tools"

	// StopMaxTokens is the output ceiling cutting the answer off.
	StopMaxTokens Stop = "max_tokens"

	// StopContext is the conversation having outgrown the context window.
	StopContext Stop = "context"

	// StopRefusal is the model declining, which arrives as a successful
	// response carrying no answer.
	StopRefusal Stop = "refusal"

	// StopIterations is MaxIterations reached with the model still asking
	// for tools. The work is unfinished and nothing went wrong.
	StopIterations Stop = "iterations"

	// StopYield is a tool having ended the run through EndTurn.
	StopYield Stop = "yield"

	// StopOther is a reason this package does not have a name for. It is
	// not an error, and a consumer should treat it as unfinished.
	StopOther Stop = "other"
)

// Complete reports whether the answer is whole. Anything else means the run
// stopped short, and only StopEnd is safe to present as a finished answer.
func (s Stop) Complete() bool { return s == StopEnd }

// Source is where a tool call came from.
//
// It is zero for local tools and ToolSourceMCP for tools reached through
// MCP, which is the only distinction consumers currently need.
type Source string

const (
	// ToolSourceLocal is a tool defined in this process.
	ToolSourceLocal Source = ""

	// ToolSourceMCP is a tool call or result that the model reached over
	// MCP rather than from the agent's local registry.
	ToolSourceMCP Source = "mcp"
)

// Event is one thing that happened during a run.
//
// The stream is the only output of an agent: SSE, a terminal, a log and a test
// are all consumers of this type, which is what keeps the loop free of any
// opinion about where its output goes.
type Event struct {
	Kind Kind

	// Text is the delta for KindText, KindThinking and KindToolOutput.
	Text string

	// Tool describes the call for KindToolCall, KindToolOutput and
	// KindToolResult.
	Tool *ToolEvent

	// Usage is the turn's cost for KindTurn, and the run's total for
	// KindDone. It is zero on every other kind.
	Usage Usage

	// Stop is why a turn or a run ended, on KindTurn and KindDone. It is
	// empty on every other kind.
	Stop Stop

	// Message is the committed message, on KindMessage only.
	Message *Message
}

// ToolEvent is a tool call, before or after it ran.
type ToolEvent struct {
	// ID is the model's identifier for this call. A KindToolResult carries
	// the same ID as the KindToolCall it answers, which is what lets a
	// consumer pair them without tracking order.
	ID string

	// Index is the call's position in the turn that asked for it.
	//
	// Tools run concurrently, so results are emitted in the order they
	// finish rather than the order they were asked for — which is real
	// information, and holding it back until the slowest one lands would
	// buy determinism with a UI that stops moving. Index is how a consumer
	// that wants the model's order gets it without paying for that.
	Index int

	// Name is the tool's name, as the server exposes it for MCP. On
	// KindToolResult it is the tool that ran, after any Approve redirect.
	Name string

	// Input is the raw JSON the model produced, undecoded because the core
	// knows no tool's schema. On KindToolResult it is the input that ran.
	Input string

	// Result is what the tool returned, on KindToolResult only.
	Result string

	// Images are the pictures the tool returned beside Result, on
	// KindToolResult only.
	Images []Image

	// Err is non-nil when the tool failed. The run continues: a failed tool
	// is reported back to the model, which is usually better placed than
	// the caller to decide whether the task can still be finished.
	Err error

	// Duration is how long the tool took, on KindToolResult only.
	Duration time.Duration

	// Discarded reports that this call never ran: an attempt that produced
	// it was superseded before it could be executed. It arrives as a
	// KindToolResult only because the consumer's pairing contract still
	// applies — a call started must be closed — not because there is
	// anything here worth believing. A consumer replaying its conversation
	// should drop a discarded call and its close entirely, the same way the
	// backend that discarded it never replays it either.
	Discarded bool

	// Refused reports that a hook or Config.Approve stopped this call
	// before it ran: a policy decision, not a bug. Err is what the model reads.
	Refused bool

	// Source is empty for local tools and ToolSourceMCP for MCP tools.
	Source Source
}
