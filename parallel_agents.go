package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// ParallelAgentsToolName is the name the parallel sub-agent tool registers
// under, and the name stripped from the tools nested parallel agents inherit.
const ParallelAgentsToolName = "parallel_agents"

// ParallelCancelToolName is the name the parallel cancel tool registers under.
// Nested parallel agents never inherit it: a subagent cancelling its own
// fan-out would be a run reaching up to kill its siblings and its parent.
const ParallelCancelToolName = "parallel_cancel"

// ParallelSubAgentOptions overrides what each nested agent inherits from its
// parent's Config. The zero value runs up to 4 tasks concurrently on the
// parent's backend and system prompt, under the parent's iteration ceiling,
// with the parent's tools minus the parallel subagent itself.
type ParallelSubAgentOptions struct {
	// Name is the tool name the model calls, defaulting to ParallelAgentsToolName.
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

	// LiveUsage receives each nested turn's spend as it streams, tagged with
	// the fan-out's batch key and the task's index, so a host can draw a
	// per-subagent token counter that moves while the fan-out runs instead of
	// waiting for the task's result. Nil drops it. It fires on the task's
	// stream goroutine, once per turn, just before Usage when both are set;
	// keep it cheap and non-blocking.
	LiveUsage func(batch string, idx int, usage Usage)

	// Detach makes the tool non-blocking. Run returns immediately with a stub
	// that says how many agents started (and the batch key Results tags its
	// results with), and the fan-out keeps going in the background, streaming
	// each task's outcome to Results as it finishes. The model reads the stub
	// and keeps its turn; the host surfaces the real results. This is the mode
	// an interactive UI wants, so its main thread is never pinned by the
	// parent waiting on one merged result. Nil Results drops the outcomes while
	// the work still runs. Zero (false) keeps the historical blocking behaviour.
	Detach bool

	// Results receives each task's outcome when Detach is set, one call per task
	// as it finishes, on a background goroutine — keep it cheap and non-blocking.
	Results func(ParallelTaskResult)

	// Batch is the fan-out's opaque key, forwarded to Tool so a host can route a
	// detached fan-out's live tool calls alongside the streamed results it
	// already tags. A raw DelegateParallel caller that names its own batches may
	// leave it empty and tag inside the Tool callback instead.
	Batch string

	// Tool receives each nested task's tool call as it begins. batch is the
	// fan-out's key (opts.Batch), idx is the task's index in the caller's list,
	// and name is the tool the subagent is about to run. It fires on the task's
	// stream goroutine; keep it cheap and non-blocking. A host draws "what is
	// this subagent doing right now" from it.
	Tool func(batch string, idx int, name string)

	// ToolDone receives each nested task's tool call as it ends. The arguments
	// match Tool's — batch, idx, the tool's name — plus err, nil when the call
	// succeeded and set when it failed. It fires on the task's stream goroutine;
	// keep it cheap and non-blocking. A host colours the running-tool glyph green
	// or red from it, since the call happened inside a subagent and no other
	// outcome event reaches the parent until the whole task returns.
	ToolDone func(batch string, idx int, name string, err error)
}

// parallelBatch hands out the batch key that ties a Detach'd fan-out's streamed
// results to the stub the model read, so a host can route overlapping calls.
var parallelBatch atomic.Uint64

// NewParallelSubAgentTool builds a tool that fans out to multiple concurrent
// agents, one per task, and collects their results. The parent's event stream
// sees one tool call and one tool result; thinking and usage from nested runs
// are consumed here.
//
// Each task runs in its own fresh agent on the same backend. Results are
// returned as a JSON object mapping task index to result string, with each
// task's spend alongside:
//
//	{"tasks":{"0":"result of task 0","1":"result of task 1"},
//	 "usage":{"0":{"InputTokens":..,"OutputTokens":..,..},"1":{..}}}
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

// detachParallelResult is the stub a Detach'd parallel tool returns so the
// parent's stream can continue instead of waiting on the whole fan-out. It
// names how many agents started and the batch Results tags its outcomes with.
type detachParallelResult struct {
	Started int    `json:"started"`
	Batch   string `json:"batch"`
}

