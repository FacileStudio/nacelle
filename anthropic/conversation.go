package anthropic

import (
	"encoding/json"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// toParams converts a conversation into the SDK's message shape.
//
// A message whose parts all drop out is left out rather than sent empty. That
// happens for real: a turn recorded as nothing but its reasoning and its finish
// reason has no blocks to render, and the API refuses a message with empty
// content rather than ignoring it.
func toParams(conversation []nacelle.Message) []sdk.BetaMessageParam {
	params := make([]sdk.BetaMessageParam, 0, len(conversation))
	for _, message := range conversation {
		content := blocksOf(message.Parts)
		if len(content) == 0 {
			continue
		}
		role := sdk.BetaMessageParamRoleUser
		if message.Role == nacelle.RoleAssistant {
			role = sdk.BetaMessageParamRoleAssistant
		}
		params = append(params, sdk.BetaMessageParam{Role: role, Content: content})
	}
	return params
}

// blocksOf renders one message's parts as content blocks.
//
// Three of the five parts have no block to render into, and each is dropped for
// its own reason. Reasoning would need the signature it was issued with, which
// the stream never carries, so sending it back is a rejected request. Finish is
// this package's own bookkeeping and has no field in the wire format. And a
// tool call still streaming has a truncated JSON object for arguments, which is
// a rejected request rather than a partial one — the run it belongs to was
// abandoned, so nothing is waiting on it either.
func blocksOf(parts []nacelle.Part) []sdk.BetaContentBlockParamUnion {
	blocks := make([]sdk.BetaContentBlockParamUnion, 0, len(parts))
	for _, part := range parts {
		switch part := part.(type) {
		case nacelle.Text:
			if part.Text != "" {
				blocks = append(blocks, sdk.NewBetaTextBlock(part.Text))
			}
		case nacelle.ToolCall:
			if part.Finished {
				blocks = append(blocks, sdk.NewBetaToolUseBlock(part.ID, toolInput(part.Input), part.Name))
			}
		case nacelle.ToolResult:
			blocks = append(blocks, sdk.NewBetaToolResultBlock(part.ID, part.Result, part.Failed))
		}
	}
	return blocks
}

// toolInput is a tool call's input in the shape the SDK marshals.
//
// A call the model made with no arguments is an empty string here and has to
// reach the API as an empty object: input is a required field, and a tool that
// takes nothing is still a tool that was called.
func toolInput(input json.RawMessage) any {
	if len(input) == 0 {
		return json.RawMessage("{}")
	}
	return input
}
