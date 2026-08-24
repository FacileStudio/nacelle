package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// SubAgentToolName is the name the sub-agent tool registers under, and the
// name stripped from the tools a nested run inherits. Stripping by this name
// is the recursion guard: a sub-agent cannot ask for another sub-agent, so
// delegation is exactly one level deep unless a caller builds that on purpose.
const SubAgentToolName = "subagent"

// SubAgentOptions overrides what the nested agent inherits from its parent's
// Config. The zero value is a working sub-agent: it runs on the parent's
// backend and system prompt, under the parent's iteration ceiling, with the
// parent's tools minus the sub-agent itself.
type SubAgentOptions struct {
	// Name is the tool name the model calls, defaulting to SubAgentToolName.
	// Renaming it renames what the recursion guard strips too.
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
}

// NewSubAgentTool builds a `task`-style delegation tool: a fresh Agent run,
// on the same backend as cfg but with its own message list and its own
// context, that works a task to completion and returns only its final answer.
//
// The parent's event stream sees one tool call and one tool result — whatever
// RunTool already reports — and nothing else. Text, thinking and usage from
// the nested run are consumed here: a transcript showing two agents talking
// over each other is a transcript nobody can read.
//
// Everything the nested run does is bounded: its tools are cfg.Tools with the
// sub-agent removed, its iterations come from opts or cfg.MaxIterations, and
// its approvals come from opts.Approve, defaulting to deny-all. The tool is
// built eagerly, so a backend that cannot honour the inherited config fails
// here rather than the first time the model delegates.
func NewSubAgentTool(cfg Config, opts SubAgentOptions) (Tool, error) {
	name := opts.Name
	if name == "" {
		name = SubAgentToolName
	}

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

	nested, err := New(Config{
		Backend:       cfg.Backend,
		System:        system,
		Thinking:      thinking,
		MaxTokens:     cfg.MaxTokens,
		MaxIterations: iterations,
		Tools:         withoutTool(cfg.Tools, name),
		MCP:           cfg.MCP,
		Approve:       approve,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return nil, fmt.Errorf("nacelle: building the %s agent: %w", name, err)
	}

	description := opts.Description
	if description == "" {
		description = "Delegate a self-contained task to a fresh assistant run with its own context window. " +
			"The task must carry everything needed to do the work: no part of this conversation carries over, " +
			"and nothing the delegate does is visible here except the final answer it returns."
	}

	return NewTool(name, description, func(ctx context.Context, in subAgentInput) (string, error) {
		return delegate(ctx, nested, in.Task)
	})
}

// subAgentInput is what the model hands the tool: the task, whole.
type subAgentInput struct {
	Task string `json:"task" jsonschema:"required,description=The complete task for the delegate, with every fact and file path it needs"`
}

// delegate runs the nested agent to completion and returns its final text.
//
// A stream error ends the delegation with that error, which is reported to
// the parent's model like any tool failure. A run that stopped short without
// erroring — out of iterations, cut off mid-answer — comes back as text that
// says so, because handing the caller a truncated answer shaped like a whole
// one is the failure Stop exists to prevent.
func delegate(ctx context.Context, nested *Agent, task string) (string, error) {
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
			if event.Stop != "" && event.Stop != StopTools {
				stop = event.Stop
			}
		}
	}

	summary := strings.TrimSpace(answer.String())
	if summary == "" {
		summary = "the delegated run returned no answer"
	}
	if !stop.Complete() {
		summary += fmt.Sprintf("\n\n(The delegated run ended before finishing: %s.)", stop)
	}
	return summary, nil
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
