package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/FacileStudio/nacelle"
)

// Jardin builds tools that give the model access to this machine's jardin
// installation: its recorded flows and its memory search, both narrower and
// more legible than reaching the same commands through run_command.
//
// "Every harness hands the model run_command and hopes" is the reason these
// exist as tools of their own rather than as instructions to shell out: a
// scoped run_flow(name) with an enum-shaped argument is a smaller blast
// radius than freeform bash, works even when Config.AllowBash is off, and a
// flow — unlike an improvised shell pipeline — is deterministic and already
// gated by a human trust decision on this machine, not by anything this
// package adds.
//
// Absent jardin is not an error. Most consumers of this package will not
// have it installed, and a constructor that failed for them would make every
// other tool file conditional on one machine's local tooling. No binary on
// PATH means no tools and no error — exactly like AllowBash being off means
// no command tool.
func Jardin() ([]nacelle.Tool, error) {
	if _, err := exec.LookPath("jardin"); err != nil {
		return nil, nil //nolint:nilerr // absent is the expected case, not a failure; see the doc comment
	}
	return buildAll(listFlowsTool, runFlowTool, searchMemoryTool)
}

func listFlowsTool() (nacelle.Tool, error) {
	return nacelle.NewTool("list_flows",
		"List the jardin flows recorded on this machine, with what each one does and whether it is trusted to run yet. Call this before run_flow if you do not already know the exact flow name.",
		func(ctx context.Context, _ struct{}) (string, error) {
			out, err := jardin(ctx, "flow", "list")
			return report(out, err, DefaultMaxOutputBytes), nil
		})
}

type runFlowInput struct {
	Name string `json:"name" jsonschema:"required,description=The exact name of an existing flow, as listed by list_flows. This runs a named flow that already exists; it cannot create or edit one"`
}

func runFlowTool() (nacelle.Tool, error) {
	return nacelle.NewTool("run_flow",
		"Run a named jardin flow: a recorded, deterministic procedure, in place of an improvised shell pipeline. A flow nobody has reviewed on this machine yet refuses to run and says so — that is jardin's own trust gate, not something to work around; tell the person running you if you hit it, since only a human can run `jardin flow trust <name>` to clear it.",
		func(ctx context.Context, in runFlowInput) (string, error) {
			if strings.TrimSpace(in.Name) == "" {
				return "", fmt.Errorf("no flow name given")
			}
			out, err := jardin(ctx, "flow", "run", in.Name)
			return report(out, err, DefaultMaxOutputBytes), nil
		})
}

type searchMemoryInput struct {
	Query string `json:"query" jsonschema:"required,description=What to look for in the jardin wiki, in plain words"`
}

func searchMemoryTool() (nacelle.Tool, error) {
	return nacelle.NewTool("search_memory",
		"Search jardin's memory wiki — durable, project-specific notes an earlier session recorded: known bugs, tool gotchas, conventions, past decisions. Best match first. Prefer this over guessing when a project might already have a documented answer.",
		func(ctx context.Context, in searchMemoryInput) (string, error) {
			if strings.TrimSpace(in.Query) == "" {
				return "", fmt.Errorf("no query given")
			}
			out, err := jardin(ctx, "memory", "search", in.Query, "--limit", "10")
			return report(out, err, DefaultMaxOutputBytes), nil
		})
}

// jardin runs one jardin subcommand and returns its combined output.
//
// argv form, not a shell string: the model's own input never reaches a
// shell, so there is nothing here for it to inject regardless of what it
// puts in a flow name or a search query.
func jardin(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "jardin", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}
