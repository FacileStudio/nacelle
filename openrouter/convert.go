package openrouter

import (
	"encoding/json"
	"slices"
	"strings"

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

// toolParams renders nacelle tools as OpenAI function definitions, sorted by
// name.
//
// The order is not the caller's because prompt caching keys on an exact
// prefix, and the tool schema sits at the front of every request. A caller
// that builds its tool slice from a map hands over a different order each run
// and pays full price for a prompt that never changed. Sorting costs nothing,
// the model is indifferent to the order, and it is what the Anthropic backend
// does — two backends that disagree here would cache differently for reasons
// nobody could see. The caller's slice is cloned rather than sorted in place:
// it belongs to them.
func toolParams(tools []nacelle.Tool) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	ordered := slices.Clone(tools)
	slices.SortStableFunc(ordered, func(a, b nacelle.Tool) int {
		return strings.Compare(a.Name(), b.Name())
	})

	params := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range ordered {
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
//
// A cost that is absent, or that arrives in a shape this cannot decode, is
// left at zero, which is indistinguishable from a generation that was free.
// Nothing better is available: Usage has no third state, so the caveat is
// documented on Capabilities instead of invented here. Zero means unreported.
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
// The OpenAI schema documents five values — stop, length, tool_calls,
// content_filter and the deprecated function_call — and OpenRouter adds a
// sixth of its own, "error", for a generation that failed part-way. The SDK
// types the field as a bare string rather than an enum, so a provider behind
// the gateway is free to invent a seventh. Everything unrecognised, the empty
// string included, becomes StopOther: a reason this package cannot name is
// unfinished work, and claiming StopEnd for it would tell a consumer the
// answer is whole on exactly the runs where it is not.
//
// "error" is listed rather than left to that default, and it stays a stop
// reason rather than becoming a stream error. Two reasons. It usually arrives
// alongside a top-level error object, which is already parsed, already
// classified as retryable or not, and already fails the run — inventing a
// second error from the finish reason would race that one and carry less. And
// when it arrives alone there is nothing to raise: no code, no message,
// nothing a retry could act on, while the tokens spent getting there were
// still billed and a failed sequence never reaches the KindDone that reports
// them. StopOther is false for Complete, so a consumer that checks is told the
// answer is not whole either way.
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
	case "error":
		return nacelle.StopOther
	}
	return nacelle.StopOther
}
