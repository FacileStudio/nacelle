// Package oairunner runs agents on OpenAI-compatible APIs.
package oairunner

import (
	"encoding/json"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
)

func deltaEvents(delta oai.ChatCompletionChunkChoiceDelta, thinking bool) []nacelle.Event {
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

// usageOf maps this schema's usage onto nacelle's.
//
// prompt_tokens is the whole prompt, and the two fields of details are parts of
// it, not neighbours of it: the schema documents cached_tokens as "cached
// tokens present in the prompt" and cache_write_tokens as "the number of prompt
// tokens written to cache", and the response's own total_tokens is prompt plus
// completion, grown by neither. Carrying prompt_tokens through alongside a part
// of itself counted that part twice, which overstated Usage.Total, skewed
// CacheHitRate, and inflated every caller's idea of how full the context was —
// a client sizing compaction on it compacts a conversation that still fits.
// Both parts are subtracted, and CacheWriteTokens becomes the cache-creation
// count so the four backends report the same disjoint breakdown Anthropic does.
//
// The subtraction is floored at zero because a response can report more cached
// and written tokens than prompt tokens, which OpenAI's own forum has an
// unanswered report of (cached 3945 and written 4580 against a prompt of 4583).
// A negative input is worse than a wrong one: it would subtract from a caller's
// totals, where an over-count only overstates a figure already inconsistent at
// the source.
func usageOf(chunk oai.ChatCompletionChunk) (nacelle.Usage, bool) {
	if chunk.Usage.TotalTokens == 0 && chunk.Usage.PromptTokens == 0 {
		return nacelle.Usage{}, false
	}
	cached := chunk.Usage.PromptTokensDetails.CachedTokens
	written := chunk.Usage.PromptTokensDetails.CacheWriteTokens
	u := nacelle.Usage{
		InputTokens:         max(chunk.Usage.PromptTokens-cached-written, 0),
		OutputTokens:        chunk.Usage.CompletionTokens,
		CacheReadTokens:     cached,
		CacheCreationTokens: written,
	}
	if raw, ok := extra(chunk.Usage.JSON.ExtraFields, "cost"); ok {
		var cost float64
		if err := json.Unmarshal(raw, &cost); err == nil {
			u.Cost = cost
		}
	}
	return u, true
}

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
	default:
		return nacelle.StopOther
	}
}
