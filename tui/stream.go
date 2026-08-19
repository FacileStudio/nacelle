package main

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// result is one thing an agent run produced: an event, or the error that ended
// it. It doubles as the Bubble Tea message, so nothing has to be translated
// between the goroutine reading the run and the loop drawing it.
type result struct {
	event nacelle.Event
	err   error
}

// finished says the run's channel closed and no more results are coming.
type finished struct{}

// start runs an agent in its own goroutine and hands back the channel its
// results arrive on. The channel closes when the run ends, however it ends,
// which is what lets the update loop tell "still streaming" from "over".
func start(ctx context.Context, agent *nacelle.Agent, conversation []nacelle.Message) <-chan result {
	results := make(chan result)

	go func() {
		defer close(results)
		for event, err := range agent.Stream(ctx, conversation) {
			select {
			case results <- result{event: event, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	return results
}

// waitFor takes exactly one result and re-arms itself from Update.
//
// This is the shape Bubble Tea wants for a stream, rather than sending into
// the program from the goroutine: each command runs on its own goroutine, so
// the blocking receive costs nothing, and every state change still happens in
// the update loop where it can be reasoned about. A closed channel arriving as
// finished is the end-of-run signal, so no separate sentinel is needed.
func waitFor(results <-chan result) tea.Cmd {
	return func() tea.Msg {
		next, open := <-results
		if !open {
			return finished{}
		}
		return next
	}
}
