package client

import (
	"maps"
	"os"
	"slices"
)

// implementationVersion is what a server is told it is talking to.
//
// A constant rather than a build stamp, because MCP treats the field as
// display metadata: a server logs it, nothing negotiates on it. It is bumped
// by hand with the tag rather than derived from one — debug.ReadBuildInfo
// reports the version of the *consumer's* module for a library, not this one,
// so deriving it would report whatever imported us.
const implementationVersion = "0.1.0"

// defaultPath is the PATH an isolated MCP server starts with, matching the one
// tools/ gives a command. A caller who needs another one names it in
// Command.Env, which wins: os/exec keeps the last value for a repeated key.
const defaultPath = "PATH=/usr/local/bin:/usr/bin:/bin"

// environment is what an MCP server starts with. By default it inherits this
// process's environment in full — the agent was launched from a shell whose
// PATH, locale and toolchain matter to the servers it starts, and a server
// that cannot find its helpers on PATH fails in ways no operator can debug
// from "no engine returned results".
//
// Command.Env entries are appended last in either mode, so a caller's value
// always overrides what was inherited: os/exec keeps the last value for a
// repeated key. That is also how a caller narrows an inherited PATH.
//
// With Command.IsolateEnv set, the child gets PATH, HOME and only what the
// caller named. That is the defensive posture for a third-party server that
// is about to be handed model-chosen arguments and can be asked to print
// things — the process environment is where a service keeps its API keys,
// database URLs and session secrets. Tools that find nothing to reach for
// read as a broken server rather than a guarded one, which is the honest
// trade a person makes when they name a secret in Command.Env instead.
//
// The keys are sorted so that two identical configurations produce the same
// environment. Map order is not a detail worth leaving to chance in something
// a test has to assert on.
func environment(extra map[string]string, isolate bool) []string {
	if isolate {
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

	env := os.Environ()
	for _, key := range slices.Sorted(maps.Keys(extra)) {
		env = append(env, key+"="+extra[key])
	}
	return env
}
