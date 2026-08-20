// Package tools is the local tool set: reading, writing, editing, searching
// and running commands inside one directory.
//
// # What is confined
//
// Every file operation goes through [os.Root], so a path that resolves outside
// the root is refused by the kernel-backed check rather than by string
// comparison. That closes the escapes a manual check tends to leave open: a
// symlink pointing out of the tree, a `..` that survives normalisation, an
// absolute path, and the window between checking a path and using it.
//
// # Where the root comes from
//
// Config.Root is set by the host when the tool set is built, and no tool takes
// a directory argument. That is deliberate and it is the one rule here worth
// stating as a rule: a model must never be able to nominate its own boundary.
// CVE-2025-59532 is what happens otherwise — Codex CLI accepted a
// model-generated working directory as the sandbox's writable root, so the
// output being confined was also the thing choosing the confinement. If you
// add a tool here, it does not get a `path` that escapes, a `cwd`, or a
// `root`.
//
// # What confinement is not
//
// It does not make this a sandbox, and the standard library says so plainly:
// os.Root does not prohibit traversal of filesystem boundaries, bind mounts,
// /proc, or device files. Bash is not confined at all — a command runs with
// every privilege the process has, and no denylist of dangerous commands
// survives contact with a shell that has a thousand ways to spell the same
// thing. If an agent runs untrusted output, the isolation has to be a
// container or a VM. This package gives you a working directory, not a jail,
// and Config.AllowBash is opt-in so that choice is made on purpose.
package tools

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/FacileStudio/nacelle"
)

// Defaults for the limits every tool applies.
const (
	// DefaultMaxOutputBytes caps what one tool call may return.
	//
	// Output goes into the context window and stays there for the rest of
	// the conversation, so an uncapped grep over a large tree does not just
	// produce a long message: it evicts the reason the search was run.
	DefaultMaxOutputBytes = 64 * 1024

	// DefaultMaxReadBytes caps a single file read, below the output cap so
	// a read that hit the limit still leaves room for the notice saying so.
	DefaultMaxReadBytes = 48 * 1024

	// DefaultCommandTimeout bounds one command. A command that outlives it
	// is killed with its children.
	DefaultCommandTimeout = 2 * time.Minute
)

// Config describes the tool set.
type Config struct {
	// Root is the directory every tool is confined to. Required.
	Root string

	// AllowBash mounts the command tool.
	//
	// Off by default, and deliberately not a limit that can be tuned into
	// safety: running commands is either something this agent may do or it
	// is not. See the package comment for what confinement does and does
	// not mean.
	AllowBash bool

	// CommandEnv is the environment commands run with.
	//
	// Nil means a minimal environment — PATH, HOME, and nothing else. The
	// process environment is not inherited, because it is where a service
	// keeps the credentials the model is not supposed to read, and `env` is
	// a command like any other.
	CommandEnv []string

	// CommandTimeout defaults to DefaultCommandTimeout.
	CommandTimeout time.Duration

	// MaxOutputBytes defaults to DefaultMaxOutputBytes.
	MaxOutputBytes int

	// MaxReadBytes defaults to DefaultMaxReadBytes.
	MaxReadBytes int
}

// Set is the tool set, bound to one directory.
type Set struct {
	root           *os.Root
	dir            string
	allowBash      bool
	commandEnv     []string
	commandTimeout time.Duration
	maxOutput      int
	maxRead        int

	// read records which files have been read this session, because Edit
	// refuses to touch a file the model has not looked at.
	read *readLog
}

// New opens the tool set on cfg.Root.
//
// The caller closes it with Close when the agent is done, which releases the
// directory handle os.Root keeps open.
func New(cfg Config) (*Set, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("nacelle/tools: a root directory is required")
	}
	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("nacelle/tools: opening the root: %w", err)
	}

	set := &Set{
		root:           root,
		dir:            cfg.Root,
		allowBash:      cfg.AllowBash,
		commandEnv:     cfg.CommandEnv,
		commandTimeout: cfg.CommandTimeout,
		maxOutput:      cfg.MaxOutputBytes,
		maxRead:        cfg.MaxReadBytes,
		read:           newReadLog(),
	}
	if set.commandEnv == nil {
		set.commandEnv = minimalEnv()
	}
	if set.commandTimeout == 0 {
		set.commandTimeout = DefaultCommandTimeout
	}
	if set.maxOutput == 0 {
		set.maxOutput = DefaultMaxOutputBytes
	}
	if set.maxRead == 0 {
		set.maxRead = DefaultMaxReadBytes
	}
	return set, nil
}

// Close releases the root directory handle.
func (s *Set) Close() error { return s.root.Close() }

// Dir is the directory the set is confined to.
func (s *Set) Dir() string { return s.dir }

// Tools is everything this set offers, honouring Config.AllowBash.
func (s *Set) Tools() ([]nacelle.Tool, error) {
	tools, err := s.ReadOnly()
	if err != nil {
		return nil, err
	}

	writing, err := buildAll(s.writeTool, s.editTool)
	if err != nil {
		return nil, err
	}
	tools = append(tools, writing...)

	if s.allowBash {
		command, err := s.commandTool()
		if err != nil {
			return nil, err
		}
		tools = append(tools, command)
	}
	return tools, nil
}

// ReadOnly is the subset that cannot change anything.
//
// Worth reaching for whenever an agent only has to answer questions: a tool
// that cannot write is the cheapest guarantee available, and it costs nothing
// to give an agent less.
func (s *Set) ReadOnly() ([]nacelle.Tool, error) {
	return buildAll(s.readTool, s.listTool, s.globTool, s.grepTool)
}

// buildAll collects tool constructors, failing on the first that cannot build
// its schema.
func buildAll(builders ...func() (nacelle.Tool, error)) ([]nacelle.Tool, error) {
	built := make([]nacelle.Tool, 0, len(builders))
	for _, build := range builders {
		tool, err := build()
		if err != nil {
			return nil, err
		}
		built = append(built, tool)
	}
	return built, nil
}

// minimalEnv is what a command inherits when the caller names nothing.
//
// PATH so that commands resolve, HOME because enough tools fail without one,
// and nothing else. Everything a service holds in its environment — API keys,
// database URLs, session secrets — is exactly what a model must not be able to
// print.
func minimalEnv() []string {
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

// truncate caps text at limit, saying so rather than trimming in silence.
//
// The notice matters more than the trimming: a model handed a quietly cut file
// will reason about it as if it were whole, and confidently describe the end
// of something it never saw.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	kept := text[:limit]
	if cut := strings.LastIndexByte(kept, '\n'); cut > limit/2 {
		kept = kept[:cut]
	}
	return kept + fmt.Sprintf("\n\n[truncated: %d of %d bytes shown]", len(kept), len(text))
}
