package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// namedServers writes one mcpServers file and returns its path, so a test can
// say what it is about in the JSON rather than in setup around it.
func namedServers(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".mcp.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the MCP config: %v", err)
	}
	return path
}

// The launch nearly everybody gets. Nobody has written one of these files, so
// naming none has to cost nothing at all: no processes, no tools, and a Set
// that still closes rather than a nil to guard against on the way out.
func TestNoMCPConfigStartsNothingAndChangesNothing(t *testing.T) {
	local := []nacelle.Tool{}

	mcp, grown, err := mcpTools(defaults(), local)
	if err != nil {
		t.Fatalf("mcpTools: %v", err)
	}
	defer func() { _ = mcp.set.Close() }()

	if mcp.servers != 0 || mcp.tools != 0 {
		t.Errorf("connected = %+v, want nothing connected", mcp)
	}
	if len(grown) != 0 {
		t.Errorf("tools = %+v, want the local set handed straight back", grown)
	}
	if mcp.set == nil {
		t.Error("set = nil, want an empty Set the caller can close unconditionally")
	}
}

// Naming nothing must also leave the banner exactly as it was, because the
// people this affects are the ones who never asked about MCP and would have
// no idea what a line about it referred to.
func TestTheBannerSaysNothingAboutMCPWhenNoneIsConfigured(t *testing.T) {
	got := banner(&answeringStub{}, asSettled(Config{Root: "."}), loaded{}, connected{})

	if strings.Contains(got, "MCP") {
		t.Errorf("banner = %q, want no mention of MCP when none is configured", got)
	}
}

// A server named by hand and then missing is a run that ends, not a run with
// a quietly smaller tool set: the symptom of the second is a model saying it
// cannot do the thing, which reads as the model refusing rather than as a
// server that is down. The error has to name which one, since a file with
// nine of them looks the same either way.
func TestAServerThatWillNotStartEndsTheRun(t *testing.T) {
	config := defaults()
	config.MCP = []string{namedServers(t, `{"mcpServers": {"ledger": {"command": "/nonexistent/nacelle-mcp"}}}`)}

	_, _, err := mcpTools(config, nil)
	if err == nil {
		t.Fatal("a server that cannot start was accepted")
	}
	if !strings.Contains(err.Error(), "ledger") {
		t.Errorf("error = %v, want it to name the server that would not start", err)
	}
}

// The same refusal one step earlier, and the reason Load is strict inside a
// server entry: a file that cannot be read at all must not leave a launch
// looking like one that simply had no servers.
func TestAnUnreadableMCPFileEndsTheRun(t *testing.T) {
	config := defaults()
	config.MCP = []string{filepath.Join(t.TempDir(), "never-written.json")}

	if _, _, err := mcpTools(config, nil); err == nil {
		t.Fatal("a config file that was never there was accepted")
	}
}

// ~/.nacelle.yml goes through no shell, so a tilde written there would reach
// os.ReadFile verbatim and fail on a path nobody typed wrong. The order is
// checked in the same breath because it is what client.Load merges by: later
// files win, so reversing them would silently swap which server survives.
func TestConfiguredExpandsTildesAndKeepsTheOrderGiven(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := configured([]string{"~/.claude/.mcp.json", "/etc/team.mcp.json"})

	want := []string{filepath.Join(home, ".claude", ".mcp.json"), "/etc/team.mcp.json"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("configured = %v, want %v", got, want)
	}
}

// Both counts, because a server that starts and offers nothing is the failure
// this client cannot otherwise show: it is an error nowhere, and its only
// other trace is a model that never reaches for the tool.
func TestTheBannerReportsTheServersAndToolsItWasGiven(t *testing.T) {
	got := banner(&answeringStub{}, asSettled(Config{Root: "."}), loaded{}, connected{servers: 2, tools: 7})

	if !strings.Contains(got, "2 MCP servers, 7 tools") {
		t.Errorf("banner = %q, want the server and tool counts", got)
	}
	if alone := banner(&answeringStub{}, asSettled(Config{Root: "."}), loaded{},
		connected{servers: 1, tools: 1}); !strings.Contains(alone, "1 MCP server, 1 tool") {
		t.Errorf("banner = %q, want both counts in the singular", alone)
	}
}

// The other list setting, and the one that merges the other way. A -mcp typed
// for one run layers over the file's list rather than replacing it, and lands
// after it, because that is the order client.Load resolves a server named
// twice in: later wins. Reversed, a project's file would lose to the personal
// one it was meant to override.
func TestMCPFilesFromTheFileAndTheFlagCombineWithTheFlagLast(t *testing.T) {
	written(t, "mcp:\n  - /from/the/file.json\n")

	config, err := settings(Config{Sources: Sources{MCP: []string{"/from/the/flag.json"}}})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if want := []string{"/from/the/file.json", "/from/the/flag.json"}; !slices.Equal(config.MCP, want) {
		t.Errorf("mcp = %v, want %v", config.MCP, want)
	}
}

// Naming no server anywhere has to leave the setting empty rather than
// holding a path nobody wrote, since an empty list is what makes the whole
// feature a no-op for everyone who has not asked for it.
func TestNoMCPKeyAnywhereLeavesTheListEmpty(t *testing.T) {
	written(t, "backend: openrouter\n")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if len(config.MCP) != 0 {
		t.Errorf("mcp = %v, want nothing configured", config.MCP)
	}
}
