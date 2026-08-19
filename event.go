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
}

// ToolEvent is a tool call, before or after it ran.
type ToolEvent struct {
	// ID is the model's identifier for this call. A KindToolResult carries
	// the same ID as the KindToolCall it answers, which is what lets a
	// consumer pair them without tracking order.
	ID string

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
}

// Add returns the sum of two usages.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:         u.InputTokens + other.InputTokens,
		OutputTokens:        u.OutputTokens + other.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens + other.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens + other.CacheCreationTokens,
	}
}

// Total is every token the run was billed for.
func (u Usage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}
