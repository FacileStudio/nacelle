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

	// KindToolResult is that tool having finished, successfully or not.
	KindToolResult Kind = "tool_result"

	// KindTurn ends one assistant turn and carries what that turn cost. A
	// turn that used tools is followed by more turns; the last one is
	// followed by KindDone.
	KindTurn Kind = "turn"

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

	// StopOther is a reason this package does not have a name for. It is
	// not an error, and a consumer should treat it as unfinished.
	StopOther Stop = "other"
)

// Complete reports whether the answer is whole. Anything else means the run
// stopped short, and only StopEnd is safe to present as a finished answer.
func (s Stop) Complete() bool { return s == StopEnd }

// Event is one thing that happened during a run.
//
// The stream is the only output of an agent: SSE, a terminal, a log and a test
// are all consumers of this type, which is what keeps the loop free of any
// opinion about where its output goes.
type Event struct {
	Kind Kind

	// Text is the delta for KindText and KindThinking.
	Text string

	// Tool describes the call for KindToolCall and KindToolResult.
	Tool *ToolEvent

	// Usage is the turn's cost for KindTurn, and the run's total for
	// KindDone. It is zero on every other kind.
	Usage Usage

	// Stop is why a turn or a run ended, on KindTurn and KindDone. It is
	// empty on every other kind.
	Stop Stop
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

	// Name is the tool's name. For a tool reached over MCP it is the name
	// the server exposes.
	Name string

	// Input is the raw JSON the model produced. It is not decoded here
	// because the core does not know any tool's schema.
	Input string

	// Result is what the tool returned, on KindToolResult only.
	Result string

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
}

// Usage is what a turn or a run cost.
//
// It is reported on every turn rather than only at the end because comparing
// runs on cost is one of the reasons this package exists, and a total that has
// to be reconstructed afterwards is a total nobody trusts.
type Usage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64

	// Cost is what the run was charged, in US dollars.
	//
	// Only backends whose Capabilities report Cost fill it; the rest leave
	// it zero and the caller prices the tokens itself. It is here rather
	// than left to every consumer because a gateway that already knows the
	// number is more trustworthy than a price table copied into an app and
	// then not updated.
	//
	// It is a float64, which means adding two costs does not always give
	// the decimal you expect — 0.0001 plus 0.0002 is 0.00030000000000000003.
	// That is deliberate: the wire format is a JSON number, the error is
	// around one part in 1e16, and the question this field answers is which
	// of two runs cost more. Do not use it as a ledger; compare with a
	// tolerance, and bill from the provider's own records.
	Cost float64
}

// Add returns the sum of two usages.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:         u.InputTokens + other.InputTokens,
		OutputTokens:        u.OutputTokens + other.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens + other.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens + other.CacheCreationTokens,
		Cost:                u.Cost + other.Cost,
	}
}

// Total is every token the run was billed for.
func (u Usage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

// CacheHitRate is the share of input this run read from cache rather than
// paid full price for, from 0 to 1.
//
// It excludes CacheCreationTokens from the denominator on purpose: a write is
// not a hit, and counting it as attempted-but-missed would understate the
// rate on a run's first turn, when every prefix is written and none can have
// been read yet. Zero on a run with no cacheable input at all — that is not
// a miss, there was nothing to hit.
func (u Usage) CacheHitRate() float64 {
	eligible := u.InputTokens + u.CacheReadTokens
	if eligible == 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(eligible)
}
