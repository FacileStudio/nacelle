package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// searchTool builds a minimal tool the contract test can run.
func searchTool(t *testing.T) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewTool("search", "d", func(_ context.Context, in struct {
		Query string `json:"query"`
	}) (string, error) {
		return "found it", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	return tool
}

// writeHooks drops a hooks file into a fake project root and returns the root.
func writeHooks(t *testing.T, yaml string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".nacelle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, HooksFile), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const guardYAML = `hooks:
  - on: before_tool_call
    match: [run_command]
    run: test "$1" = "$1"
`

func TestAProjectHooksFileIsRefusedUntilTrusted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := writeHooks(t, guardYAML)

	hooks, notice, err := loadProjectHooks(root, false)
	if err != nil {
		t.Fatalf("loadProjectHooks: %v", err)
	}
	if len(hooks) != 0 {
		t.Fatalf("untrusted file loaded %d hook points", len(hooks))
	}
	if !strings.Contains(notice, "-trust-hooks") {
		t.Errorf("notice = %q; want it to name the flag that fixes it", notice)
	}
}

func TestTrustingAHooksFileLoadsItAndAnEditRearms(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := writeHooks(t, guardYAML)

	if _, notice, err := loadProjectHooks(root, true); err != nil || notice != "" {
		t.Fatalf("first trusted load: %v, notice %q", err, notice)
	}
	hooks, notice, err := loadProjectHooks(root, false)
	if err != nil || notice != "" {
		t.Fatalf("second load: %v, notice %q; trust should have been remembered", err, notice)
	}
	if len(hooks[nacelle.BeforeToolCall]) != 1 {
		t.Fatalf("loaded %d before-hooks, want 1", len(hooks[nacelle.BeforeToolCall]))
	}

	// One byte changed is a different file, whatever the path says.
	if err := os.WriteFile(filepath.Join(root, HooksFile), []byte(guardYAML+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, notice, _ := loadProjectHooks(root, false); notice == "" {
		t.Fatal("an edited hooks file stayed trusted")
	}
}

func TestUnknownEventOrEmptyCommandIsRefusedAtLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for name, yaml := range map[string]string{
		"event": `hooks:
  - on: on_stop
    run: echo hi
`,
		"command": `hooks:
  - on: after_tool_call
`,
	} {
		root := writeHooks(t, yaml)
		if _, _, err := loadProjectHooks(root, true); err == nil {
			t.Errorf("%s spec was accepted", name)
		}
	}
}

// The whole contract in one round trip: exit 0 with stdout injects, exit 2
// denies with stderr as the reason, another failure denies but keeps its
// output away from the model.
func TestTheProcessContract(t *testing.T) {
	build := func(t *testing.T, run string) *nacelle.ToolSink {
		t.Helper()
		hooks, err := buildHooks(hookConfig{{On: "after_tool_call", Run: run}})
		if err != nil {
			t.Fatalf("buildHooks: %v", err)
		}
		return &nacelle.ToolSink{Hooks: hooks}
	}
	run := func(t *testing.T, sink *nacelle.ToolSink) string {
		t.Helper()
		result, err := nacelle.RunTool(context.Background(), searchTool(t),
			nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
		if err != nil {
			t.Fatalf("RunTool: %v", err)
		}
		return result
	}

	if got := run(t, build(t, `echo seen`)); !strings.Contains(got, "found it") || !strings.Contains(got, "seen") {
		t.Errorf("exit 0 result = %q; want tool output plus the hook's stdout", got)
	}

	deny := build(t, `echo not allowed >&2; exit 2`)
	deny.Hooks = map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: deny.Hooks[nacelle.AfterToolCall],
	}
	_, err := nacelle.RunTool(context.Background(), searchTool(t),
		nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), deny)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("exit 2 err = %v; want stderr as the denial reason", err)
	}

	crash := build(t, `exit 1`)
	crash.Hooks = map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: crash.Hooks[nacelle.AfterToolCall],
	}
	_, err = nacelle.RunTool(context.Background(), searchTool(t),
		nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), crash)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("exit 1 err = %v; want a fail-closed denial", err)
	}
}
