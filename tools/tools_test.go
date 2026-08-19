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
	t.Cleanup(func() {
		if err := set.Close(); err != nil {
			t.Errorf("closing the set: %v", err)
		}
	})
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

// The confinement is os.Root's, and this is the test that it is actually load
// bearing: a symlink pointing out of the tree is the escape a string-prefix
// check lets through, because the path looks fine right up until it is opened.
func TestASymlinkCannotReachOutsideTheRoot(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("password"), 0o600); err != nil {
		t.Fatalf("seeding the secret: %v", err)
	}

	set := newSet(t, map[string]string{"ok.txt": "fine"})
	if err := os.Symlink(secret, filepath.Join(set.Dir(), "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if got, err := call(t, set, "read_file", readInput{Path: "escape"}); err == nil {
		t.Fatalf("read a file outside the root through a symlink: %q", got)
	}
}

// A leading slash is normalised rather than refused: a model shown
// absolute-looking paths will send them back, and the confinement does not
// depend on the string having been tidy.
func TestTraversalAndAbsolutePathsStayInside(t *testing.T) {
	set := newSet(t, map[string]string{"in.txt": "inside"})

	for _, name := range []string{"../../etc/passwd", "../outside", "a/../../b"} {
		if _, err := call(t, set, "read_file", readInput{Path: name}); err == nil {
			t.Errorf("read %q, which is outside the root", name)
		}
	}

	if _, err := call(t, set, "read_file", readInput{Path: "/in.txt"}); err != nil {
		t.Errorf("a rooted path was refused: %v", err)
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

	out, err := call(t, set, "search_files", grepInput{Pattern: "needle"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "a.go:1") || !strings.Contains(out, "internal/deep/c.txt:1") {
		t.Errorf("search missed a real match:\n%s", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Errorf("search walked node_modules:\n%s", out)
	}

	scoped, err := call(t, set, "search_files", grepInput{Pattern: "needle", Glob: "**/*.txt"})
	if err != nil {
		t.Fatalf("scoped search: %v", err)
	}
	if strings.Contains(scoped, "a.go") {
		t.Errorf("the glob did not restrict the search:\n%s", scoped)
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
