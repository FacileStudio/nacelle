package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// said is the prose of a message, which is what most of these tests are about.
func said(message nacelle.Message) string {
	var text []string
	for _, part := range message.Parts {
		if prose, ok := part.(nacelle.Text); ok {
			text = append(text, prose.Text)
		}
	}
	return strings.Join(text, "")
}

// shape names every part of a message in order, so a test can assert on the
// structure without unpacking five types by hand.
func shape(message nacelle.Message) []string {
	names := make([]string, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch part.(type) {
		case nacelle.Text:
			names = append(names, "text")
		case nacelle.Reasoning:
			names = append(names, "reasoning")
		case nacelle.ToolCall:
			names = append(names, "call")
		case nacelle.ToolResult:
			names = append(names, "result")
		case nacelle.Finish:
			names = append(names, "finish")
		}
	}
	return names
}

// using replays the run a tool loop produces: the model says something, asks
// for a tool, the tool answers, and the model carries on.
func using(t *testing.T) *model {
	t.Helper()

	m := sized()
	m.agent = answering(t)
	m.prompt.SetValue("what is in go.mod?")
	m.ask()
	t.Cleanup(m.run.cancel)

	m.record(nacelle.Event{Kind: nacelle.KindText, Text: "let me look"})
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "let me look"})
	m.record(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{
		ID: "call_1", Name: "read_file", Input: `{"path":"go.mod"}`,
	}})
	return m
}

// This is the whole of A6 seen from the consumer: a run that used a tool has to
// come back as a conversation the model could have produced, or the next
// question resumes from a transcript with the tool history cut out of it.
func TestAToolRoundIsRebuiltAsTheModelProducedIt(t *testing.T) {
	m := using(t)
	m.record(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{
		ID: "call_1", Name: "read_file", Result: "module nacelle",
	}})
	m.record(nacelle.Event{Kind: nacelle.KindText, Text: "it is the nacelle module"})
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "it is the nacelle module"})
	m.settle()

	want := []struct {
		role  nacelle.Role
		parts []string
	}{
		{nacelle.RoleUser, []string{"text"}},
		{nacelle.RoleAssistant, []string{"text", "call"}},
		{nacelle.RoleUser, []string{"result"}},
		{nacelle.RoleAssistant, []string{"text"}},
	}
	if len(m.conversation) != len(want) {
		t.Fatalf("conversation = %d messages, want %d: %+v", len(m.conversation), len(want), m.conversation)
	}
	for i, expected := range want {
		if role := m.conversation[i].Role; role != expected.role {
			t.Errorf("message %d role = %q, want %q", i, role, expected.role)
		}
		if got := shape(m.conversation[i]); strings.Join(got, ",") != strings.Join(expected.parts, ",") {
			t.Errorf("message %d parts = %v, want %v", i, got, expected.parts)
		}
	}
}

// The call and the result have to carry the same id, because that pairing is
// the only thing telling the model which answer belongs to which question when
// it asked for two tools at once.
func TestTheCallAndItsResultKeepTheSameID(t *testing.T) {
	m := using(t)
	m.record(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{
		ID: "call_1", Name: "read_file", Result: "module nacelle", Err: errors.New("no such file"),
	}})
	m.settle()

	call, okCall := m.conversation[1].Parts[1].(nacelle.ToolCall)
	result, okResult := m.conversation[2].Parts[0].(nacelle.ToolResult)
	if !okCall || !okResult {
		t.Fatalf("conversation = %+v, want a call answered by a result", m.conversation)
	}
	if call.ID != result.ID || call.ID != "call_1" {
		t.Errorf("ids = %q and %q, want both call_1", call.ID, result.ID)
	}
	if string(call.Input) != `{"path":"go.mod"}` {
		t.Errorf("input = %q, want the bytes the model wrote", call.Input)
	}
	if !result.Failed {
		t.Error("a tool that errored was recorded as having succeeded")
	}
}

// A call nothing answered is what an abandoned run and a capped run both leave
// behind, and every provider rejects a conversation carrying one. The turn has
// to go back without it.
func TestACallNothingAnsweredIsNotReplayed(t *testing.T) {
	m := using(t)
	m.settle()

	for _, message := range m.conversation {
		for _, part := range shape(message) {
			if part == "call" {
				t.Fatalf("conversation = %+v, want the unanswered call left out", m.conversation)
			}
		}
	}
	if len(m.conversation) != 2 || said(m.conversation[1]) != "let me look" {
		t.Errorf("conversation = %+v, want the question and what the model managed to say", m.conversation)
	}
}

// A transcript read back later still has to tell an answer that finished from
// one the token ceiling cut off, which is the reason Finish is a part at all.
func TestWhyTheRunStoppedIsRecordedWithTheTurn(t *testing.T) {
	m := sized()
	m.run.busy = true
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "half an ans"})
	m.absorb(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopMaxTokens})
	m.settle()

	if got := shape(m.conversation[0]); strings.Join(got, ",") != "text,finish" {
		t.Fatalf("parts = %v, want the answer and why it stopped", got)
	}
	if finish := m.conversation[0].Parts[1].(nacelle.Finish); finish.Stop != nacelle.StopMaxTokens {
		t.Errorf("stop = %q, want the ceiling that cut it off", finish.Stop)
	}
}
