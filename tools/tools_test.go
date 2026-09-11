package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// newSet opens a tool set over a temporary tree seeded with files.
func newSet(t *testing.T, files map[string]string) *Set {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	set, err := New(Config{Root: dir, AllowBash: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { set.Close() })
	return set
}

// call runs one tool by name with the given arguments.
func call(t *testing.T, set *Set, name string, args any) (string, error) {
	t.Helper()
	all, err := set.Tools()
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	tool, ok := nacelle.ToolsByName(all)[name]
	if !ok {
		t.Fatalf("no tool named %q", name)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encoding arguments: %v", err)
	}
	return tool.Run(context.Background(), encoded)
}

// The model can use absolute paths and symlinks, which now resolve through the
// regular filesystem rather than being confined by os.Root. Relative paths are
// resolved against the set's working directory, so ".." escapes are possible
// from here too.
func TestAbsolutePathsAndSymlinksWork(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("password"), 0o600); err != nil {
		t.Fatalf("seeding the secret: %v", err)
	}

	set := newSet(t, map[string]string{"ok.txt": "fine"})
	if err := os.Symlink(secret, filepath.Join(set.Dir(), "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if got, err := call(t, set, "read_file", readInput{Path: "escape"}); err != nil {
		t.Fatalf("symlink read failed: %v", err)
	} else if !strings.Contains(got, "password") {
		t.Errorf("symlink read = %q, want it to contain the target file's contents", got)
	}
}

// Relative paths resolve against the working directory, so a model can reach
// outside it with "..". Absolute paths are resolved as-is, and the model sees
// paths that look absolute from banner/environment.
func TestRelativeAndAbsolutePathsResolve(t *testing.T) {
	set := newSet(t, map[string]string{"in.txt": "inside"})

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside"), 0o600); err != nil {
		t.Fatalf("seeding outside file: %v", err)
	}
	rel := "../" + filepath.Base(outside) + "/secret.txt"
	if _, err := call(t, set, "read_file", readInput{Path: rel}); err != nil {
		t.Fatalf("relative escape failed: %v", err)
	}

	abs := filepath.Join(set.Dir(), "in.txt")
	if got, err := call(t, set, "read_file", readInput{Path: abs}); err != nil || !strings.Contains(got, "inside") {
		t.Fatalf("absolute path failed: %v, %v", err, got)
	}
}

func TestGlobMatchesAcrossDirectories(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.go", "a/b/c.go", true},
		{"**/*.go", "c.go", true},
		{"*.go", "a/c.go", false},
		{"cmd/**", "cmd/x/y.txt", true},
		{"cmd/**", "internal/x.txt", false},
		{"**/*_test.go", "internal/deep/a_test.go", true},
		{"**/*_test.go", "internal/deep/a.go", false},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/d/c", false},
	}
	for _, tc := range cases {
		if got := matchGlob(tc.pattern, tc.name); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestSearchFindsMatchesAndSkipsGeneratedTrees(t *testing.T) {
	set := newSet(t, map[string]string{
		"a.go":                "package main // needle\n",
		"node_modules/b.go":   "// needle in generated code\n",
		"internal/deep/c.txt": "needle here too\n",
	})

	out, err := call(t, set, "search_content", grepInput{Pattern: "needle"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "a.go:1") || !strings.Contains(out, "internal/deep/c.txt:1") {
		t.Errorf("search missed a real match:\n%s", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Errorf("search walked node_modules:\n%s", out)
	}

	scoped, err := call(t, set, "search_content", grepInput{Pattern: "needle", Glob: "**/*.txt"})
	if err != nil {
		t.Fatalf("scoped search: %v", err)
	}
	if strings.Contains(scoped, "a.go") {
		t.Errorf("the glob did not restrict the search:\n%s", scoped)
	}

	relative, err := call(t, set, "search_content", grepInput{Pattern: "needle", Glob: "internal/deep/*.txt"})
	if err != nil {
		t.Fatalf("relative-glob search: %v", err)
	}
	if !strings.Contains(relative, "c.txt") {
		t.Errorf("a relative glob must match the walk's absolute names:\n%s", relative)
	}
}

func TestFindFilesMatchesRelativeGlob(t *testing.T) {
	set := newSet(t, map[string]string{"internal/deep/c.txt": "x"})
	out, err := call(t, set, "find_files", globInput{Pattern: "internal/deep/*.txt"})
	if err != nil {
		t.Fatalf("find_files: %v", err)
	}
	if !strings.Contains(out, "c.txt") {
		t.Errorf("a relative glob must match the walk's absolute names:\n%s", out)
	}
}

func TestOutputIsCappedAndSaysSo(t *testing.T) {
	capped := truncate(strings.Repeat("line of text\n", 10000), 200)
	if len(capped) > 400 {
		t.Errorf("truncate returned %d bytes, want it near the limit", len(capped))
	}
	if !strings.Contains(capped, "truncated") {
		t.Error("truncation was silent; a model will describe an end it never saw")
	}
}

// No tool may take a directory argument.
//
// CVE-2025-59532 is the reason this is a test and not a convention: Codex CLI
// accepted a model-generated working directory as its sandbox root, so the
// output being confined was also the thing choosing the confinement. The root
// comes from the host at construction, and a future tool that adds a `cwd` for
// convenience fails here.
func TestNoToolLetsTheModelChooseItsOwnRoot(t *testing.T) {
	set := newSet(t, nil)
	all, err := set.Tools()
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	forbidden := map[string]bool{"root": true, "cwd": true, "dir": true, "directory": true, "workspace": true}
	for _, tool := range all {
		properties, ok := tool.Schema()["properties"].(map[string]any)
		if !ok {
			continue
		}
		for field := range properties {
			if forbidden[strings.ToLower(field)] {
				t.Errorf("tool %q accepts a %q argument; the boundary must come from the host", tool.Name(), field)
			}
		}
	}
}

func TestLocalToolsReadOnlyDeclarations(t *testing.T) {
	set := newSet(t, nil)
	all, err := set.Tools()
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	toolsMap := nacelle.ToolsByName(all)

	for _, name := range []string{"read_file", "list_directory", "find_files", "search_content"} {
		tool, ok := toolsMap[name]
		if !ok {
			t.Fatalf("missing expected tool %q", name)
		}
		ro, ok := tool.(nacelle.ReadOnlyTool)
		if !ok || !ro.IsReadOnly() {
			t.Errorf("tool %q expected read-only", name)
		}
	}

	for _, name := range []string{"write_file", "edit_file", "run_command"} {
		tool, ok := toolsMap[name]
		if !ok {
			t.Fatalf("missing expected tool %q", name)
		}
		if ro, ok := tool.(nacelle.ReadOnlyTool); ok && ro.IsReadOnly() {
			t.Errorf("tool %q expected mutating", name)
		}
	}
}

func TestWebToolsReadOnlyDeclarations(t *testing.T) {
	fetchTools, err := WebFetch()
	if err != nil || len(fetchTools) != 1 {
		t.Fatalf("WebFetch: %v, len %d", err, len(fetchTools))
	}
	if ro, ok := fetchTools[0].(nacelle.ReadOnlyTool); !ok || !ro.IsReadOnly() {
		t.Errorf("web_fetch does not satisfy ReadOnlyTool")
	}
}
