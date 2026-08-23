package main

import (
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func TestStatusSeparatesInputFromOutputTokens(t *testing.T) {
	m := sized()
	m.spent = nacelle.Usage{
		InputTokens:     2600,
		OutputTokens:    1100,
		CacheReadTokens: 9800,
	}

	got := visible(m.View().Content)
	for _, want := range []string{"in 2.6k", "out 1.1k", "9.8k cached"} {
		if !strings.Contains(got, want) {
			t.Errorf("status %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "3700 tokens") || strings.Contains(got, "13.5k tokens") {
		t.Errorf("status still shows one merged total: %q", got)
	}
}

func TestShortTokens(t *testing.T) {
	cases := map[int64]string{
		0:         "0",
		999:       "999",
		1000:      "1.0k",
		12345:     "12.3k",
		99999:     "100.0k",
		100000:    "100k",
		123456789: "123456k",
	}
	for in, want := range cases {
		if got := shortTokens(in); got != want {
			t.Errorf("shortTokens(%d) = %q, want %q", in, got, want)
		}
	}
}
