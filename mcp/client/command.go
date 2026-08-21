package client

import (
	"fmt"
	"io"
	"os/exec"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

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

func (c Command) details() details {
	return details{name: c.Name, allowed: c.AllowedTools, timeout: timeoutOr(c.Timeout)}
}

// check refuses a subprocess that cannot be started.
func (c Command) check() error {
	if c.Path == "" {
		return fmt.Errorf("nacelle/mcp/client: server %q has no executable", c.Name)
	}
	return nil
}

// dial builds the subprocess and the buffer that keeps what it says on the
// way out, so that a server which fails to start can explain itself.
//
// exec.Command and not exec.CommandContext: the context here bounds the
// handshake, and binding the process to it would kill every server the
// moment Connect returned.
func (c Command) dial() (sdk.Transport, *diagnostics, error) {
	subprocess := exec.Command(c.Path, c.Args...)
	subprocess.Dir = c.Dir
	subprocess.Env = environment(c.Env)

	notes := &diagnostics{}
	subprocess.Stderr = notes.tee(c.Stderr)
	return &sdk.CommandTransport{Command: subprocess}, notes, nil
}
