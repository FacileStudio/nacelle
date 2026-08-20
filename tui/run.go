package main

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// inflight is the one run this client allows at a time: how to hear from it,
// how to abandon it, what it has produced, and what it has cost.
//
// Reasoning gets a buffer of its own: sharing the answer's put the last
// thought against the first word with no separator, and worse, committed the
// reasoning to the conversation — re-sending a chain of thought every later
// turn that the provider bills for and does not want replayed.
//
// usage is this run alone, because KindTurn accumulates and KindDone
// replaces: a counter carried over from the last run would double-count its
// total against this run's turns.
//
// waiting is true from the moment a question is sent until the first event
// comes back, success or error — otherwise a screen that has not moved since
// the question was echoed looks exactly like a hung client, not a slow one.
//
// pending is set only when -approve-tools is on and a call is waiting on a
// decision — nil otherwise, so status() and key() only change behaviour for
// someone who asked for a gate at all.
type inflight struct {
	results     <-chan result
	cancel      context.CancelFunc
	answer      strings.Builder
	reasoning   strings.Builder
	usage       nacelle.Usage
	stop        nacelle.Stop
	interrupted time.Time
	busy        bool
	waiting     bool
	asked       []nacelle.Part
	answered    []nacelle.Part
	pending     *approvalRequest
}

// send starts a run over text — a plain question as typed, or a
// /skill:name's own SKILL.md content with whatever followed the name
// appended. Everything from the last run is cleared here, nowhere else: a
// leftover stop reason mislabels this answer truncated, a leftover usage
// double-counts, and a leftover interruption quits a fresh run outright.
//
// render() runs again here, after waiting is set: ask()'s own echo already
// rendered once with waiting still false, and nothing else redraws the
// viewport before the first event arrives, so the spinner line would
// otherwise never appear until something else happened to trigger it.
func (m *model) send(text string) tea.Cmd {
	m.run.stop = ""
	m.run.usage = nacelle.Usage{}
	m.run.interrupted = time.Time{}
	m.run.asked, m.run.answered = nil, nil
	m.run.waiting = true
	m.conversation = append(m.conversation, nacelle.UserText(text))
	m.render()

	ctx, cancel := context.WithCancel(context.Background())
	m.run.cancel = cancel
	m.run.busy = true
	m.run.results = start(ctx, m.agent, m.conversation)
	return tea.Batch(waitFor(m.run.results), m.spin.Tick)
}

// consume folds one result into the transcript and waits for the next. The
// answer is committed before the error is — an error printed first would
// tell the reader the request failed, then show the half sentence it
// interrupted, the wrong order to read a failure in.
//
// waiting ends here, on the very first result whatever kind it is: an error
// is still an answer to "is anything happening," and the spinner's job was
// only ever to cover the silence before that first response.
func (m *model) consume(next result) tea.Cmd {
	m.run.waiting = false
	if next.err != nil {
		m.flush()
		m.say(fromFailure, next.err.Error())
		return waitFor(m.run.results)
	}
	m.record(next.event)
	m.absorb(next.event)
	m.render()
	return waitFor(m.run.results)
}

// settle ends a run: whatever streamed is committed, what it cost joins the
// session total, and the prompt opens again. Usage is folded into spent
// here, not left standing, so the next question starts from a clean counter
// and the status line keeps showing a total that only grows.
//
// waiting and pending are cleared here too, not only on their own paths: a
// stream that closes without yielding anything never reaches either one, and
// nothing should be left spinning, or asking a question, about a dead run.
func (m *model) settle() tea.Cmd {
	m.run.cancel()
	m.run.busy = false
	m.run.waiting = false
	m.run.pending = nil

	m.closeResults()
	m.dropUnanswered()
	m.closeTurn(m.run.stop)
	m.spent = m.spent.Add(m.run.usage)
	m.run.usage = nacelle.Usage{}

	m.render()
	return nil
}
