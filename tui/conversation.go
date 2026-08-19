package main

import (
	"encoding/json"

	"github.com/FacileStudio/nacelle"
)

// record folds one event into the conversation the next question will be asked
// with.
//
// This is a separate job from what reaches the screen, because the two want
// different things: the transcript is a log and the conversation is a request.
// A run is many turns — what the model said, the tools it asked for, their
// results, then more of the same — and it has to be handed back in the shape it
// was produced in, or the next question resumes from a transcript the model
// never wrote.
//
// The turn boundary is taken from the tool results rather than from KindTurn,
// which is the event that nominally ends a turn. The two backends agree on the
// order of a call and the result answering it, and disagree on where KindTurn
// falls between them: the Anthropic backend reports a call inside the turn that
// asked for it, and OpenRouter reports it after that turn has already ended.
// Keying on the results is what makes one state machine correct on both.
func (m *model) record(event nacelle.Event) {
	switch event.Kind {
	case nacelle.KindText:
		m.closeResults()
	case nacelle.KindToolCall:
		m.closeResults()
		m.run.asked = append(m.run.asked, nacelle.ToolCall{
			ID:       event.Tool.ID,
			Name:     event.Tool.Name,
			Input:    json.RawMessage(event.Tool.Input),
			Finished: true,
		})
	case nacelle.KindToolResult:
		if event.Tool.Discarded {
			m.forgetAsked(event.Tool.ID)
			return
		}
		m.closeTurn("")
		m.run.answered = append(m.run.answered, nacelle.ToolResult{
			ID:     event.Tool.ID,
			Name:   event.Tool.Name,
			Result: event.Tool.Result,
			Failed: event.Tool.Err != nil,
		})
	}
}

// forgetAsked drops one call from the turn being built, as if the model had
// never made it.
//
// This is what a Discarded result means: an attempt that produced the call
// was superseded before it ran, and the backend that discarded it never
// replays it either — see anthropic/unanswered.go's discard. Closing the
// turn here the ordinary way would fabricate history the model never
// actually has: a call it never made, answered by an error it never saw.
func (m *model) forgetAsked(id string) {
	kept := m.run.asked[:0]
	for _, call := range m.run.asked {
		if part, ok := call.(nacelle.ToolCall); !ok || part.ID != id {
			kept = append(kept, call)
		}
	}
	m.run.asked = kept
}

// closeResults commits the tool results collected since the last turn.
//
// They go back as the user's side of the conversation whatever they look like,
// because that is what Anthropic's shape is and this package follows it: a tool
// result is a block inside the user turn there, and a message of its own in the
// OpenAI schema, which the OpenRouter backend splits out at its own edge.
func (m *model) closeResults() {
	if len(m.run.answered) == 0 {
		return
	}
	m.conversation = append(m.conversation, nacelle.Message{Role: nacelle.RoleUser, Parts: m.run.answered})
	m.run.answered = nil
}

// closeTurn ends the assistant turn on screen and in the conversation: what the
// model said, the tools it asked for, and why it stopped when it did.
//
// A turn with nothing in it is not committed. A run abandoned before the model
// said anything has no assistant message to send, and an empty one is refused
// by both APIs rather than ignored.
func (m *model) closeTurn(stop nacelle.Stop) {
	parts := m.run.asked
	m.run.asked = nil

	if said := m.flush(); said != "" {
		parts = append([]nacelle.Part{nacelle.Text{Text: said}}, parts...)
	}
	if stop != "" {
		parts = append(parts, nacelle.Finish{Stop: stop})
	}
	if len(parts) == 0 {
		return
	}
	m.conversation = append(m.conversation, nacelle.Message{Role: nacelle.RoleAssistant, Parts: parts})
}

// dropUnanswered forgets the tool calls of a run that ended before their
// results arrived.
//
// Every call whose result did arrive was committed by closeTurn at the moment
// it did, so whatever is left here was never answered — and that is neither
// hypothetical nor an error. A run capped at its iteration limit reports the
// tools it stopped short of and runs none of them, and a run the reader
// abandoned mid-tool never reaches the result at all; the OpenRouter backend
// says as much outright, that a consumer pairing calls to results by id must
// tolerate a call with no answer.
//
// Replaying one is the thing that is not allowed. Every provider rejects a
// conversation holding a tool call nothing answered, so the honest turn to send
// is the one without it.
func (m *model) dropUnanswered() { m.run.asked = nil }
