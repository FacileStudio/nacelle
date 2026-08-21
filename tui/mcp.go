package main

import (
	"context"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/mcp/client"
)

// connected is the live session to every MCP server this launch was told
// about, and the two counts the banner reports back.
//
// The counts are carried rather than derived because only one of them can be:
// a Set knows how many tools it bridged and not how many servers produced
// them, and "2 MCP servers, 0 tools" is exactly the launch somebody needs to
// see spelled out. Holding both also lets the banner be tested without a
// subprocess anywhere near it.
type connected struct {
	set     *client.Set
	servers int
	tools   int
}

// mcpTools starts every MCP server the settings name and hands local back with
// their tools appended. The caller owns the session and must close it — each
// server is a process nothing else will reap.
//
// The local tool set goes in and comes out grown, rather than the bridged half
// coming back on its own for the caller to append. It is the same reason
// declareWeb is a function of its own: run() sits at the statement budget the
// gate allows, and an append on a line there costs one that this costs nothing.
//
// Naming none is a working no-op and is meant to stay one. Load over no paths
// finds no servers, Connect over no servers starts nothing and still hands
// back a Set that closes cleanly, so the great majority of people — who have
// never written one of these files — get the launch they had before this
// existed, down to the banner.
//
// A server that will not start ends the run, which is the opposite of how
// skills and project context fail. Those are discovered, so finding nothing is
// indistinguishable from there being nothing to find; this was asked for by
// name, and a tool that is quietly missing reads as a model refusing to work
// rather than as a server that is down. client.Connect refuses to degrade to
// the servers that did come up for the same reason, one level further in.
//
// Both errors go back unwrapped, the call webTools makes about WebSearch and
// for the same reason: each already begins with nacelle/mcp/client and names
// the file or the server it is about, so a "connecting to the MCP servers" in
// front of it says nothing the reader has not just read.
//
// context.Background is deliberate rather than lazy. Connect bounds its own
// handshake per server, and the sessions have to outlive this call by the
// entire length of the run — a context cancelled on the way out of here would
// take every server down with it before the model was asked anything.
//
// Nothing here goes looking for a .mcp.json under root, and that absence is
// the decision rather than an oversight. Such a file names executables to run,
// so honouring one found on the way past would start a stranger's process on
// the strength of having cd'd into their repository. That is strictly worse
// than the project-local skills this client already gates behind
// ~/.nacelle/trust.json and -trust-skills: a skill is text the model may
// decline to act on, and this is a subprocess started before the model is
// asked anything at all. Doing it safely means that same trust gate, which is
// a feature of its own rather than a line in this function.
func mcpTools(config Config, local []nacelle.Tool) (connected, []nacelle.Tool, error) {
	servers, err := client.Load(configured(config.MCP)...)
	if err != nil {
		return connected{}, nil, err
	}

	set, err := client.Connect(context.Background(), servers...)
	if err != nil {
		return connected{}, nil, err
	}
	bridged := set.Tools()
	return connected{set: set, servers: len(servers), tools: len(bridged)}, append(local, bridged...), nil
}

// configured is every named file with a leading "~" resolved, which is the
// same thing extraSkills does to -skill-dir's directories and is needed here
// for the same reason expandHome's own doc comment gives: the flag's argument
// arrives already expanded by the shell, and ~/.nacelle.yml's goes through no
// shell at all, so "~/.claude/.mcp.json" would otherwise work written one way
// and silently not the other.
func configured(paths []string) []string {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		resolved = append(resolved, expandHome(path))
	}
	return resolved
}
