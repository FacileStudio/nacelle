// Package oairunner runs agents on OpenAI-compatible APIs.
package oairunner

import (
	"strings"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

func (b *Backend) messages(request nacelle.Request) ([]oai.ChatCompletionMessageParamUnion, error) {
	messages := make([]oai.ChatCompletionMessageParamUnion, 0, len(request.Messages)+1)
	messages = append(messages, oai.SystemMessage(request.System))

	for _, message := range request.Messages {
		converted, err := b.convert(message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted...)
	}
	return messages, nil
}

type sorted struct {
	text    []string
	content []oai.ChatCompletionContentPartUnionParam
	media   bool
	calls   []oai.ChatCompletionMessageToolCallUnionParam
	results []oai.ChatCompletionMessageParamUnion
	extra   map[string]any
}

func (b *Backend) sift(parts []nacelle.Part) (sorted, error) {
	var out sorted
	for _, part := range parts {
		switch part := part.(type) {
		case nacelle.Text:
			out.addText(part.Text)
		case nacelle.ToolCall:
			out.addCall(part)
		case nacelle.ToolResult:
			out.addResult(part)
		case nacelle.Image:
			out.addMedia(imagePart(part))
		case nacelle.Document:
			if !b.Options.Documents {
				return out, &nacelle.Unsupported{Backend: b.Options.Name, Feature: "Document parts"}
			}
			out.addMedia(filePart(part))
		case nacelle.Native:
			out.addNative(b.Options.Name, part)
		}
	}
	return out, nil
}

func (s *sorted) addText(text string) {
	if text != "" {
		s.text = append(s.text, text)
		s.content = append(s.content, oai.TextContentPart(text))
	}
}

func (s *sorted) addCall(call nacelle.ToolCall) {
	if call.Finished {
		s.calls = append(s.calls, functionCall(call))
	}
}

func (b *Backend) convert(message nacelle.Message) ([]oai.ChatCompletionMessageParamUnion, error) {
	parts, err := b.sift(message.Parts)
	if err != nil {
		return nil, err
	}
	text := strings.Join(parts.text, "\n")

	if message.Role == nacelle.RoleAssistant {
		return append(assistantTurn(text, parts.calls, parts.extra), parts.results...), nil
	}
	if parts.media {
		return append(parts.results, oai.UserMessage(parts.content)), nil
	}
	if text != "" {
		return append(parts.results, oai.UserMessage(text)), nil
	}
	return parts.results, nil
}

func assistantTurn(text string, calls []oai.ChatCompletionMessageToolCallUnionParam, extra map[string]any) []oai.ChatCompletionMessageParamUnion {
	if text == "" && len(calls) == 0 {
		return nil
	}
	assistant := oai.ChatCompletionAssistantMessageParam{ToolCalls: calls}
	if text != "" {
		assistant.Content.OfString = oai.String(text)
	}
	if len(extra) > 0 {
		assistant.SetExtraFields(extra)
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
