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
// running is every tool between its call and its result, keyed by call id so
// several at once are counted right — the SDK's own runner executes a turn's
// tools concurrently. It is what lets the status line name what it is waiting
// on instead of only that it is waiting, and it is emptied by settle rather
// than trusted to drain: a run capped at its iteration limit, or abandoned
// mid-tool, reaches no result for the calls it stopped short of.
//
// queued is what was typed while a run was still going, delivered when it
// settles. Refusing input until a run finishes is the behaviour this replaces,
// and it was worse than it sounds: the prompt silently ignored enter, so the
// only way to know a question had not been asked was to notice nothing
// happening. pi calls this a follow-up message and delivers it the same way.
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
	pending     *approvalRequest
	queued      []string
	running     map[string]string

	// turn is embedded rather than named so both fields still read as
	// m.run.asked and m.run.answered — the grouping exists only to keep
	// inflight's own field count from growing every time this list does,
	// the same reason Config embeds Discovery in config.go.
	turn
}

// turn is the assistant turn being built for the conversation: the tools it
// asked for, and the results collected to answer them. They are one state
// machine — see conversation.go, which is the only thing that drives them —
// and separate from everything above, which is about the run rather than
// about what will be sent back.
type turn struct {
	asked    []nacelle.Part
	answered []nacelle.Part
}

// send starts a run over text — a plain question as typed, or a
// /skill:name's own SKILL.md content with whatever followed the name
// appended. Everything from the last run is cleared here, nowhere else: a
// leftover stop reason mislabels this answer truncated, a leftover usage
// double-counts, and a leftover interruption quits a fresh run outright.
//
// running is emptied here rather than only in settle so a run never inherits
// the last one's unanswered calls and reports them as its own.
func (m *model) send(text string) tea.Cmd {
	m.run.stop = ""
	m.run.usage = nacelle.Usage{}
	m.run.interrupted = time.Time{}
	m.run.asked, m.run.answered = nil, nil
	m.run.running = map[string]string{}
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
func (m *model) consume(next result) tea.Cmd {
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
// pending and running are cleared here too, not only on their own paths: a
// stream that closes without yielding anything never reaches either one, and
// nothing should be left asking a question, or named as still running, about
// a dead run.
//
// Whatever was typed while this run was going is delivered last, once the
// state above is clean — send is what the dispatch reaches, and it would
// otherwise be handed a run that has not finished tidying up after itself.
// One at a time, because dispatching the next queued line starts a run that
// settles again and takes the one after it.
func (m *model) settle() tea.Cmd {
	m.run.cancel()
	m.run.busy = false
	m.run.pending = nil
	m.run.running = map[string]string{}

	m.closeResults()
	m.dropUnanswered()
	m.closeTurn(m.run.stop)
	m.spent = m.spent.Add(m.run.usage)
	m.run.usage = nacelle.Usage{}

	m.render()
	return m.deliver()
}

// deliver sends what was typed while the last run was going, and keeps going
// until something is actually running again.
//
// The loop is the whole point. dispatch has two outcomes and only one of them
// leads back here: a question starts a run, which settles and takes the next
// line, but a command answers on the spot and starts nothing. Popping a single
// line per settle therefore stranded everything behind the first queued
// /help — still drawn as queued, still holding its row, never sent and with
// nothing left that would ever send it, which is precisely the disappearing
// input this queue exists to prevent.
//
// Every command's own Cmd is collected rather than dropped, so a queued /quit
// still quits, and the loop stops on run.busy rather than on a non-nil Cmd —
// a command that returns work to do is not a command that started a run, and
// reading it as one would strand the queue all over again.
func (m *model) deliver() tea.Cmd {
	var cmds []tea.Cmd
	for len(m.run.queued) > 0 {
		next := m.run.queued[0]
		m.run.queued = m.run.queued[1:]
		m.layout(m.windowHeight)

		cmds = append(cmds, m.dispatch(next))
		if m.run.busy {
			break
		}
	}
	return tea.Batch(cmds...)
}
