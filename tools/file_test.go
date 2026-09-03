package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()

	cases := map[string]struct {
		input    string
		expected string
	}{
		"no tilde":          {"relative/path", "relative/path"},
		"just tilde":        {"~", home},
		"tilde slash":       {"~/foo", filepath.Join(home, "foo")},
		"tilde deep":        {"~/foo/bar", filepath.Join(home, "foo", "bar")},
		"tilde user":        {"~user/foo", "~user/foo"}, // not supported
		"absolute path":     {"/absolute/path", "/absolute/path"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := expandHome(tc.input)
			if result != tc.expected {
				t.Errorf("expandHome(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestCleanWithHomeExpansion(t *testing.T) {
	home, _ := os.UserHomeDir()
	root := "/tmp/workdir"

	cases := map[string]struct {
		input    string
		expected string
		wantErr  bool
	}{
		"relative path":           {"foo/bar", "foo/bar", false},
		"absolute inside root":    {"/tmp/workdir/foo", "foo", false},
		// absolute outside root: falls back to stripping leading slash
		"absolute outside root":   {"/etc/passwd", "etc/passwd", false},
		// tilde paths are expanded then treated as absolute outside root
		// so they get leading slash stripped and os.Root rejects them
		"tilde":                   {"~", strings.TrimPrefix(home, "/"), false},
		"tilde inside root":       {"~/workdir/foo", strings.TrimPrefix(filepath.Join(home, "workdir", "foo"), "/"), false},
		"tilde user":              {"~user/foo", "~user/foo", false},
		"empty":                   {"", "", true},
		"root directory":          {".", "", true},
		"absolute root directory": {"/", "", true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := clean(tc.input, root)
			if (err != nil) != tc.wantErr {
				t.Errorf("clean(%q) error = %v, wantErr = %v", tc.input, err, tc.wantErr)
				return
			}
			if !tc.wantErr && result != tc.expected {
				t.Errorf("clean(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}