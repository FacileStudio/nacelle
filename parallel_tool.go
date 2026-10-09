package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

const ParallelAgentsToolName = "parallel_agents"
const ParallelCancelToolName = "parallel_cancel"

// ParallelSubAgentCallbacks groups the callbacks for ParallelSubAgentOptions.
type ParallelSubAgentCallbacks struct {
	Usage     func(Usage)
	LiveUsage func(batch string, idx int, usage Usage)
	Results   func(ParallelTaskResult)
	Tool      func(batch string, idx int, name string)
	ToolDone  func(batch string, idx int, name string, err error)
}

// ParallelSubAgentOptions overrides what each nested agent inherits from its
// parent's Config.
type ParallelSubAgentOptions struct {
	Name           string
	Description    string
	System         string
	MaxIterations  int
	MaxConcurrency int
	Approve        Approve
	Detach         bool
	Batch          string
	Registry       *ParallelRegistry
	Callbacks      ParallelSubAgentCallbacks
}

var parallelBatch atomic.Uint64

// NewParallelSubAgentTool builds a tool that fans out to multiple concurrent
// agents, one per task, and collects their results.
func NewParallelSubAgentTool(cfg Config, opts ParallelSubAgentOptions) (Tool, error) {
	name := opts.Name
	if name == "" {
		name = ParallelAgentsToolName
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
			cfg:   cfg,
			opts:  opts,
			name:  name,
			batch: opts.Batch,
		}
		if opts.Detach {
			return detachToolResult(cfg, opts, in.Tasks)
		}
		return parallelDelegate(ctx, config, in.Tasks, maxConcurrency)
	})
}

type detachParallelResult struct {
	Started int    `json:"started"`
	Batch   string `json:"batch"`
}

func detachToolResult(cfg Config, opts ParallelSubAgentOptions, tasks []string) (string, error) {
	batch := fmt.Sprintf("psa-%d", parallelBatch.Add(1))
	opts.Batch = batch
	ctx, cancel := context.WithCancel(context.Background())
	results, err := DelegateParallel(ctx, cfg, tasks, opts)
	if err != nil {
		cancel()
		return "", err
	}
	if opts.Registry != nil {
		opts.Registry.Register(batch, cancel)
	}
	go func() {
		if opts.Registry != nil {
			defer opts.Registry.Drop(batch)
		} else {
			defer cancel()
		}
		for r := range results {
			if opts.Callbacks.Results != nil {
				r.Batch = batch
				opts.Callbacks.Results(r)
			}
		}
	}()
	data, _ := json.Marshal(detachParallelResult{Started: len(tasks), Batch: batch})
	return string(data), nil
}

type parallelSubAgentInput struct {
	Tasks []string `json:"tasks" jsonschema:"required,minItems=1,description=List of independent tasks to run in parallel"`
}

func (in *parallelSubAgentInput) UnmarshalJSON(data []byte) error {
	var raw struct {
		Tasks json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Tasks) == 0 || string(raw.Tasks) == "null" {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw.Tasks, &list); err == nil {
		in.Tasks = list
		return nil
	}
	var single string
	if err := json.Unmarshal(raw.Tasks, &single); err != nil {
		return fmt.Errorf("tasks must be a list of strings, got %s", raw.Tasks)
	}
	trimmed := strings.TrimSpace(single)
	if strings.HasPrefix(trimmed, "[") && json.Unmarshal([]byte(trimmed), &list) == nil {
		in.Tasks = list
		return nil
	}
	in.Tasks = []string{single}
	return nil
}

type parallelCancelInput struct {
	Batch string `json:"batch" jsonschema:"required,description=The batch key of the parallel_agents run to cancel, from the started stub"`
}

// NewParallelCancelTool builds the tool a model uses to stop a detached
// parallel_agents fan-out it no longer wants.
func NewParallelCancelTool(registry *ParallelRegistry) (Tool, error) {
	return NewTool(ParallelCancelToolName,
		"Cancel a parallel_agents fan-out you started earlier, by the batch key its started stub named. Use it when a fan-out is no longer wanted: a task is stuck, the work became pointless, or you have what you need from the tasks that already finished. Cancelled tasks come back as errors on the results stream; finished tasks keep their results.",
		func(ctx context.Context, in parallelCancelInput) (string, error) {
			if in.Batch == "" {
				return "", fmt.Errorf("no batch given")
			}
			if registry == nil {
				return "", fmt.Errorf("cancel tool requires a parallel registry")
			}
			if !registry.Cancel(in.Batch) {
				return fmt.Sprintf("no live parallel batch named %q — it already finished or never existed", in.Batch), nil
			}
			return fmt.Sprintf("cancelled %q", in.Batch), nil
		})
}
