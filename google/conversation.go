package google

import (
	"strings"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

func (b *Backend) messages(request nacelle.Request) []oai.ChatCompletionMessageParamUnion {
	messages := make([]oai.ChatCompletionMessageParamUnion, 0, len(request.Messages)+1)
	messages = append(messages, oai.SystemMessage(request.System))

	for _, message := range request.Messages {
		messages = append(messages, convert(message)...)
	}
	return messages
}

type sorted struct {
	text    []string
	calls   []oai.ChatCompletionMessageToolCallUnionParam
	results []oai.ChatCompletionMessageParamUnion
}

func sift(parts []nacelle.Part) sorted {
	var out sorted
	for _, part := range parts {
		switch part := part.(type) {
		case nacelle.Text:
			if part.Text != "" {
				out.text = append(out.text, part.Text)
			}
		case nacelle.ToolCall:
			if part.Finished {
				out.calls = append(out.calls, functionCall(part))
			}
		case nacelle.ToolResult:
			out.results = append(out.results, oai.ToolMessage(part.Result, part.ID))
		}
	}
	return out
}

func convert(message nacelle.Message) []oai.ChatCompletionMessageParamUnion {
	parts := sift(message.Parts)
	text := strings.Join(parts.text, "\n")

	if message.Role == nacelle.RoleAssistant {
		return append(assistantTurn(text, parts.calls), parts.results...)
	}
	if text != "" {
		return append(parts.results, oai.UserMessage(text))
	}
	return parts.results
}

func assistantTurn(text string, calls []oai.ChatCompletionMessageToolCallUnionParam) []oai.ChatCompletionMessageParamUnion {
	if text == "" && len(calls) == 0 {
		return nil
	}
	assistant := oai.ChatCompletionAssistantMessageParam{ToolCalls: calls}
	if text != "" {
		assistant.Content.OfString = oai.String(text)
	}
	return []oai.ChatCompletionMessageParamUnion{{OfAssistant: &assistant}}
}

func functionCall(call nacelle.ToolCall) oai.ChatCompletionMessageToolCallUnionParam {
	arguments := string(call.Input)
	if arguments == "" {
		arguments = "{}"
	}
	return oai.ChatCompletionMessageToolCallUnionParam{
		OfFunction: &oai.ChatCompletionMessageFunctionToolCallParam{
			ID: call.ID,
			Function: oai.ChatCompletionMessageFunctionToolCallFunctionParam{
				Name:      call.Name,
				Arguments: arguments,
			},
		},
	}
}
