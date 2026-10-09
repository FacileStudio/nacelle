package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
)

type parallelTask struct {
	result string
	err    string
	usage  Usage
}

type parallelContext struct {
	cfg   Config
	opts  ParallelSubAgentOptions
	name  string
	batch string
}

func parallelDelegate(ctx context.Context, config parallelContext, tasks []string, maxConcurrency int) (string, error) {
	if len(tasks) == 0 {
		return "", fmt.Errorf("no tasks given")
	}
	results := make([]parallelTask, len(tasks))
	fanOut(ctx, config, tasks, maxConcurrency, func(idx int, pt parallelTask) {
		results[idx] = pt
	})
	data, err := json.Marshal(buildParallelResponse(results))
	if err != nil {
		return "", fmt.Errorf("encoding response: %w", err)
	}
	return string(data), nil
}

func fanOut(ctx context.Context, config parallelContext, tasks []string, maxConcurrency int, done func(int, parallelTask)) {
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for i, task := range tasks {
		sem <- struct{}{}
		wg.Add(1)
		go func(idx int, task string) {
			defer wg.Done()
			defer func() { <-sem }()
			done(idx, runParallelTask(ctx, config, idx, task))
		}(i, task)
	}
	wg.Wait()
}

func runParallelTask(ctx context.Context, config parallelContext, idx int, task string) parallelTask {
	nested, err := New(parallelSubAgentConfig(config.cfg, config.opts, config.name))
	if err != nil {
		return parallelTask{err: fmt.Sprintf("building agent: %v", err)}
	}
	var spent Usage
	cbs := buildDelegateCallbacks(config, idx, &spent)
	result, err := delegate(ctx, nested, task, cbs)
	if err != nil {
		return parallelTask{err: err.Error(), usage: spent}
	}
	return parallelTask{result: result, usage: spent}
}

func parallelSubAgentConfig(cfg Config, opts ParallelSubAgentOptions, name string) Config {
	nested := subAgentConfig(cfg, SubAgentOptions{
		Name:          name,
		Description:   opts.Description,
		System:        opts.System,
		MaxIterations: opts.MaxIterations,
		Approve:       opts.Approve,
		Usage:         opts.Callbacks.Usage,
	}, name)
	nested.Tools = withoutTool(nested.Tools, ParallelCancelToolName)
	return nested
}

type parallelResponse struct {
	Tasks  map[string]string `json:"tasks,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
	Usage  map[string]Usage  `json:"usage,omitempty"`
}

func buildParallelResponse(results []parallelTask) parallelResponse {
	resp := parallelResponse{
		Tasks:  make(map[string]string),
		Errors: make(map[string]string),
		Usage:  make(map[string]Usage),
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
		if r.usage.Total() > 0 {
			resp.Usage[key] = r.usage
		}
	}
	return resp
}

// ParallelTaskResult is the outcome of one detached parallel agent.
type ParallelTaskResult struct {
	Index  int
	Result string
	Err    string
	Usage  Usage
	Batch  string
}

// DelegateParallel is the detached counterpart to NewParallelSubAgentTool.
func DelegateParallel(ctx context.Context, cfg Config, tasks []string, opts ParallelSubAgentOptions) (<-chan ParallelTaskResult, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks given")
	}
	name := opts.Name
	if name == "" {
		name = ParallelAgentsToolName
	}
	config := parallelContext{
		cfg:   cfg,
		opts:  opts,
		name:  name,
		batch: opts.Batch,
	}
	results := make(chan ParallelTaskResult)
	go func() {
		defer close(results)
		fanOut(ctx, config, tasks, clampConcurrency(opts.MaxConcurrency), func(idx int, pt parallelTask) {
			results <- ParallelTaskResult{Index: idx, Result: pt.result, Err: pt.err, Usage: pt.usage}
		})
	}()
	return results, nil
}

func clampConcurrency(n int) int {
	switch {
	case n == 0:
		return 16
	case n < 0 || n > 32:
		return 32
	}
	return n
}


func buildDelegateCallbacks(config parallelContext, idx int, spent *Usage) DelegateCallbacks {
	opts := config.opts.Callbacks
	return DelegateCallbacks{
		Report: func(usage Usage) {
			*spent = spent.Add(usage)
			if opts.Usage != nil {
				opts.Usage(usage)
			}
			if opts.LiveUsage != nil {
				opts.LiveUsage(config.batch, idx, usage)
			}
		},
		OnTool: func(name string) {
			if opts.Tool != nil {
				opts.Tool(config.batch, idx, name)
			}
		},
		OnToolDone: func(name string, err error) {
			if opts.ToolDone != nil {
				opts.ToolDone(config.batch, idx, name, err)
			}
		},
	}
}

