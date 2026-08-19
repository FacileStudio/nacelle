package nacelle

import (
	"context"
	"iter"
)

// Stream runs the conversation and yields what happens as it happens.
//
// The sequence ends after a KindDone event, or early with a non-nil error. A
// consumer that stops ranging cancels the run: the underlying request is torn
// down with the context, so abandoning the loop is a supported way to stop an
// agent rather than a leak.
//
// Tool failures are not stream errors. A tool that returns an error is
// reported as a KindToolResult carrying it and handed back to the model, which
// is better placed than the caller to decide whether the task can still be
// finished. An error out of this sequence means the run itself failed.
func (a *Agent) Stream(ctx context.Context, conversation []Message) iter.Seq2[Event, error] {
	request := a.request
	request.Messages = conversation
	return a.backend.Stream(ctx, request)
}
