package main

import (
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func bigConversation() []nacelle.Message {
	result := func(id string, n int) nacelle.ToolResult {
		return nacelle.ToolResult{ID: id, Name: "read", Result: strings.Repeat("x", n)}
	}
	return []nacelle.Message{
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{result("old-1", 40_000), result("small", 100)}},
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{nacelle.Text{Text: "earlier answer"}}},
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{result("old-2", 30_000)}},
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{nacelle.Text{Text: "middle answer"}}},
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{result("recent", 50_000)}},
		{Role: nacelle.RoleAssistant},
	}
}

func TestCompactDropsOldLargeResults(t *testing.T) {
	m := sized()
	m.conversation = bigConversation()
	m.size = compactAt + 25_000

	m.compact()

	dropped, kept := 0, 0
	for _, message := range m.conversation {
		for _, part := range message.Parts {
			result, ok := part.(nacelle.ToolResult)
			if !ok {
				continue
			}
			if strings.HasPrefix(result.Result, droppedNotice) {
				dropped++
				continue
			}
			if len(result.Result) > compactMinResult {
				kept++
			}
		}
	}
	if dropped != 1 || kept != 2 {
		t.Errorf("dropped %d and kept %d large results, want the one outside the keep window dropped and both inside it kept", dropped, kept)
	}
	if m.trimmed != 1 {
		t.Errorf("trimmed count = %d, want 1", m.trimmed)
	}
}

func TestCompactKeepsThePairingShape(t *testing.T) {
	m := sized()
	before := bigConversation()
	m.conversation = before
	m.size = compactAt + 1

	m.compact()

	for i, message := range m.conversation {
		if len(message.Parts) != len(before[i].Parts) {
			t.Fatalf("message %d changed shape: %d parts, was %d", i, len(message.Parts), len(before[i].Parts))
		}
		for j, part := range message.Parts {
			was, ok := before[i].Parts[j].(nacelle.ToolResult)
			now, still := part.(nacelle.ToolResult)
			if ok != still {
				t.Fatalf("message %d part %d changed kind", i, j)
			}
			if ok && now.ID != was.ID {
				t.Errorf("message %d part %d: id %q, was %q — the pairing broke", i, j, now.ID, was.ID)
			}
		}
	}
}

func TestCompactLeavesAShortConversationAlone(t *testing.T) {
	m := sized()
	m.conversation = bigConversation()
	m.size = compactAt + 1
	m.conversation = m.conversation[len(m.conversation)-compactKeepMessages:]

	m.compact()

	if m.trimmed != 0 {
		t.Errorf("a conversation inside the keep window lost %d results", m.trimmed)
	}
}

func TestCompactDoesNothingUnderTheThreshold(t *testing.T) {
	m := sized()
	m.conversation = bigConversation()
	m.size = compactAt

	m.compact()

	if m.trimmed != 0 {
		t.Errorf("at the threshold exactly, trimmed %d anyway", m.trimmed)
	}
}

func TestCompactIsNotPaidTwiceOnASecondPass(t *testing.T) {
	m := sized()
	m.conversation = bigConversation()
	m.size = compactAt + 1

	m.compact()
	first := m.trimmed
	m.size = compactAt + compactSlack + 1

	m.compact()

	if m.trimmed != first {
		t.Errorf("second pass trimmed %d more; placeholders were counted as savings again", m.trimmed-first)
	}
}

func TestSizedCountsEveryBilledInputKind(t *testing.T) {
	m := sized()
	m.sized(nacelle.Usage{InputTokens: 1000, CacheReadTokens: 9000, CacheCreationTokens: 500})
	if m.size != 10_500 {
		t.Errorf("size = %d, want cache reads and creations billed as input", m.size)
	}
}
