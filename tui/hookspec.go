package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/FacileStudio/nacelle"
	"go.yaml.in/yaml/v4"
)

// HookSpec is one entry under a config's `hooks:` key.
//
// Run is a command line handed to sh -c, not an argv: every entry in here is
// written by hand in YAML, and the escape hatch of shell syntax beats
// inventing a list-splitting rule that is wrong for somebody. The event JSON
// arrives on stdin, so anything complex can be a standalone script — which
// also keeps it portable to other harnesses speaking the same protocol.
type HookSpec struct {
	On      string   `yaml:"on"`
	Match   []string `yaml:"match"`
	Run     string   `yaml:"run"`
	Timeout string   `yaml:"timeout"`
	Async   bool     `yaml:"async"`
}

// hookConfig is what decodes from a `hooks:` key. A pointer-free slice is
// fine here where the toggles needed pointers: merge accumulates hooks, so
// "not mentioned" and "empty" resolve the same way, unlike a bool where they
// are different answers.
type hookConfig []HookSpec

// point maps the YAML spelling of an event onto the library's.
var point = map[string]nacelle.HookPoint{
	string(nacelle.BeforeToolCall): nacelle.BeforeToolCall,
	string(nacelle.AfterToolCall):  nacelle.AfterToolCall,
}

// validate refuses a spec that could only misbehave at 2am: an event nobody
// fires, a command nobody sees, or a timeout that parses as nothing.
func (s HookSpec) validate() error {
	if _, known := point[s.On]; !known {
		return fmt.Errorf("hook %q: unknown event %q, want before_tool_call or after_tool_call", s.Run, s.On)
	}
	if strings.TrimSpace(s.Run) == "" {
		return fmt.Errorf("a hook on %s has no command to run", s.On)
	}
	if s.Timeout != "" {
		if _, err := time.ParseDuration(s.Timeout); err != nil {
			return fmt.Errorf("hook %q: timeout %q is not a duration: %w", s.Run, s.Timeout, err)
		}
	}
	return nil
}

// duration is the spec's timeout with its default filled in. Five seconds:
// a hook sits between the model asking and the tool running, so a generous
// default multiplies into minutes over a long session.
func (s HookSpec) duration() time.Duration {
	if s.Timeout == "" {
		return 5 * time.Second
	}
	d, _ := time.ParseDuration(s.Timeout)
	return d
}

// matches reports whether a spec asked to hear about this tool. An empty
// match list hears about everything; names elsewhere are exact, because a
// regex in YAML quoting rules has bitten every tool that shipped one
// (Claude Code's own matcher gotcha) and no hook here needs it yet.
func (s HookSpec) matches(tool string) bool {
	if len(s.Match) == 0 {
		return true
	}
	for _, name := range s.Match {
		if name == tool {
			return true
		}
	}
	return false
}

// parseHooks decodes one hooks file. KnownFields applies for the reason
// config.load already wrote down: an unrecognised key is a typo found in one
// second instead of a hook that never fires. An empty file is the same
// ordinary "no hooks" answer an empty config file is — io.EOF, not an error.
func parseHooks(raw []byte) (hookConfig, error) {
	var file struct {
		Hooks hookConfig `yaml:"hooks"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return file.Hooks, nil
}
