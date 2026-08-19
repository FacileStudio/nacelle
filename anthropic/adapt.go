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
func adapt(tools []nacelle.Tool, sink *nacelle.ToolSink, pending *invocations) []sdk.BetaTool {
	if len(tools) == 0 {
		return nil
	}
	ordered := slices.SortedFunc(slices.Values(tools), func(a, b nacelle.Tool) int {
		return strings.Compare(a.Name(), b.Name())
	})
	adapted := make([]sdk.BetaTool, 0, len(ordered))
	for _, tool := range ordered {
		adapted = append(adapted, sdkTool{tool: tool, sink: sink, pending: pending})
	}
	return adapted
}

// sdkTool presents a nacelle.Tool to the SDK's runner.
type sdkTool struct {
	tool    nacelle.Tool
	sink    *nacelle.ToolSink
	pending *invocations
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

// Execute runs the tool and reports it, having first looked up which call it
// is answering.
//
// The lookup is here because the runner does not tell a handler which
// tool_use block it was started for, and a result that cannot name its call
// cannot be paired with it by anyone downstream. It runs on one of the
// runner's parallel goroutines, which is why the registry it reads is locked.
//
// The lookup also hands back the bytes the model wrote, and those are what the
// tool runs on rather than the ones the runner passed in. The runner decodes
// the streamed block and re-encodes it, so its bytes are the same arguments in
// a different spelling — and a consumer diffing KindToolCall.Input against
// KindToolResult.Input would see a change that never happened. One string for
// both is what the OpenRouter backend reports, and there is no reason for a
// consumer to have to know which backend it is reading.
func (t sdkTool) Execute(ctx context.Context, input json.RawMessage) ([]sdk.BetaToolResultBlockParamContentUnion, error) {
	call, arguments := t.pending.take(t.tool.Name(), input)
	result, err := nacelle.RunTool(ctx, t.tool, call, arguments, t.sink)
	if err != nil {
		return nil, err
	}
	return []sdk.BetaToolResultBlockParamContentUnion{
		{OfText: &sdk.BetaTextBlockParam{Text: result}},
	}, nil
}

// toParams converts a conversation into the SDK's message shape.
func toParams(conversation []nacelle.Message) []sdk.BetaMessageParam {
	params := make([]sdk.BetaMessageParam, 0, len(conversation))
	for _, message := range conversation {
		role := sdk.BetaMessageParamRoleUser
		if message.Assistant {
			role = sdk.BetaMessageParamRoleAssistant
		}
		params = append(params, sdk.BetaMessageParam{
			Role:    role,
			Content: []sdk.BetaContentBlockParamUnion{sdk.NewBetaTextBlock(message.Text)},
		})
	}
	return params
}
