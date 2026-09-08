package openrouter

import (
	"encoding/json"
	"slices"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
)

var payloadKeys = []string{"text", "summary", "data"}

type details struct {
	blocks []map[string]any
}

func (d *details) add(raw json.RawMessage) {
	var incoming []map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return
	}
	for _, fragment := range incoming {
		if open := d.open(); open != nil && continues(open, fragment) {
			merge(open, fragment)
			continue
		}
		d.blocks = append(d.blocks, fragment)
	}
}

func (d *details) open() map[string]any {
	if len(d.blocks) == 0 {
		return nil
	}
	return d.blocks[len(d.blocks)-1]
}

func continues(open, fragment map[string]any) bool {
	p1, _ := open["index"].(float64)
	p2, _ := fragment["index"].(float64)
	if p1 != p2 {
		return false
	}
	first, _ := open["type"].(string)
	second, _ := fragment["type"].(string)
	return first == "" || second == "" || first == second
}

func merge(open, fragment map[string]any) {
	for key, value := range fragment {
		if merged, changed := mergedValue(open[key], value, slices.Contains(payloadKeys, key)); changed {
			open[key] = merged
		}
	}
}

func mergedValue(existing, incoming any, grows bool) (any, bool) {
	if existing == nil {
		return incoming, true
	}
	was, wasText := existing.(string)
	addition, isText := incoming.(string)
	if !wasText || !isText {
		return nil, false
	}
	if grows {
		return was + addition, true
	}
	return addition, was == ""
}

func (d *details) observe(chunk openai.ChatCompletionChunk) {
	if len(chunk.Choices) == 0 {
		return
	}
	if raw, ok := extraField(chunk.Choices[0].Delta.JSON.ExtraFields, "reasoning_details"); ok {
		d.add(raw)
	}
}

func (d *details) attach(assistant openai.ChatCompletionMessageParamUnion) openai.ChatCompletionMessageParamUnion {
	if len(d.blocks) > 0 && assistant.OfAssistant != nil {
		assistant.OfAssistant.SetExtraFields(map[string]any{"reasoning_details": d.blocks})
	}
	return assistant
}

func extraField(fields map[string]respjson.Field, name string) (json.RawMessage, bool) {
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
