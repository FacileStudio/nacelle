package nacelle

// LegacyToolNames maps each built-in tool name used before v0.32 to the name
// that replaced it, so a consumer replaying a stored transcript can rename
// the calls in it to tools the current request declares. todo_write is the
// name a consumer's own task-list tool is expected to adopt. Every call
// returns a new map, so the caller may change it.
func LegacyToolNames() map[string]string {
	return map[string]string{
		"search_content": "grep",
		"find_files":     "glob",
		"run_command":    "bash",
		"tasks":          "todo_write",
	}
}
