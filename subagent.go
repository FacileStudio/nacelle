package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// SubAgentOptions overrides what the nested agent inherits from its parent's
// Config. The zero value is a working sub-agent: it runs on the parent's
// backend and system prompt, under the parent's iteration ceiling, with the
// parent's tools minus the sub-agent itself.
type SubAgentOptions struct {
	// Name is the tool name the model calls, and the name the recursion guard
	// strips from the tools the nested run inherits.
	Name string

	// Description is what the model reads when choosing the tool. Empty
	// keeps the default, which describes the delegation shape rather than
	// any particular task.
	Description string

	// System replaces the parent's system prompt for the nested run. Empty
	// means the parent's.
	System string

	// MaxIterations caps the nested run, overriding the parent's ceiling
	// when positive. Zero inherits; the parent's zero means no cap, which
	// is the parent's own decision to make twice if it wants to.
	MaxIterations int

	// Approve governs tool calls inside the nested run. Nil — the default —
	// denies every call: a nested context has nobody to ask, and an approval
	// prompt surfacing from inside a tool result would be a question nobody
	// can answer honestly. A caller that wants the sub-agent to work hands
	// it a policy that decides without asking.
	Approve Approve

	// Usage receives what every nested turn costs, as it is spent. Nil —
	// the default — drops the delegate's spend on the floor, which makes
	// the session's own accounting quietly wrong the moment somebody
	// delegates: the work happened, the bill arrives, the counters never
	// moved. A caller that shows totals anywhere wires this into them.
	// It runs on the stream's goroutine; keep it cheap and non-blocking.
	Usage func(Usage)
}

// subAgentConfig builds the Config the nested agent runs on: the parent's
// backend and token budget, the parent's system prompt and iteration ceiling
// when the sub-agent options leave them zero, thinking hidden so the nested
// transcript never reaches the parent, and the parent's tools minus the
// sub-agent itself, which is the recursion guard.
func subAgentConfig(cfg Config, opts SubAgentOptions, name string) Config {
	system := opts.System
	if system == "" {
		system = cfg.System
	}
	iterations := opts.MaxIterations
	if iterations == 0 {
		iterations = cfg.MaxIterations
	}
	thinking := cfg.Thinking
	thinking.Show = false
	approve := opts.Approve
	if approve == nil {
		approve = func(context.Context, string, json.RawMessage) bool { return false }
	}
	return Config{
		Backend:       cfg.Backend,
		System:        system,
		Thinking:      thinking,
		MaxTokens:     cfg.MaxTokens,
		MaxIterations: iterations,
		Tools:         withoutTool(cfg.Tools, name),
		MCP:           cfg.MCP,
		Approve:       approve,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// delegate runs the nested agent to completion and returns its final text.
//
// A stream error ends the delegation with that error, which is reported to
// the parent's model like any tool failure. A run that stopped short without
// erroring — out of iterations, cut off mid-answer — comes back as text that
// says so, because handing the caller a truncated answer shaped like a whole
// one is the failure Stop exists to prevent.
func delegate(ctx context.Context, nested *Agent, task string, report func(Usage)) (string, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("no task given")
	}

	var answer strings.Builder
	stop := StopEnd
	for event, err := range nested.Stream(ctx, []Message{{Role: RoleUser, Parts: []Part{Text{Text: task}}}}) {
		if err != nil {
			return "", fmt.Errorf("the delegated run failed: %w", err)
		}
		switch event.Kind {
		case KindText:
			answer.WriteString(event.Text)
		case KindTurn, KindDone:
			if report != nil && event.Kind == KindTurn {
				report(event.Usage)
			}
			stop = trackStop(stop, event.Stop)
		}
	}

	return finish(answer.String(), stop), nil
}

// trackStop keeps the first non-tool stop reason seen, so a run that ended
// early reports why rather than the last event's reason.
func trackStop(current, stop Stop) Stop {
	if stop != "" && stop != StopTools {
		return stop
	}
	return current
}

// finish trims the accumulated text and appends a note when the run stopped
// short of completion.
func finish(answer string, stop Stop) string {
	summary := strings.TrimSpace(answer)
	if summary == "" {
		summary = "the delegated run returned no answer"
	}
	if !stop.Complete() {
		summary += fmt.Sprintf("\n\n(The delegated run ended before finishing: %s.)", stop)
	}
	return summary
}

// withoutTool copies tools, dropping the one named. The copy rather than an
// in-place filter matters because cfg.Tools belongs to the caller: slicing it
// would reorder a list the parent agent is about to send.
func withoutTool(tools []Tool, name string) []Tool {
	kept := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Name() != name {
			kept = append(kept, tool)
		}
	}
	return kept
}
