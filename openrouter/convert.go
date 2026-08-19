package openrouter

import (
	"encoding/json"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
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
		if message.Assistant {
			messages = append(messages, openai.AssistantMessage(message.Text))
			continue
		}
		messages = append(messages, openai.UserMessage(message.Text))
	}
	return messages
}

// toolParams renders nacelle tools as OpenAI function definitions.
func toolParams(tools []nacelle.Tool) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	params := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		params = append(params, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        tool.Name(),
			Description: openai.String(tool.Description()),
			Parameters:  openai.FunctionParameters(tool.Schema()),
		}))
	}
	return params
}

// deltaEvents maps one streamed delta onto nacelle events.
//
// Reasoning is only forwarded when the caller asked for it. The request
// already told OpenRouter to exclude it, so this is a second gate on a stream
// that should be empty anyway — cheap, and it keeps a provider that ignores
// `exclude` from surprising a consumer that never opted in.
func deltaEvents(delta openai.ChatCompletionChunkChoiceDelta, thinking bool) []nacelle.Event {
	var events []nacelle.Event

	if thinking {
		if raw, ok := extra(delta.JSON.ExtraFields, "reasoning"); ok {
			var text string
			if err := json.Unmarshal(raw, &text); err == nil && text != "" {
				events = append(events, nacelle.Event{Kind: nacelle.KindThinking, Text: text})
			}
		}
	}
	if delta.Content != "" {
		events = append(events, nacelle.Event{Kind: nacelle.KindText, Text: delta.Content})
	}
	return events
}

// usageOf reads the accounting off a chunk, reporting whether there was any.
//
// OpenRouter sends usage exactly once, in the last chunk before the stream
// ends, and that chunk carries an empty choices array. The cost is not part of
// the OpenAI schema, so it arrives as an extra field and is read by name.
func usageOf(chunk openai.ChatCompletionChunk) (nacelle.Usage, bool) {
	if chunk.Usage.TotalTokens == 0 && chunk.Usage.PromptTokens == 0 {
		return nacelle.Usage{}, false
	}

	usage := nacelle.Usage{
		InputTokens:     chunk.Usage.PromptTokens,
		OutputTokens:    chunk.Usage.CompletionTokens,
		CacheReadTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
	}
	if raw, ok := extra(chunk.Usage.JSON.ExtraFields, "cost"); ok {
		if err := json.Unmarshal(raw, &usage.Cost); err != nil {
			usage.Cost = 0
		}
	}
	return usage, true
}

// extra reads a field the OpenAI schema does not define.
//
// Everything OpenRouter adds — cost, reasoning, reasoning_details,
// native_finish_reason — arrives this way. The SDK keeps the raw JSON rather
// than discarding what it cannot type, which is the only reason a
// gateway-specific field is reachable at all.
func extra(fields map[string]respjson.Field, name string) (json.RawMessage, bool) {
	field, present := fields[name]
	if !present {
		return nil, false
	}
	raw := field.Raw()
	if raw == "" || raw == "null" {
		return nil, false
	}
	return json.RawMessage(raw), true
}

// stopOf maps the OpenAI-schema finish_reason onto a reason nacelle names.
//
// The schema documents exactly five values — stop, length, tool_calls,
// content_filter and the deprecated function_call — and the SDK types the
// field as a bare string rather than an enum, so a provider behind OpenRouter
// is free to invent a sixth. Everything unrecognised, the empty string
// included, becomes StopOther: a reason this package cannot name is unfinished
// work, and claiming StopEnd for it would tell a consumer the answer is whole
// on exactly the runs where it is not.
//
// function_call folds into StopTools because it is the older spelling of the
// same event, and StopContext has no source here at all: OpenRouter reports a
// conversation that outgrew the window as an error, not as a finish_reason.
func stopOf(reason string) nacelle.Stop {
	switch reason {
	case "stop":
		return nacelle.StopEnd
	case "length":
		return nacelle.StopMaxTokens
	case "tool_calls", "function_call":
		return nacelle.StopTools
	case "content_filter":
		return nacelle.StopRefusal
	}
	return nacelle.StopOther
}
