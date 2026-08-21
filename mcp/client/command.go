package client

import (
	"io"
	"time"
)

// DefaultCallTimeout bounds one tools/call, and the handshake that precedes
// the first one.
//
// It matches tools.DefaultCommandTimeout because it bounds the same kind of
// work: an MCP server that greps a tree or runs a build needs the room a
// local command needs. What the bound really buys is the failure it prevents.
// A server that accepts a call and never answers would otherwise hold the
// agent's goroutine for the life of the process, and the run reads as a model
// that stopped thinking rather than as a subprocess that stopped talking.
const DefaultCallTimeout = 2 * time.Minute

// Command is one MCP server, run as a subprocess and spoken to over its
// standard input and output.
type Command struct {
	// Name namespaces this server's tools and must be unique in one call
	// to Connect. Every tool is presented to the model as <Name>_<tool>,
	// always — a tool's name must not change depending on how many servers
	// happen to be configured, because a prompt that mentions a tool by
	// name would then be wrong for half the configurations.
	Name string

	// Path is the executable to run. Required.
	//
	// Prefer an absolute path. A bare name is resolved by exec.Command
	// against *this* process's PATH, at the moment the command is built,
	// and setting Env["PATH"] does not change that — os/exec resolves
	// before it ever looks at Env, so the child's PATH is what the server
	// then finds its own helpers on and nothing more. Naming the file in
	// full is the only spelling where the binary that is found and the
	// binary that was meant are certainly the same one.
	Path string

	// Args are handed to it unchanged.
	Args []string

	// Env names the variables the server starts with, on top of a minimal
	// base. Read the doc comment on environment for why the process
	// environment is not inherited and why every credential the server
	// needs belongs here.
	Env map[string]string

	// Dir is the working directory. Empty means the calling process's.
	Dir string

	// Stderr receives everything the server writes to its standard error,
	// which for most MCP servers is where their logging goes. Nil sends it
	// nowhere.
	//
	// Setting it is not what makes a failure to start readable — the first
	// few kilobytes are kept regardless and hung off the error Connect
	// returns, because an operator reading an error should not have to go
	// find a log to learn what it meant. This is for the rest: a server
	// that stays up for hours and has something to say about it.
	//
	// It is written from os/exec's own copier, so it must not block, and it
	// may still be written after Connect returns.
	Stderr io.Writer

	// AllowedTools restricts what the model may call on this server. Empty
	// allows every tool the server exposes.
	//
	// Worth setting for any server that can write. The narrowest list that
	// does the job is the one that survives the server growing a
	// destructive tool later without anyone here noticing.
	AllowedTools []string

	// Timeout bounds one tools/call and the connect-time handshake.
	// Defaults to DefaultCallTimeout.
	Timeout time.Duration
}
