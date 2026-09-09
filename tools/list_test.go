package tools

import (
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

func TestListDefaultsToTheWorkingDirectory(t *testing.T) {
	root := setDir(t)
	cases := []struct {
		in   string
		want string
	}{
		{"", filepath.Clean(root)},
		{"   ", filepath.Clean(root)},
		{"/", "/"},
		{"/src", "/src"},
		{"src/", filepath.Clean(root) + "/src/"},
		{"./src", filepath.Clean(root) + "/src/"},
	}
	for _, tc := range cases {
		got, err := cleanDir(tc.in, root, false)
		if err != nil {
			t.Fatalf("cleanDir(%q): %v", tc.in, err)
		}
		if filepath.Clean(got) != filepath.Clean(tc.want) {
			t.Errorf("cleanDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// setDir returns the absolute path of the temp directory used by newSet.
func setDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if len(dir) == 0 {
		t.Fatal("temp dir is empty")
	}
	return dir
}
