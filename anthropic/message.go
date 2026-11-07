package anthropic

import (
	"encoding/json"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// provider is the Native.Provider this package writes and reads back.
const provider = "anthropic"

// messageOf turns the accumulated turn into the transcript's message.
//
// Text and the local calls the loop will run become the parts every backend
// reads. A call the tracker discarded is left out, because it never runs and
// a tool_use block with no result is a rejected request. Every other block —
// a signed thinking block, a remote MCP exchange, a compaction block — is kept
// as a Native part in its own wire form, so the next turn sends it back as it
// came. Reasoning is recorded beside a thinking block when it was shown.
func messageOf(assistant *sdk.BetaMessage, calls *callTracker) nacelle.Message {
	message := nacelle.Message{Role: nacelle.RoleAssistant}
	for _, block := range assistant.Content {
		message.Parts = append(message.Parts, partsOf(block, calls)...)
	}
	return message
}

// partsOf is what one content block contributes to the message.
func partsOf(block sdk.BetaContentBlockUnion, calls *callTracker) []nacelle.Part {
	switch block.Type {
	case "text":
		return []nacelle.Part{nacelle.Text{Text: block.Text}}
	case "tool_use":
		call, kept := calls.local(block.ID)
		if !kept {
			return nil
		}
		return []nacelle.Part{nacelle.ToolCall{ID: call.ID, Name: call.Name, Input: json.RawMessage(call.Input), Finished: true}}
	case "thinking":
		if calls.thinking && block.Thinking != "" {
			return append([]nacelle.Part{nacelle.Reasoning{Text: block.Thinking}}, native(block)...)
		}
	}
	return native(block)
}

// native keeps a block in the wire form the API takes it back in.
func native(block sdk.BetaContentBlockUnion) []nacelle.Part {
	data, err := json.Marshal(block.ToParam())
	if err != nil {
		return nil
	}
	return []nacelle.Part{nacelle.Native{Provider: provider, Data: data}}
}

// nativeBlock reads back a block this package wrote, or reports false for one
// it did not.
func nativeBlock(part nacelle.Native) (sdk.BetaContentBlockParamUnion, bool) {
	var block sdk.BetaContentBlockParamUnion
	if part.Provider != provider || json.Unmarshal(part.Data, &block) != nil {
		return block, false
	}
	return block, true
}
