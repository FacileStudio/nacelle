package oairunner

import (
	"encoding/json"
	"testing"

	oai "github.com/openai/openai-go/v3"
)

// usageChunk unmarshals a final chunk the way the streaming client does, so the
// details object arrives with the raw JSON the mapping reads.
func usageChunk(t *testing.T, raw string) oai.ChatCompletionChunk {
	t.Helper()
	var chunk oai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		t.Fatalf("unmarshalling chunk: %v", err)
	}
	return chunk
}

// The schema is explicit that cached_tokens is a subset of prompt_tokens —
// total_tokens in this very response is prompt plus completion, and it does not
// grow by the cached share. Mapping both through as reported billed the cached
// tokens twice, so Usage.Total disagreed with the provider's own figure on
// every cached prompt: a 194-token prompt with 100 cached read as 296 tokens
// against the 196 the response says.
func TestCachedTokensAreCountedOnceAgainstTheProvidersOwnTotal(t *testing.T) {
	chunk := usageChunk(t, `{
		"choices": [],
		"usage": {
			"prompt_tokens": 194,
			"completion_tokens": 2,
			"total_tokens": 196,
			"prompt_tokens_details": {"cached_tokens": 100}
		}
	}`)

	usage, ok := usageOf(chunk)
	if !ok {
		t.Fatal("usageOf reported no usage for a chunk that carries one")
	}
	if usage.InputTokens != 94 || usage.CacheReadTokens != 100 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v, want 94 uncached in, 100 cached, 2 out", usage)
	}
	if usage.Total() != chunk.Usage.TotalTokens {
		t.Errorf("total = %d, want the provider's own %d", usage.Total(), chunk.Usage.TotalTokens)
	}
	if rate := usage.CacheHitRate(); rate < 0.51 || rate > 0.52 {
		t.Errorf("cache hit rate = %v, want the cached share of the whole prompt", rate)
	}
}

// A prompt that was not served from cache is untouched by the subtraction, and
// the response that carries no details object at all is the ordinary first
// turn.
func TestAnUncachedPromptKeepsItsWholeCount(t *testing.T) {
	for name, raw := range map[string]string{
		"no details": `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		"zero cached": `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,
			"prompt_tokens_details":{"cached_tokens":0}}}`,
	} {
		usage, ok := usageOf(usageChunk(t, raw))
		if !ok {
			t.Fatalf("%s: usageOf reported no usage", name)
		}
		if usage.InputTokens != 10 || usage.CacheReadTokens != 0 {
			t.Errorf("%s: usage = %+v, want the prompt whole and nothing cached", name, usage)
		}
	}
}

// A negative input would not merely be wrong, it would subtract from whatever a
// caller accumulated. The floor keeps a rewritten or malformed response from
// poisoning a session total.
func TestAnImpossibleCacheCountFloorsAtZero(t *testing.T) {
	usage, _ := usageOf(usageChunk(t, `{
		"choices": [],
		"usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12,
			"prompt_tokens_details": {"cached_tokens": 50}}
	}`))

	if usage.InputTokens != 0 {
		t.Errorf("input = %d, want zero rather than a negative prompt", usage.InputTokens)
	}
}

// A chunk that reports nothing is not usage: the loop uses this to decide
// whether it has a figure at all, and a zero usage would erase a real one.
func TestAnEmptyChunkReportsNoUsage(t *testing.T) {
	if _, ok := usageOf(usageChunk(t, `{"choices":[]}`)); ok {
		t.Error("usageOf reported usage for a chunk that carries none")
	}
}
