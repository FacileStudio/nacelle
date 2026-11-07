package nacelle

import (
	"context"
	"fmt"
	"time"
)

// call runs one tool call through the hooks, Approve and the tool, reports
// its outcome to the sink, and returns what the model reads.
//
// It is the one place a tool runs, whichever provider asked for it, so a
// refusal, an edited input or a redirect looks the same everywhere. A refusal
// is reported as a failed call, not skipped in silence: a call started must
// be closed, and Refused tells a consumer it was a policy decision. A tool
// that returns EndTurn is recorded as a success with the result it carried.
func (s *sink) call(ctx context.Context, call Call) ToolResult {
	tool, known := s.tools[call.Name]
	if !known {
		err := fmt.Errorf("nacelle: no tool named %q is available", call.Name)
		s.report(toolResultEvent(call, err.Error(), err))
		return failed(call, err)
	}
	ruled, err := s.rule(ctx, tool, call)
	if err != nil {
		event := toolResultEvent(ruled.call, err.Error(), err)
		event.Tool.Refused = true
		s.report(event)
		return failed(ruled.call, err)
	}

	started := time.Now()
	result, images, err := s.invoke(ctx, ruled.tool, ruled.call)
	result, err = s.settle(result, err, ruled.note)
	result = s.runAfterHooks(ctx, ruled.call, result, err)
	event := toolResultEvent(ruled.call, result, err)
	event.Tool.Duration = time.Since(started)
	event.Tool.Images = images
	s.report(event)
	if err != nil {
		return failed(ruled.call, err)
	}
	return ToolResult{ID: call.ID, Name: ruled.call.Name, Result: result, Images: images}
}

// failed is the answer the model reads for a call that did not succeed.
func failed(call Call, err error) ToolResult {
	return ToolResult{ID: call.ID, Name: call.Name, Result: err.Error(), Failed: true}
}

// settle turns an EndTurn into the successful result it carries, marking the
// run as yielded, and prepends the reviewer's note to what the model reads.
func (s *sink) settle(result string, err error, note string) (string, error) {
	if ended, ok := endedTurn(err); ok {
		result, err = ended, nil
		s.markYielded()
	}
	switch {
	case note == "":
		return result, err
	case err != nil:
		return result, fmt.Errorf("%s\n\n%w", note, err)
	}
	return note + "\n\n" + result, nil
}

// invoke runs the tool, routing what it emits to the stream as output.
func (s *sink) invoke(ctx context.Context, tool Tool, call Call) (string, []Image, error) {
	result, err := tool.Run(ctx, call, func(fragment string) {
		if fragment == "" {
			return
		}
		s.report(Event{
			Kind: KindToolOutput,
			Tool: &ToolEvent{ID: call.ID, Index: call.Index, Name: call.Name},
			Text: fragment,
		})
	})
	return result.Text, result.Images, err
}

// toolResultEvent builds the one event shape every outcome of a tool call
// arrives as, naming the tool and input that actually ran.
func toolResultEvent(call Call, result string, err error) Event {
	return Event{
		Kind: KindToolResult,
		Tool: &ToolEvent{
			ID: call.ID, Index: call.Index, Name: call.Name,
			Input: string(call.Input), Result: result, Err: err,
		},
	}
}

// plan splits a round's calls into the read-only ones, which run first and
// together, and the rest, which run after them one at a time in the model's
// order. A call to a tool this run does not have is not read-only: it has no
// declaration to trust.
func plan(calls []Call, tools map[string]Tool) (readers, writers []int) {
	for position, call := range calls {
		if tool, ok := tools[call.Name]; ok && tool.Spec().ReadOnly {
			readers = append(readers, position)
			continue
		}
		writers = append(writers, position)
	}
	return readers, writers
}
