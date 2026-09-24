package oairunner

import (
	"encoding/json"
	"fmt"
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

// promptChunk builds a response around one prompt, cached and written share.
func promptChunk(prompt, cached, written int64) string {
	return fmt.Sprintf(`{
		"choices": [],
		"usage": {
			"prompt_tokens": %d,
			"completion_tokens": 2,
			"total_tokens": %d,
			"prompt_tokens_details": {"cached_tokens": %d, "cache_write_tokens": %d}
		}
	}`, prompt, prompt+2, cached, written)
}

// The schema reports prompt_tokens as the whole prompt with cached_tokens and
// cache_write_tokens as parts of it, and its own total_tokens is prompt plus
// completion. So Total matching total_tokens, on every shape of prompt, is the
// whole contract: it is what the provider's arithmetic says and what this
// mapping has to reproduce. Reading a part of prompt_tokens as a neighbour of it
// counted it twice — this test read 296 against a total_tokens of 196.
func TestUsageTotalMatchesTheProvidersOwnTotal(t *testing.T) {
	for name, tc := range map[string]struct {
		prompt, cached, written int64
	}{
		"uncached":               {10, 0, 0},
		"fully cached":           {10339, 10318, 0},
		"cache write, no read":   {34375, 0, 32768},
		"cache read, no write":   {34380, 32768, 0},
		"read and write at once": {5000, 2000, 1500},
	} {
		chunk := usageChunk(t, promptChunk(tc.prompt, tc.cached, tc.written))
		usage, ok := usageOf(chunk)
		if !ok {
			t.Fatalf("%s: usageOf reported no usage for a chunk that carries one", name)
		}
		if usage.Total() != chunk.Usage.TotalTokens {
			t.Errorf("%s: total = %d, want the provider's own %d",
				name, usage.Total(), chunk.Usage.TotalTokens)
		}
		if usage.InputTokens+usage.CacheReadTokens+usage.CacheCreationTokens != tc.prompt {
			t.Errorf("%s: parts sum to %d, want the whole prompt %d",
				name, usage.InputTokens+usage.CacheReadTokens+usage.CacheCreationTokens, tc.prompt)
		}
	}
}

// A cache write is input paid at the write rate, so it belongs in
// CacheCreationTokens where Anthropic reports the same thing, not in the
// full-price input. Left in InputTokens it is counted as much as the writes
// that follow it are counted at all.
func TestCacheWritesLandInCreationNotInFullPriceInput(t *testing.T) {
	usage, _ := usageOf(usageChunk(t, promptChunk(34375, 0, 32768)))

	if usage.CacheCreationTokens != 32768 {
		t.Errorf("creation = %d, want the whole write", usage.CacheCreationTokens)
	}
	if usage.InputTokens != 34375-32768 {
		t.Errorf("input = %d, want the remainder billed at full price", usage.InputTokens)
	}
	if usage.CacheReadTokens != 0 {
		t.Errorf("read = %d, want nothing read on the turn that wrote", usage.CacheReadTokens)
	}
}

// CacheHitRate divides by the prompt a hit could have been served from, which
// is the prompt less the part that was only ever written. Counting the write in
// the denominator would report a miss on a turn that had nothing to read.
func TestCacheHitRateIgnoresTheShareThatWasOnlyWritten(t *testing.T) {
	write, _ := usageOf(usageChunk(t, promptChunk(34375, 0, 32768)))
	if rate := write.CacheHitRate(); rate != 0 {
		t.Errorf("hit rate = %v on a write turn, want 0", rate)
	}

	read, _ := usageOf(usageChunk(t, promptChunk(34380, 32768, 0)))
	if rate := read.CacheHitRate(); rate < 0.95 || rate > 0.96 {
		t.Errorf("hit rate = %v, want the cached share of the reusable prompt", rate)
	}
}

// The floor keeps a rewritten or inconsistent response from poisoning a session
// total. OpenAI's own forum carries an unanswered report of exactly this shape:
// cached 3945 and written 4580 against a prompt of 4583, whose parts sum to more
// than the whole. An input below zero is worse than one too high, because a
// caller adds it to a running figure.
func TestImpossibleCacheCountsFloorAtZero(t *testing.T) {
	usage, _ := usageOf(usageChunk(t, `{
		"choices": [],
		"usage": {"prompt_tokens": 4583, "completion_tokens": 15, "total_tokens": 4598,
			"prompt_tokens_details": {"cached_tokens": 3945, "cache_write_tokens": 4580}}
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
