package nacelle

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// call runs one resolved call through the hooks, Approve and the tool, reports
// its outcome to the sink, and returns what the model reads.
//
// It is the one place a tool runs, whichever provider asked for it, so a
// refusal, an edited input or a redirect looks the same everywhere. A refusal
// is reported as a failed call, not skipped in silence: a call started must
// be closed, and Refused tells a consumer it was a policy decision. A tool
// that returns EndTurn is recorded as a success with the result it carried.
func (s *sink) call(ctx context.Context, resolved target) ToolResult {
	if resolved.err != nil {
		s.report(toolResultEvent(resolved.call, resolved.err.Error(), resolved.err))
		return failed(resolved.call, resolved.err)
	}
	ruled, err := s.rule(ctx, resolved.tool, resolved.call)
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
	return ToolResult{ID: ruled.call.ID, Name: ruled.call.Name, Result: result, Images: images}
}

// errNoTool is the error the model reads for a name nothing answers to.
func errNoTool(name string) error {
	return fmt.Errorf("nacelle: no tool named %q is available", name)
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

// round runs every target, the read-only ones first and together, and
// writes each answer at its call's position.
func (s *sink) round(ctx context.Context, targets []target, results []Part) {
	readers, writers := plan(targets)
	var group sync.WaitGroup
	for _, position := range readers {
		group.Go(func() { results[position] = s.call(ctx, targets[position]) })
	}
	group.Wait()
	for _, position := range writers {
		results[position] = s.call(ctx, targets[position])
	}
}

// plan splits a round's calls into the read-only ones, which run first and
// together, and the rest, which run after them one at a time in the model's
// order. A call that resolved to no tool is not read-only: it has no
// declaration to trust.
func plan(targets []target) (readers, writers []int) {
	for position, resolved := range targets {
		if resolved.tool != nil && resolved.tool.Spec().ReadOnly {
			readers = append(readers, position)
			continue
		}
		writers = append(writers, position)
	}
	return readers, writers
}
