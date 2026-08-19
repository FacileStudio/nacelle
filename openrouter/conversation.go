package openrouter

import (
	"strings"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3"
)

// messages builds the conversation, with the system prompt at the front.
//
// The OpenAI schema has no separate system field: the prompt is the first
// message, which is why it is prepended here on every run rather than carried
// in the conversation the caller owns.
func (b *Backend) messages(request nacelle.Request) []openai.ChatCompletionMessageParamUnion {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(request.Messages)+1)
	messages = append(messages, openai.SystemMessage(request.System))

	for _, message := range request.Messages {
		messages = append(messages, convert(message)...)
	}
	return messages
}

// sorted is one message's parts, put into the three shapes the OpenAI schema
// has a place for.
type sorted struct {
	text    []string
	calls   []openai.ChatCompletionMessageToolCallUnionParam
	results []openai.ChatCompletionMessageParamUnion
}

// sift sorts one message's parts, dropping the two the schema cannot carry.
//
// Reasoning goes because OpenRouter is asked to exclude it unless the caller
// opted in, and replaying it would bill a chain of thought twice. Finish goes
// because there is no field for it. A tool call still streaming goes too: its
// arguments are a truncated JSON object, which is a rejected request rather
// than a partial one.
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
			out.results = append(out.results, openai.ToolMessage(part.Result, part.ID))
		}
	}
	return out
}

// convert renders one message as the schema's messages, which is not always
// one message.
//
// A tool result is a message of its own here, where Anthropic carries it as a
// block inside the user turn, so this is where the two shapes are reconciled.
// Results go out ahead of any prose in the same turn because the schema wants a
// tool message to follow the assistant message that asked for it, and text
// wedged between the two breaks the pairing the ids exist to keep.
func convert(message nacelle.Message) []openai.ChatCompletionMessageParamUnion {
	parts := sift(message.Parts)
	text := strings.Join(parts.text, "\n")

	if message.Role == nacelle.RoleAssistant {
		return append(assistantTurn(text, parts.calls), parts.results...)
	}
	if text != "" {
		return append(parts.results, openai.UserMessage(text))
	}
	return parts.results
}

// assistantTurn is the model's turn, or nothing at all.
//
// The schema requires an assistant message to carry content or tool calls, so a
// turn recorded as nothing but its reasoning and its finish reason has no
// message to become, and is left out rather than sent empty.
func assistantTurn(text string, calls []openai.ChatCompletionMessageToolCallUnionParam) []openai.ChatCompletionMessageParamUnion {
	if text == "" && len(calls) == 0 {
		return nil
	}
	assistant := openai.ChatCompletionAssistantMessageParam{ToolCalls: calls}
	if text != "" {
		assistant.Content.OfString = openai.String(text)
	}
	return []openai.ChatCompletionMessageParamUnion{{OfAssistant: &assistant}}
}

// functionCall renders one tool call the way the schema asks for it.
//
// The arguments are a string of JSON rather than JSON, which is the schema's
// own choice, and the field is required: a call the model made with no
// arguments goes out as an empty object, because a provider handed an empty
// string has nothing to parse.
func functionCall(call nacelle.ToolCall) openai.ChatCompletionMessageToolCallUnionParam {
	arguments := string(call.Input)
	if arguments == "" {
		arguments = "{}"
	}
	return openai.ChatCompletionMessageToolCallUnionParam{
		OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
			ID: call.ID,
			Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
				Name:      call.Name,
				Arguments: arguments,
			},
		},
	}
}
