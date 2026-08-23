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
// reasoning to the conversation as prose, re-sending a chain of thought every
// later turn in the one field no provider wants it replayed in. The copy that
// does travel back is assembled inside the backend, where the provider's own
// shape for it survives.
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
//
// The batch this returns blocks until the model sends its first anything, and
// so does the one consume returns. Neither of them flushes what has been said
// first — Update does, before it runs either. See its doc comment for what
// went wrong while that was the other way round.
func (m *model) send(text string) tea.Cmd {
	m.run.stop = ""
	m.run.usage = nacelle.Usage{}
	m.run.interrupted = time.Time{}
	m.run.asked, m.run.answered = nil, nil
	m.run.running = map[string]string{}
	m.conversation = append(m.conversation, nacelle.UserText(text))

	ctx, cancel := context.WithCancel(context.Background())
	m.run.cancel = cancel
	m.run.busy = true
	m.run.results = start(ctx, m.agent, m.conversation)
	return tea.Batch(waitFor(m.run.results), m.spin.Tick)
}

// abandon stops the run in flight and everything it would have led to.
//
// It is what both stop keys do, and it is all they have in common: they differ
// only in what they mean when there is nothing to stop, where ctrl+c quits the
// client and esc does nothing at all. Stopping itself is one behaviour and
// this is it, in one place, so the two can never drift into stopping a run
// differently.
//
// interrupted is stamped here rather than by either caller. It is what the
// status line reads to say the run is stopping instead of leaving the spinner
// claiming it is still going, and it is what arms the force quit — busy is
// only cleared by settle, settle waits on the results channel, and a tool
// wedged on a subprocess never closes it.
//
// The queue goes with the run. Left alone it is delivered by settle, which
// cancelling reaches like any other ending, so stopping one run would start
// the next one on the spot — the opposite of what either key was pressed for.
func (m *model) abandon() {
	m.run.interrupted = time.Now()
	m.run.stop = abandoned
	m.run.pending = nil
	m.run.cancel()
	m.dropQueued()
}

// escaped is esc once the dropdown has had its turn: it stops a run in flight,
// and does nothing else ever.
//
// Idle it is not this client's key at all, which is why it reports the press
// unhandled rather than swallowing it. Claiming it would turn the one press
// every terminal reader uses to back out of something into a press that
// silently does nothing here, and stop it reaching the prompt.
//
// A run already asked to stop is left alone rather than asked again, and the
// press is still claimed. Asking twice re-stamps run.interrupted, which is
// what holds the force-quit offer open — so an esc held down, or tapped at a
// tool that is not listening, would keep ctrl+c meaning "quit now" for as long
// as the tapping lasted rather than the three seconds it is meant to. Past
// that window a fresh press is a fresh request, and cancels again.
//
// It lives beside abandon rather than in key for the same reason decide and
// navigateMenu do: a binding belongs with the thing it acts on, and this one
// acts on the run.
func (m *model) escaped() (bool, tea.Cmd) {
	if !m.run.busy {
		return false, nil
	}
	if time.Since(m.run.interrupted) >= forceQuit {
		m.abandon()
	}
	return true, nil
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
	m.compact()

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
//
// Sequence, not Batch, for the reason prints() and route() both give: a batch
// makes no promise about the order its commands run in, and /clear returns one
// that prints. Two queued /clears under a batch can interleave their blank
// runs with each other's transcript, which is a scrambled screen rather than a
// cleared one. Batch was safe only while every command returned something
// order-independent, and that stopped being true without the type changing.
//
// What makes sequencing safe here is the break above: send's own Cmd blocks on
// the results channel, so a run-starting line must be the last thing in cmds
// or everything behind it would wait for the run's first event. Breaking on
// run.busy is what guarantees that, and it is now load-bearing twice.
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
	return tea.Sequence(cmds...)
}
