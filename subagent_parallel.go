package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
)

// ParallelSubAgentToolName is the name the parallel sub-agent tool registers
// under, and the name stripped from the tools nested parallel agents inherit.
const ParallelSubAgentToolName = "parallel_subagent"

// ParallelSubAgentOptions overrides what each nested agent inherits from its
// parent's Config. The zero value runs up to 4 tasks concurrently on the
// parent's backend and system prompt, under the parent's iteration ceiling,
// with the parent's tools minus the parallel subagent itself.
type ParallelSubAgentOptions struct {
	// Name is the tool name the model calls, defaulting to ParallelSubAgentToolName.
	Name string

	// Description is what the model reads when choosing the tool.
	Description string

	// System replaces the parent's system prompt for all nested runs. Empty
	// means the parent's.
	System string

	// MaxIterations caps each nested run, overriding the parent's ceiling
	// when positive.
	MaxIterations int

	// MaxConcurrency caps how many agents run at once. Zero means 4; values
	// above 8 are clamped to 8.
	MaxConcurrency int

	// Approve governs tool calls inside each nested run. Nil — the default —
	// denies every call.
	Approve Approve

	// Usage receives what every nested turn costs. Nil drops it.
	Usage func(Usage)
}

// NewParallelSubAgentTool builds a tool that fans out to multiple concurrent
// agents, one per task, and collects their results. The parent's event stream
// sees one tool call and one tool result; thinking and usage from nested runs
// are consumed here.
//
// Each task runs in its own fresh agent on the same backend. Results are
// returned as a JSON object mapping task index to result string:
//
//	{"tasks":{"0":"result of task 0","1":"result of task 1"}}
//
// Errors from individual tasks appear in the "errors" field:
//
//	{"errors":{"1":"task 1 failed: ..."}}
//
// An empty task list fails the call. Tasks are processed concurrently up to
// MaxConcurrency; if one task fails, others continue to completion.
func NewParallelSubAgentTool(cfg Config, opts ParallelSubAgentOptions) (Tool, error) {
	name := opts.Name
	if name == "" {
		name = ParallelSubAgentToolName
	}

	description := opts.Description
	if description == "" {
		description = "Delegate independent sub-tasks to parallel assistant runs. " +
			"Each task runs in its own agent concurrently, and all results are " +
			"returned together. Use it when tasks are independent: search multiple " +
			"sources, analyze multiple files, or run multiple investigations at once. " +
			"Results are returned as a JSON map of task index to result string."
	}

	maxConcurrency := clampConcurrency(opts.MaxConcurrency)

	return NewTool(name, description, func(ctx context.Context, in parallelSubAgentInput) (string, error) {
		config := parallelContext{
			cfg:  cfg,
			opts: opts,
			name: name,
		}
		return parallelDelegate(ctx, config, in.Tasks, maxConcurrency)
	})
}

func clampConcurrency(n int) int {
	switch {
	case n == 0:
		return 4
	case n < 0 || n > 8:
		return 8
	}
	return n
}

// parallelSubAgentInput is what the model hands the parallel tool.
type parallelSubAgentInput struct {
	Tasks []string `json:"tasks" jsonschema:"required,minItems=1,description=List of independent tasks to run in parallel"`
}

// parallelTask holds the result of one parallel agent run.
type parallelTask struct {
	result string
	err    string
}

// parallelContext carries everything parallelDelegate needs to spawn a worker.
type parallelContext struct {
	cfg  Config
	opts ParallelSubAgentOptions
	name string
}

// parallelDelegate runs each task in its own goroutine up to maxConcurrency at
// a time, collects results, and returns a JSON map.
func parallelDelegate(ctx context.Context, config parallelContext, tasks []string, maxConcurrency int) (string, error) {
	if len(tasks) == 0 {
		return "", fmt.Errorf("no tasks given")
	}

	sem := make(chan struct{}, maxConcurrency)
	results := make([]parallelTask, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		sem <- struct{}{}
		wg.Add(1)
		go func(idx int, task string) {
			defer wg.Done()
			defer func() { <-sem }()

			results[idx] = runParallelTask(ctx, config, task)
		}(i, task)
	}

	wg.Wait()

	data, err := json.Marshal(buildParallelResponse(results))
	if err != nil {
		return "", fmt.Errorf("encoding response: %w", err)
	}
	return string(data), nil
}

// runParallelTask executes one task inside a nested agent and returns the result.
func runParallelTask(ctx context.Context, config parallelContext, task string) parallelTask {
	nested, err := New(parallelSubAgentConfig(config.cfg, config.opts, config.name))
	if err != nil {
		return parallelTask{err: fmt.Sprintf("building agent: %v", err)}
	}

	result, err := delegate(ctx, nested, task, config.opts.Usage)
	if err != nil {
		return parallelTask{err: err.Error()}
	}
	return parallelTask{result: result}
}

// parallelSubAgentConfig builds the Config each parallel nested agent runs on.
func parallelSubAgentConfig(cfg Config, opts ParallelSubAgentOptions, name string) Config {
	return subAgentConfig(cfg, SubAgentOptions{
		Name:          name,
		Description:   opts.Description,
		System:        opts.System,
		MaxIterations: opts.MaxIterations,
		Approve:       opts.Approve,
		Usage:         opts.Usage,
	}, name)
}

// parallelResponse is the JSON returned to the model.
type parallelResponse struct {
	Tasks  map[string]string `json:"tasks,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
}

// buildParallelResponse builds the JSON-serializable response map from results.
func buildParallelResponse(results []parallelTask) parallelResponse {
	resp := parallelResponse{
		Tasks:  make(map[string]string),
		Errors: make(map[string]string),
	}
	for i, r := range results {
		key := strconv.Itoa(i)
		if r.err != "" {
			resp.Errors[key] = r.err
			continue
		}
		if r.result != "" {
			resp.Tasks[key] = r.result
		}
	}
	return resp
}
