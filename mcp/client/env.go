package client

import (
	"maps"
	"os"
	"slices"
)

// implementationVersion is what a server is told it is talking to.
//
// A constant rather than a build stamp because nacelle has no version to
// report yet, and MCP treats the field as display metadata: a server logs it,
// nothing negotiates on it. It gets a real value when the module gets a tag.
const implementationVersion = "0"

// defaultPath is the PATH an MCP server starts with, matching the one
// tools/ gives a command. A caller who needs another one names it in
// Command.Env, which wins: os/exec keeps the last value for a repeated key.
const defaultPath = "PATH=/usr/local/bin:/usr/bin:/bin"

// environment is what an MCP server starts with: PATH, HOME, and whatever the
// caller named. Never the process environment.
//
// This is the one decision in this package worth stating as a rule. The
// process environment is where a service keeps its API keys, database URLs
// and session secrets, and an MCP server is a third-party program that is
// about to be handed model-chosen arguments — so it is a program that can be
// asked to print things. tools/ already refuses to inherit for the commands
// it runs; the argument is stronger here, because that code is ours and this
// code is somebody else's.
//
// Naming a credential in Command.Env is not a workaround, it is the point: a
// secret the server needs is then visible where the server is configured,
// rather than reaching it invisibly from whatever happened to start the
// agent.
//
// The keys are sorted so that two identical configurations produce the same
// environment. Map order is not a detail worth leaving to chance in something
// a test has to assert on.
func environment(extra map[string]string) []string {
	env := make([]string, 0, len(extra)+2)
	env = append(env, defaultPath)
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	for _, key := range slices.Sorted(maps.Keys(extra)) {
		env = append(env, key+"="+extra[key])
	}
	return env
}
