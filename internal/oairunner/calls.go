// Package oairunner runs agents on OpenAI-compatible APIs.
package oairunner

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

type toolCall struct {
	id        string
	name      string
	arguments string
}

func (c toolCall) event(index int) nacelle.Event {
	return nacelle.Event{
		Kind: nacelle.KindToolCall,
		Tool: &nacelle.ToolEvent{ID: c.id, Index: index, Name: c.name, Input: c.arguments},
	}
}

type callContext struct {
	tools  []oai.ChatCompletionToolUnionParam
	byName map[string]nacelle.Tool
	sink   *nacelle.ToolSink
	out    *emitter
}

func planCalls(calls []toolCall, byName map[string]nacelle.Tool) ([]toolCall, []int) {
	if len(calls) <= 1 {
		indices := make([]int, len(calls))
		for i := range indices {
			indices[i] = i
		}
		return calls, indices
	}
	invocations := make([]nacelle.Invocation, len(calls))
	for i, c := range calls {
		invocations[i] = nacelle.Invocation{ID: c.id, Name: c.name, Index: i}
	}
	planned := nacelle.PlanCalls(invocations, byName)
	order := make([]toolCall, len(planned))
	indices := make([]int, len(planned))
	for i, p := range planned {
		order[i] = calls[p.Index]
		indices[i] = p.Index
	}
	return order, indices
}

func runCalls(ctx context.Context, calls []toolCall, call callContext) ([]oai.ChatCompletionMessageParamUnion, error) {
	order, indices := planCalls(calls, call.byName)
	results := make([]oai.ChatCompletionMessageParamUnion, 0, len(order))
	for i, invocation := range order {
		origIndex := indices[i]
		if !call.out.send(invocation.event(origIndex)) {
			return nil, errStopped
		}
		results = append(results, runCall(ctx, invocation, origIndex, call))
	}

	if !call.out.flushTools() {
		return nil, errStopped
	}
	return results, nil
}

func runCall(ctx context.Context, invocation toolCall, index int, call callContext) oai.ChatCompletionMessageParamUnion {
	tool, known := call.byName[invocation.name]
	if !known {
		err := fmt.Errorf("nacelle: no tool named %q is available", invocation.name)
		call.sink.Report(nacelle.Event{
			Kind: nacelle.KindToolResult,
			Tool: &nacelle.ToolEvent{
				ID: invocation.id, Index: index, Name: invocation.name,
				Input: invocation.arguments, Result: err.Error(), Err: err,
			},
		})
		return oai.ToolMessage(fmt.Sprintf("no tool named %q is available", invocation.name), invocation.id)
	}

	result, err := nacelle.RunTool(ctx, tool, nacelle.Invocation{ID: invocation.id, Name: invocation.name, Index: index}, json.RawMessage(invocation.arguments), call.sink)
	if err != nil {
		result = "the tool failed: " + err.Error()
	}
	return oai.ToolMessage(result, invocation.id)
}

func toolParams(tools []nacelle.Tool) []oai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	ordered := slices.Clone(tools)
	slices.SortStableFunc(ordered, func(a, b nacelle.Tool) int {
		return strings.Compare(a.Name(), b.Name())
	})

	params := make([]oai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range ordered {
		params = append(params, oai.ChatCompletionFunctionTool(oai.FunctionDefinitionParam{
			Name:        tool.Name(),
			Description: oai.String(tool.Description()),
			Parameters:  oai.FunctionParameters(tool.Schema()),
		}))
	}
	return params
}
