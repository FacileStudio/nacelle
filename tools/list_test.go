package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// The gap this tool closes: find_files walks files only, so no glob it accepts
// ever reports that a directory is there, and a model had to infer one from a
// path that happened to run through it. Asserting the whole rendering covers
// the four things that matter at once — directories appear, they are marked,
// the listing stops at one level, and no argument means the working directory.
func TestListShowsOneLevelWithItsDirectories(t *testing.T) {
	set := newSet(t, map[string]string{
		"z.md":           "",
		"b.txt":          "",
		"apps/deep/x.go": "",
	})

	out, err := call(t, set, "list_directory", listInput{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out != "apps/\nb.txt\nz.md" {
		t.Errorf("listing = %q, want the directory marked, sorted, and no descent into it", out)
	}

	nested, err := call(t, set, "list_directory", listInput{Path: "apps"})
	if err != nil {
		t.Fatalf("nested list: %v", err)
	}
	if nested != "deep/" {
		t.Errorf("listing of apps = %q, want %q", nested, "deep/")
	}
}

// A listing that disagrees with find_files about what exists teaches the model
// something false: it would search the node_modules/ it was shown, get nothing
// back, and conclude the directory is empty. Dotfiles are the other half — walk
// skips dot directories only, and .gitignore is searchable and often the point.
func TestListSkipsWhatASearchWouldSkip(t *testing.T) {
	set := newSet(t, map[string]string{
		".gitignore":          "",
		".git/config":         "",
		"node_modules/p/i.js": "",
		"vendor/x/y.go":       "",
		"src/main.go":         "",
	})

	out, err := call(t, set, "list_directory", listInput{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out != ".gitignore\nsrc/" {
		t.Errorf("listing = %q, want the generated and dot directories dropped and the dotfile kept", out)
	}
}

// The boundary is os.Root's and it has to hold for a listing exactly as it does
// for a read. Both halves are here because they fail differently: a ".." is
// what a string check catches, and a symlink out of the tree is what it never
// sees — the path looks fine right up until something opens it.
func TestListCannotEscapeTheRoot(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("password"), 0o600); err != nil {
		t.Fatalf("seeding the secret: %v", err)
	}

	set := newSet(t, map[string]string{"in.txt": ""})
	for _, name := range []string{"..", "../..", "../" + filepath.Base(outside), "a/../../b"} {
		if got, err := call(t, set, "list_directory", listInput{Path: name}); err == nil {
			t.Errorf("listed %q, which is outside the root: %q", name, got)
		}
	}

	if err := os.Symlink(outside, filepath.Join(set.Dir(), "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got, err := call(t, set, "list_directory", listInput{Path: "escape"}); err == nil {
		t.Fatalf("listed a directory outside the root through a symlink: %q", got)
	}
}

// The no-argument call is the one this tool exists for, and clean() refuses
// precisely that path: to a file reader "." is the root directory, not a file.
func TestListDefaultsToTheWorkingDirectory(t *testing.T) {
	cases := map[string]string{
		"":      ".",
		"   ":   ".",
		"/":     ".",
		"/src":  "src",
		"src/":  "src",
		"./src": "src",
	}
	for in, want := range cases {
		if got := cleanDir(in, "."); got != want {
			t.Errorf("cleanDir(%q) = %q, want %q", in, got, want)
		}
	}
}
