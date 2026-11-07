package oairunner

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

// toolCallEvent announces a call whose arguments are whole.
func toolCallEvent(call nacelle.ToolCall, index int) nacelle.Event {
	return nacelle.Event{
		Kind: nacelle.KindToolCall,
		Tool: &nacelle.ToolEvent{ID: call.ID, Index: index, Name: call.Name, Input: string(call.Input)},
	}
}

// toolCallsOf reads the calls off an accumulated message, dropping the empty
// entries a provider can close a stream with.
func toolCallsOf(message oai.ChatCompletionMessage) []nacelle.ToolCall {
	kept := slices.DeleteFunc(message.ToolCalls, func(call oai.ChatCompletionMessageToolCallUnion) bool {
		return call.ID == "" && call.Function.Name == "" && call.Function.Arguments == ""
	})
	calls := make([]nacelle.ToolCall, 0, len(kept))
	for _, call := range kept {
		calls = append(calls, nacelle.ToolCall{
			ID: call.ID, Name: call.Function.Name,
			Input: json.RawMessage(call.Function.Arguments), Finished: true,
		})
	}
	return calls
}

// toolParams declares the tools, sorted by name so the request prefix is
// stable from one turn to the next.
func toolParams(tools []nacelle.Tool) []oai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	ordered := slices.Clone(tools)
	slices.SortStableFunc(ordered, func(a, b nacelle.Tool) int {
		return strings.Compare(a.Spec().Name, b.Spec().Name)
	})

	params := make([]oai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range ordered {
		spec := tool.Spec()
		params = append(params, oai.ChatCompletionFunctionTool(oai.FunctionDefinitionParam{
			Name:        spec.Name,
			Description: oai.String(spec.Description),
			Parameters:  oai.FunctionParameters(spec.Schema),
		}))
	}
	return params
}
