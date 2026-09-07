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

func usageOf(chunk oai.ChatCompletionChunk) (nacelle.Usage, bool) {
	if chunk.Usage.TotalTokens == 0 && chunk.Usage.PromptTokens == 0 {
		return nacelle.Usage{}, false
	}
	u := nacelle.Usage{
		InputTokens:     chunk.Usage.PromptTokens,
		OutputTokens:    chunk.Usage.CompletionTokens,
		CacheReadTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
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
