package anthropic

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// adapt wraps nacelle tools in the shape the SDK's runner executes.
//
// Wrapping is also the only place a tool's result is visible: the runner owns
// execution and appends the result block itself, so a consumer watching the
// message stream sees every call and never an answer.
//
// They are sorted by name because tools render first in the request, which
// makes them the front of every cached prefix. Left in the caller's order, two
// agents configured with the same tools in a different order would share no
// cache at all, and nothing in the result would say why.
func adapt(tools []nacelle.Tool, sink *nacelle.ToolSink) []sdk.BetaTool {
	if len(tools) == 0 {
		return nil
	}
	ordered := slices.SortedFunc(slices.Values(tools), func(a, b nacelle.Tool) int {
		return strings.Compare(a.Name(), b.Name())
	})
	adapted := make([]sdk.BetaTool, 0, len(ordered))
	for _, tool := range ordered {
		adapted = append(adapted, sdkTool{tool: tool, sink: sink})
	}
	return adapted
}

// sdkTool presents a nacelle.Tool to the SDK's runner.
type sdkTool struct {
	tool nacelle.Tool
	sink *nacelle.ToolSink
}

func (t sdkTool) Name() string        { return t.tool.Name() }
func (t sdkTool) Description() string { return t.tool.Description() }

func (t sdkTool) InputSchema() sdk.BetaToolInputSchemaParam {
	schema := t.tool.Schema()
	input := sdk.BetaToolInputSchemaParam{}
	if properties, ok := schema["properties"]; ok {
		input.Properties = properties
	}
	if required, ok := schema["required"].([]any); ok {
		names := make([]string, 0, len(required))
		for _, name := range required {
			if text, ok := name.(string); ok {
				names = append(names, text)
			}
		}
		input.Required = names
	}
	return input
}

func (t sdkTool) Execute(ctx context.Context, input json.RawMessage) ([]sdk.BetaToolResultBlockParamContentUnion, error) {
	result, err := nacelle.RunTool(ctx, t.tool, "", input, t.sink)
	if err != nil {
		return nil, err
	}
	return []sdk.BetaToolResultBlockParamContentUnion{
		{OfText: &sdk.BetaTextBlockParam{Text: result}},
	}, nil
}