// parallelCancels holds the cancel function of every live detached fan-out by
// batch key. detachToolResult registers on launch and drops the entry when the
// results channel closes, so a batch is cancellable exactly while it can still
// produce results.
var parallelCancels = struct {
	sync.Mutex
	byBatch map[string]context.CancelFunc
}{byBatch: map[string]context.CancelFunc{}}

func registerParallelCancel(batch string, cancel context.CancelFunc) {
	parallelCancels.Lock()
	parallelCancels.byBatch[batch] = cancel
	parallelCancels.Unlock()
}

func dropParallelCancel(batch string) {
	parallelCancels.Lock()
	delete(parallelCancels.byBatch, batch)
	parallelCancels.Unlock()
}

// CancelParallel cancels a detached fan-out by its batch key — the one the
// detach stub named and every Results callback is tagged with. It reports
// whether a live fan-out was found: false means the batch already finished or
// never existed. Each task still running ends as a context-cancelled error on
// the Results channel, so a host sees the killed tasks as failures and the ones
// that finished before the cancel keep their results.
func CancelParallel(batch string) bool {
	parallelCancels.Lock()
	cancel, ok := parallelCancels.byBatch[batch]
	parallelCancels.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// detachToolResult is the non-blocking run path. It launches the fan-out and
// immediately returns a stub, forwarding each task's result to opts.Results as
// the background work finishes. The model keeps its turn; a host that showed
// the stub to the reader can surface the real outcomes from Results.
//
// The fan-out runs on its own background context rather than the caller's, so
// it outlives the turn that launched it: a parent that finishes (or is
// cancelled) while its subagents grind does not take them down with it. Detach
// is the "keep going in the background" contract, and a background that dies
// with the turn that spawned it would violate that name. The cost of that
// contract is that only CancelParallel can stop a detached fan-out, which is
// why the registry above exists.
func detachToolResult(cfg Config, opts ParallelSubAgentOptions, tasks []string) (string, error) {
	batch := fmt.Sprintf("psa-%d", parallelBatch.Add(1))
	opts.Batch = batch
	ctx, cancel := context.WithCancel(context.Background())
	results, err := DelegateParallel(ctx, cfg, tasks, opts)
	if err != nil {
		cancel()
		return "", err
	}
	registerParallelCancel(batch, cancel)
	go func() {
		defer dropParallelCancel(batch)
		for r := range results {
			if opts.Results != nil {
				r.Batch = batch
				opts.Results(r)
			}
		}
	}()
	data, _ := json.Marshal(detachParallelResult{Started: len(tasks), Batch: batch})
	return string(data), nil
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

// parallelCancelInput is what the model hands the cancel tool: the batch key
// the detach stub returned when the fan-out was launched.
type parallelCancelInput struct {
	Batch string `json:"batch" jsonschema:"required,description=The batch key of the parallel_agents run to cancel, from the started stub"`
}

// NewParallelCancelTool builds the tool a model uses to stop a detached
// parallel_agents fan-out it no longer wants — one task stuck on a permission
// denial, a batch that has become pointless. The batch key is in the stub the
// fan-out returned when it started.
//
// Cancelling reports whether the batch was live. Tasks that had finished keep
// their results on the Results channel; each still-running task ends there as a
// context-cancelled error, so the host shows them as killed rather than as
// silently dropped work.
func NewParallelCancelTool() (Tool, error) {
	return NewTool(ParallelCancelToolName,
		"Cancel a parallel_agents fan-out you started earlier, by the batch key its started stub named. Use it when a fan-out is no longer wanted: a task is stuck, the work became pointless, or you have what you need from the tasks that already finished. Cancelled tasks come back as errors on the results stream; finished tasks keep their results.",
		func(ctx context.Context, in parallelCancelInput) (string, error) {
			if in.Batch == "" {
				return "", fmt.Errorf("no batch given")
			}
			if !CancelParallel(in.Batch) {
				return fmt.Sprintf("no live parallel batch named %q — it already finished or never existed", in.Batch), nil
			}
			return fmt.Sprintf("cancelled %q", in.Batch), nil
		})
}

// parallelTask holds the result of one parallel agent run.
type parallelTask struct {
	result string
	err    string
	usage  Usage
}

// parallelContext carries everything parallelDelegate needs to spawn a worker.
type parallelContext struct {
	cfg   Config
	opts  ParallelSubAgentOptions
	name  string
	batch string
}

// parallelDelegate runs each task in its own goroutine up to maxConcurrency at
// a time, collects results, and returns a JSON map.
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

// fanOut runs each task in its own goroutine, capped at maxConcurrency at a
// time, and calls done with each task's result as it finishes. It shares the
// semaphore that bounds concurrency between the blocking tool and the detached
// surface below, so neither can drift from the other on how many agents run at
// once.
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

// runParallelTask executes one task inside a nested agent and returns the result.
// The task's spend is accumulated per-index so the response can report what
// each subagent cost, while the caller's own Usage hook still receives every
// nested turn as before. idx is the task's place in the caller's list, forwarded
// with each tool call so a host can show which subagent is running what.
func runParallelTask(ctx context.Context, config parallelContext, idx int, task string) parallelTask {
	nested, err := New(parallelSubAgentConfig(config.cfg, config.opts, config.name))
	if err != nil {
		return parallelTask{err: fmt.Sprintf("building agent: %v", err)}
	}

	var spent Usage
	report := config.opts.Usage
	live := config.opts.LiveUsage
	accumulate := func(usage Usage) {
		spent = spent.Add(usage)
		if report != nil {
			report(usage)
		}
		if live != nil {
			live(config.batch, idx, usage)
		}
	}

	result, err := delegate(ctx, nested, task, accumulate, func(name string) {
		if tool := config.opts.Tool; tool != nil {
			tool(config.batch, idx, name)
		}
	}, func(name string, err error) {
		if done := config.opts.ToolDone; done != nil {
			done(config.batch, idx, name, err)
		}
	})
	if err != nil {
		return parallelTask{err: err.Error(), usage: spent}
	}
	return parallelTask{result: result, usage: spent}
}

// parallelSubAgentConfig builds the Config each parallel nested agent runs on.
func parallelSubAgentConfig(cfg Config, opts ParallelSubAgentOptions, name string) Config {
	nested := subAgentConfig(cfg, SubAgentOptions{
		Name:          name,
		Description:   opts.Description,
		System:        opts.System,
		MaxIterations: opts.MaxIterations,
		Approve:       opts.Approve,
		Usage:         opts.Usage,
	}, name)
	nested.Tools = withoutTool(nested.Tools, ParallelCancelToolName)
	return nested
}

// parallelResponse is the JSON returned to the model.
type parallelResponse struct {
	Tasks  map[string]string `json:"tasks,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
	Usage  map[string]Usage  `json:"usage,omitempty"`
}

// buildParallelResponse builds the JSON-serializable response map from results.
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

// ParallelTaskResult is the outcome of one detached parallel agent. Index names
// which task in the caller's list it came from; the fan-out posts these as each
// agent finishes, so a caller that streams them sees results arrive in the order
// the agents complete, not the order the tasks were given.
type ParallelTaskResult struct {
	Index  int
	Result string
	Err    string
	Usage  Usage
	// Batch is an opaque key tying a result to the fan-out that produced it. It
	// is set only when the result is forwarded by a Detach'd NewParallelSubAgentTool,
	// which lets one host route overlapping calls' outcomes; results on a
	// DelegateParallel channel leave it empty, since the caller already holds one
	// batch per channel.
	Batch string
}

// DelegateParallel is the detached counterpart to NewParallelSubAgentTool: it
// fans the tasks out to concurrent nested agents on cfg and returns a channel
// their results arrive on for the caller to drain, instead of blocking the
// caller until every task is done and handing back one merged JSON.
//
// The returned channel carries one ParallelTaskResult per task — with that
// task's own spend, the same per-index accounting the tool reports — and closes
// when the last task finishes. An empty task list is an error, returned rather
// than posted, matching the tool.
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
