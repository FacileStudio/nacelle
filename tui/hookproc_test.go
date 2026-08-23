package main

import "testing"

// A tilde is expanded when it starts the command or a path-like prefix of
// it; anywhere else it is shell syntax this package does not touch.
func TestExpandTilde(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cases := map[string]string{
		"~/bin/hook.sh":     "/home/tester/bin/hook.sh",
		"~":                 "/home/tester",
		"/absolute/hook.sh": "/absolute/hook.sh",
		"echo ~notahome":    "echo ~notahome",
	}
	for in, want := range cases {
		if got := expandTilde(in); got != want {
			t.Errorf("expandTilde(%q) = %q, want %q", in, got, want)
		}
	}
}
