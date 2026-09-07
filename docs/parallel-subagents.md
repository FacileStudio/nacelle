# Parallel Sub-Agents

## Overview

The parallel sub-agent feature enables multiple sub-agents to run concurrently, each handling independent tasks. This allows for faster processing of multiple independent tasks and better resource utilization.

## Key Features

1. **Concurrent Execution**: Multiple sub-agents run simultaneously, each with its own context window
2. **Independent Tasks**: Each sub-agent handles a distinct task without interference
3. **Result Collection**: Results are collected and returned in a structured JSON format
4. **Error Isolation**: If one task fails, others continue to completion
5. **Concurrency Control**: Configurable maximum concurrency level (default: 4, max: 8)

## Implementation Details

### Tool Interface

```go
// NewParallelSubAgentTool creates a tool that fans out to multiple concurrent agents
func NewParallelSubAgentTool(cfg Config, opts ParallelSubAgentOptions) (Tool, error)

// ParallelSubAgentOptions configures the parallel sub-agent behavior
ParallelSubAgentOptions struct {
    Name           string
    Description    string
    System         string
    MaxIterations  int
    MaxConcurrency int
    Approve        Approve
    Usage          func(Usage)
}
```

### Usage Example

```go
// Create a parallel sub-agent tool
parallelTool, err := nacelle.NewParallelSubAgentTool(
    nacelle.Config{Backend: backend, System: "s"},
    nacelle.ParallelSubAgentOptions{MaxConcurrency: 4},
)

// Use the tool in an agent configuration
agent, err := nacelle.New(nacelle.Config{
    Backend: backend,
    Tools: []nacelle.Tool{parallelTool},
})
```

## Best Practices

1. **Task Decomposition**: Break work into independent subtasks for maximum parallelism
2. **Error Handling**: Design tasks to handle failures gracefully when run in parallel
3. **Resource Management**: Monitor system resources when running many concurrent tasks
4. **Result Synthesis**: Plan how to combine results from multiple parallel tasks

## Limitations

1. **Shared Resources**: Tasks that require shared resources may need coordination
2. **Complex Dependencies**: Tasks with complex dependencies may not benefit from parallel execution
3. **Error Recovery**: Complex error recovery may be needed when multiple tasks fail

## Future Enhancements

1. **Dynamic Task Assignment**: Agents could claim tasks from a shared queue
2. **Progress Tracking**: Better tracking of task progress and completion
3. **Result Merging**: More sophisticated result merging capabilities
4. **Priority Scheduling**: Support for prioritizing certain tasks over others

## Changelog

### Added

- **Parallel sub-agent support**: Added `NewParallelSubAgentTool` and `ParallelSubAgentOptions`
- **Concurrent task execution**: Multiple tasks can now run concurrently
- **Result collection**: Structured JSON output with task results and errors
- **Concurrency control**: Configurable maximum concurrency level
- **Error isolation**: Failed tasks don't stop other tasks from completing

### Fixed

- **Task ordering**: Fixed issue with task ordering in parallel execution
- **Result formatting**: Improved JSON result formatting for better readability
- **Error handling**: Enhanced error handling for parallel task execution

### Changed

- **API consistency**: Aligned parallel sub-agent API with single sub-agent API
- **Documentation**: Improved documentation for parallel sub-agent features

### Removed

- **Redundant code**: Removed duplicate code from parallel sub-agent implementation

## Migration Guide

### From Single Sub-Agent

1. Replace `NewSubAgentTool` with `NewParallelSubAgentTool`
2. Update configuration to use `ParallelSubAgentOptions`
3. Adjust task handling to work with concurrent execution
4. Update result processing to handle JSON output format

### From Sequential Execution

1. Identify independent tasks that can run in parallel
2. Break work into smaller, independent tasks
3. Update task handling to work with concurrent execution
4. Implement result merging for parallel task results

## Troubleshooting

### Common Issues

1. **Task Dependencies**: Tasks with dependencies may not benefit from parallel execution
2. **Resource Limits**: Running too many tasks concurrently may hit system resource limits
3. **Error Handling**: Complex error handling may be needed for parallel task execution

### Debugging Tips

1. **Log Output**: Enable logging to track task execution and results
2. **Concurrency Level**: Adjust concurrency level based on system resources
3. **Task Isolation**: Ensure tasks are properly isolated to avoid interference

## Examples

### Basic Usage

```go
// Create a parallel sub-agent tool
parallelTool, err := nacelle.NewParallelSubAgentTool(
    nacelle.Config{Backend: backend, System: "s"},
    nacelle.ParallelSubAgentOptions{MaxConcurrency: 4},
)

// Use the tool in an agent configuration
agent, err := nacelle.New(nacelle.Config{
    Backend: backend,
    Tools: []nacelle.Tool{parallelTool},
})
```

### Handling Results

```go
// Process the results from parallel task execution
result, err := parallelTool.Run(context.Background(), json.RawMessage(`{"tasks":["task1","task2","task3"]}`))

// Parse the JSON result
var resp struct {
    Tasks  map[string]string `json:"tasks"`
    Errors map[string]string `json:"errors"`
}
if err := json.Unmarshal([]byte(result), &resp); err != nil {
    // Handle error
}
```

### Error Handling

```go
// Check for errors in the result
if len(resp.Errors) > 0 {
    for task, errMsg := range resp.Errors {
        fmt.Printf("Task %s failed: %s\n", task, errMsg)
    }
}
```

## Performance Considerations

1. **Concurrency Level**: Adjust based on system resources and task complexity
2. **Task Size**: Smaller tasks benefit more from parallel execution
3. **Network Latency**: Consider network latency when running tasks remotely
4. **Resource Usage**: Monitor CPU, memory, and network usage during parallel execution

## Security Considerations

1. **Task Isolation**: Ensure tasks are properly isolated to prevent interference
2. **Input Validation**: Validate all task inputs to prevent injection attacks
3. **Error Handling**: Properly handle and log errors to prevent security issues

## Integration with Other Features

1. **Tool Call Planning**: Works with tool call planning for better task ordering
2. **Context Window**: Respects context window limits for each parallel task
3. **Thinking Support**: Supports thinking for each parallel task execution

## Open Questions

1. **Dynamic Task Assignment**: How to implement dynamic task assignment from a shared queue?
2. **Progress Tracking**: What's the best way to track progress of parallel tasks?
3. **Result Merging**: How to implement more sophisticated result merging?
4. **Priority Scheduling**: How to implement priority scheduling for tasks?

## Future Work

1. **Dynamic Task Assignment**: Implement dynamic task assignment from a shared queue
2. **Progress Tracking**: Add better progress tracking for parallel tasks
3. **Result Merging**: Implement more sophisticated result merging capabilities
4. **Priority Scheduling**: Add support for prioritizing certain tasks over others

## Conclusion

The parallel sub-agent feature provides significant improvements in processing multiple independent tasks concurrently. By following the best practices and considering the performance and security implications, you can effectively leverage this feature in your applications.