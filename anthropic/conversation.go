package anthropic

import (
	"encoding/base64"
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
// Reasoning has no block of its own: the signed thinking block it came with
// travels as a Native part, and only that is sent back. Finish is this
// package's own bookkeeping and has no field in the wire format. A tool call
// still streaming has a truncated JSON object for arguments, which is a
// rejected request rather than a partial one, so it is left out too.
func blocksOf(parts []nacelle.Part) []sdk.BetaContentBlockParamUnion {
	blocks := make([]sdk.BetaContentBlockParamUnion, 0, len(parts))
	for _, part := range parts {
		if block, ok := blockOf(part); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// blockOf renders one part, or reports false for a part with no block.
func blockOf(part nacelle.Part) (sdk.BetaContentBlockParamUnion, bool) {
	switch part := part.(type) {
	case nacelle.Text:
		return sdk.NewBetaTextBlock(part.Text), part.Text != ""
	case nacelle.ToolCall:
		return sdk.NewBetaToolUseBlock(part.ID, toolInput(part.Input), part.Name), part.Finished
	case nacelle.ToolResult:
		return toolResultBlock(part), true
	case nacelle.Image:
		return imageBlock(part), true
	case nacelle.Document:
		return documentBlock(part), true
	case nacelle.Native:
		return nativeBlock(part)
	}
	return sdk.BetaContentBlockParamUnion{}, false
}

// imageBlock is an Image as a base64 image block.
func imageBlock(image nacelle.Image) sdk.BetaContentBlockParamUnion {
	return sdk.NewBetaImageBlock(sdk.BetaBase64ImageSourceParam{
		Data:      base64.StdEncoding.EncodeToString(image.Data),
		MediaType: sdk.BetaBase64ImageSourceMediaType(image.MediaType),
	})
}

// toolResultBlock is a ToolResult as a tool_result block: its text first,
// then each picture as an image block inside the same result, which is the
// one slot the Messages API gives a tool's images.
func toolResultBlock(result nacelle.ToolResult) sdk.BetaContentBlockParamUnion {
	block := sdk.NewBetaToolResultBlock(result.ID, result.Result, result.Failed)
	for _, image := range result.Images {
		block.OfToolResult.Content = append(block.OfToolResult.Content, sdk.BetaToolResultBlockParamContentUnion{
			OfImage: imageBlock(image).OfImage,
		})
	}
	return block
}

// documentBlock is a Document as a base64 PDF document block, titled with the
// file name when there is one.
func documentBlock(document nacelle.Document) sdk.BetaContentBlockParamUnion {
	block := sdk.NewBetaDocumentBlock(sdk.BetaBase64PDFSourceParam{
		Data: base64.StdEncoding.EncodeToString(document.Data),
	})
	if document.Name != "" {
		block.OfDocument.Title = sdk.String(document.Name)
	}
	return block
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
