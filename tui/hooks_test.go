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

// TestTrustingAHooksFileLoadsItAndAnEditRearms checks that trust is
// remembered per content hash: loading works after trusting, and one byte
// changed is a different file, whatever the path says.
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

	edited := guardYAML + "\n"
	err = os.WriteFile(filepath.Join(root, HooksFile), []byte(edited), 0o644)
	if err != nil {
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

// contractCase is one row of the process-contract table: a command, the
// point it is registered at, and what the tool call should produce.
type contractCase struct {
	name      string
	run       string
	point     nacelle.HookPoint
	wantIn    []string
	wantErr   string
	denyCalls bool
}

// contractCases covers the whole protocol: exit 0 with stdout injects, exit 2
// denies with stderr as the reason, any other failure fails closed with its
// output kept from the model.
func contractCases() []contractCase {
	return []contractCase{
		{
			name:   "exit zero injects stdout into the result",
			run:    `echo seen`,
			point:  nacelle.AfterToolCall,
			wantIn: []string{"found it", "seen"},
		},
		{
			name:      "exit two denies with stderr as the reason",
			run:       `echo not allowed >&2; exit 2`,
			point:     nacelle.BeforeToolCall,
			wantErr:   "not allowed",
			denyCalls: true,
		},
		{
			name:      "any other failure fails closed without its stderr reaching the model",
			run:       `exit 1`,
			point:     nacelle.BeforeToolCall,
			wantErr:   "failed",
			denyCalls: true,
		},
	}
}

func TestTheProcessContract(t *testing.T) {
	for _, tt := range contractCases() {
		t.Run(tt.name, func(t *testing.T) { runContractCase(t, tt) })
	}
}

// runContractCase executes one row and asserts its outcome.
func runContractCase(t *testing.T, tt contractCase) {
	t.Helper()
	hooks, err := buildHooks(hookConfig{{On: string(tt.point), Run: tt.run}})
	if err != nil {
		t.Fatalf("buildHooks: %v", err)
	}
	sink := &nacelle.ToolSink{Hooks: hooks}
	result, err := nacelle.RunTool(context.Background(), searchTool(t),
		nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)

	if tt.denyCalls {
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Fatalf("err = %v; want it to contain %q", err, tt.wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("RunTool: %v", err)
	}
	for _, want := range tt.wantIn {
		if !strings.Contains(result, want) {
			t.Errorf("result = %q; want it to contain %q", result, want)
		}
	}
}
