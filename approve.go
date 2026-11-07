package nacelle

import (
	"context"
	"encoding/json"
	"errors"
)

// Call is one local tool call as the model asked for it, before it runs.
//
// It is what Approve and the tool hooks are shown, and the one value that
// identifies a call everywhere: ID pairs it with its KindToolResult, Name and
// Input are what will run, and Index is where the model put it in the turn.
type Call struct {
	ID    string
	Name  string
	Index int
	Input json.RawMessage
}

// Decision is what an Approve callback rules about one call.
//
// The zero value approves the call unchanged. Deny, when non-empty, refuses
// it and is the reason the model reads. Name redirects the call to another
// registered tool, and Input replaces the arguments, so a reviewer can edit a
// call rather than only accept or refuse it. Note is prepended to what the
// model reads back, which is how it learns that the call it made is not the
// call that ran.
type Decision struct {
	Deny  string
	Name  string
	Input json.RawMessage
	Note  string
}

// Approve decides whether a tool call may run, and in what form, asked once
// per call before RunTool ever calls Run.
//
// Nil is the default and means every call runs unasked. Most consumers (a
// server, a CI job, an unattended run) have nobody to ask, and a package that
// refused by default would make every one of them write a rubber-stamp
// callback just to get back to how every tool already worked.
//
// It is asked with the same context RunTool receives, so cancelling a run
// unblocks anyone waiting on an answer that is never coming.
//
// It may be asked from several goroutines at once, for the reason Tool.Run
// documents, and a callback that puts a question to a person has to serialise
// the prompts itself.
type Approve func(ctx context.Context, call Call) Decision

// ApproveBool adapts a yes-or-no callback to Approve, for a consumer that
// never edits a call. A false answer denies the call with a generic reason.
func ApproveBool(allow func(ctx context.Context, name string, input json.RawMessage) bool) Approve {
	return func(ctx context.Context, call Call) Decision {
		if allow(ctx, call.Name, call.Input) {
			return Decision{}
		}
		return Decision{Deny: "the call was not approved"}
	}
}

// endTurn is the error EndTurn returns. It carries the result the model reads.
type endTurn struct{ result string }

func (e *endTurn) Error() string { return "nacelle: the tool ended the turn" }

// EndTurn is what a tool returns to finish its call with result and then end
// the run, without asking the model for another turn.
//
// The call is recorded as a successful result, the other calls in the same
// round still finish, and the run ends with a KindDone whose Stop is
// StopYield. A consumer that asked the user a question uses it to hand the
// turn back and resume the conversation once the answer arrives.
func EndTurn(result string) error { return &endTurn{result: result} }

// endedTurn reports whether err is, or wraps, an EndTurn, and its result.
func endedTurn(err error) (string, bool) {
	var ended *endTurn
	if errors.As(err, &ended) {
		return ended.result, true
	}
	return "", false
}
