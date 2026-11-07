package nacelle

// Usage is what a turn or a run cost.
//
// It is reported on every turn rather than only at the end because comparing
// runs on cost is one of the reasons this package exists, and a total that has
// to be reconstructed afterwards is a total nobody trusts.
type Usage struct {
	// InputTokens is the input billed at full price: every prompt token that
	// was neither read from nor written to the cache.
	//
	// The two APIs count differently and the difference lands here. Anthropic
	// reports input_tokens already net of its cache fields. The OpenAI schema
	// reports prompt_tokens as the whole prompt, with
	// prompt_tokens_details.cached_tokens and cache_write_tokens as parts of
	// it — a prompt of 194 with 100 cached bills 194 prompt tokens, not 294 —
	// so a backend on that schema subtracts both parts on the way in, and
	// reports the writes as CacheCreationTokens. An InputTokens that meant
	// "the whole prompt" on one backend and "the uncached rest" on another
	// would make Total and CacheHitRate wrong on whichever one disagreed, by
	// most of an agentic session's input.
	InputTokens int64

	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64

	// Cost is what the run was charged, in US dollars.
	//
	// Only backends whose Capabilities report Cost fill it; the rest leave
	// it zero and the caller prices the tokens itself. It is here rather
	// than left to every consumer because a gateway that already knows the
	// number is more trustworthy than a price table copied into an app and
	// then not updated.
	//
	// It is a float64, which means adding two costs does not always give
	// the decimal you expect — 0.0001 plus 0.0002 is 0.00030000000000000003.
	// That is deliberate: the wire format is a JSON number, the error is
	// around one part in 1e16, and the question this field answers is which
	// of two runs cost more. Do not use it as a ledger; compare with a
	// tolerance, and bill from the provider's own records.
	Cost float64
}

// Add returns the sum of two usages.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:         u.InputTokens + other.InputTokens,
		OutputTokens:        u.OutputTokens + other.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens + other.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens + other.CacheCreationTokens,
		Cost:                u.Cost + other.Cost,
	}
}

// Total is every token the run was billed for.
func (u Usage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

// CacheHitRate is the share of input this run read from cache rather than
// paid full price for, from 0 to 1.
//
// It excludes CacheCreationTokens from the denominator on purpose: a write is
// not a hit, and counting it as attempted-but-missed would understate the
// rate on a run's first turn, when every prefix is written and none can have
// been read yet. Zero on a run with no cacheable input at all — that is not
// a miss, there was nothing to hit.
func (u Usage) CacheHitRate() float64 {
	eligible := u.InputTokens + u.CacheReadTokens
	if eligible == 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(eligible)
}
