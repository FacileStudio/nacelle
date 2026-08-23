package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/FacileStudio/nacelle"
	"go.yaml.in/yaml/v4"
)

// HooksFile is the project-level hooks file, read in addition to the
// `hooks:` entries in the user's own config. That one is trusted by
// definition — it is already a file of commands this person chose to run —
// but a file that ships inside a project runs arbitrary code on load, and
// gets the same first-sight question project skills get.
const HooksFile = ".nacelle/hooks.yml"

// HookTrustFile records, per absolute path, the hash of the last hooks file
// trusted from there. It sits beside trust.json on purpose: skills are
// trusted by directory, hooks by content, and one record type each is
// clearer than one store pretending to do both.
const HookTrustFile = "hooks.json"

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

// buildHooks turns config entries into live library hooks, refusing any
// spec that would otherwise fail silently mid-session. Errors name the
// offending command, because "one bad line refused my whole agent" is only
// tolerable when it says which line.
func buildHooks(specs hookConfig) (map[nacelle.HookPoint][]nacelle.Hook, error) {
	var hooks map[nacelle.HookPoint][]nacelle.Hook
	for _, spec := range specs {
		if err := spec.validate(); err != nil {
			return nil, err
		}
		hook := nacelle.WithTimeout(spec.duration(), execHook(spec))
		if spec.Async {
			hook = nacelle.Async(hook)
		}
		p := point[spec.On]
		hooks = appendHook(hooks, p, hook)
	}
	return hooks, nil
}

func appendHook(hooks map[nacelle.HookPoint][]nacelle.Hook, p nacelle.HookPoint, hook nacelle.Hook) map[nacelle.HookPoint][]nacelle.Hook {
	if hooks == nil {
		hooks = map[nacelle.HookPoint][]nacelle.Hook{}
	}
	hooks[p] = append(hooks[p], hook)
	return hooks
}

// hookPayload is the process contract's input: one JSON object on stdin,
// nothing else. Field names follow Claude Code's, because hooks written for
// one harness reading .tool / .input should read the same names here.
type hookPayload struct {
	Event  string `json:"event"`
	Tool   string `json:"tool"`
	Input  string `json:"input"`
	Result string `json:"result,omitempty"`
	Retry  bool   `json:"retry"`
}

// execHook builds the hook that runs one spec's command and translates its
// exit back into a decision:
//
//	exit 0        allow; stdout becomes injected context
//	exit 2        deny; stderr is the reason the model reads
//	other failure deny; stderr goes to this process's stderr instead,
//	              because a crash is a bug report for the human, not
//	              instruction-shaped text for the model
func execHook(spec HookSpec) nacelle.Hook {
	return func(ctx context.Context, ev nacelle.HookEvent) nacelle.HookResult {
		if !spec.matches(ev.Tool) {
			return nacelle.HookResult{}
		}

		payload, err := json.Marshal(hookPayload{
			Event: string(ev.Point), Tool: ev.Tool,
			Input: ev.Input, Result: ev.Result, Retry: ev.Retry,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "nacelle hooks: encoding event for %q: %v\n", spec.Run, err)
			return nacelle.HookResult{}
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", spec.Run)
		cmd.Stdin = bytes.NewReader(payload)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut

		runErr := cmd.Run()
		switch {
		case runErr == nil:
			if ev.Point == nacelle.AfterToolCall && out.Len() > 0 {
				return nacelle.HookResult{Inject: strings.TrimRight(out.String(), "\n")}
			}
			return nacelle.HookResult{}
		case exitCode(runErr) == 2:
			reason := strings.TrimSpace(errOut.String())
			if reason == "" {
				reason = "denied by hook without a reason"
			}
			return nacelle.HookResult{Deny: reason}
		default:
			fmt.Fprintf(os.Stderr, "nacelle hooks: %q failed: %v: %s\n",
				spec.Run, runErr, strings.TrimSpace(errOut.String()))
			return nacelle.HookResult{Deny: fmt.Sprintf("hook watching %q failed", ev.Tool)}
		}
	}
}

// exitCode recovers a command's status without importing syscall for it.
func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// sessionHooks resolves every hooks layer in one place: the user's own
// config entries, always trusted by origin, then the project file through
// its trust gate. The returned notice is what the untrusted case wants said
// — the caller renders it in the transcript, where a refusal buried in a
// launch error would read as breakage rather than as a decision waiting.
func sessionHooks(config Config) (map[nacelle.HookPoint][]nacelle.Hook, string, error) {
	hooks, err := buildHooks(config.Hooks)
	if err != nil {
		return nil, "", err
	}

	project, notice, err := loadProjectHooks(config.Root, *config.TrustHooks)
	if err != nil {
		return nil, "", err
	}
	for p, list := range project {
		for _, hook := range list {
			hooks = appendHook(hooks, p, hook)
		}
	}
	return hooks, notice, nil
}

// loadProjectHooks reads <root>/.nacelle/hooks.yml through the trust gate
// and returns whatever hooks it adds, alongside the message worth showing
// when the file exists but has never been approved.
//
// The gate is keyed by content hash, not path: editing the file re-arms the
// question, which is the whole difference between trusting a thing once and
// trusting everything it will ever become. A missing file is the ordinary
// case and loads nothing quietly.
func loadProjectHooks(root string, trustNew bool) (map[nacelle.HookPoint][]nacelle.Hook, string, error) {
	path := filepath.Join(root, HooksFile)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %w", path, err)
	}

	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])

	store, err := loadHookTrust()
	if err != nil {
		return nil, "", err
	}
	record, seen := store[path]
	if !seen || record.Hash != hash {
		if trustNew {
			if err := saveHookTrust(store, path, hash); err != nil {
				return nil, "", err
			}
		} else {
			return nil, fmt.Sprintf(
				"This project defines hooks in %s (%d lines of commands that run on every tool call) and they are not trusted yet.\n"+
					"Read them, then restart with -trust-hooks to approve this version.", HooksFile, bytes.Count(raw, []byte("\n"))), nil
		}
	}

	specs, err := parseHooks(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", path, err)
	}
	hooks, err := buildHooks(specs)
	if err != nil {
		return nil, "", fmt.Errorf("in %s: %w", path, err)
	}
	return hooks, "", nil
}

// parseHooks decodes one hooks file. KnownFields applies for the reason
// config.load already wrote down: an unrecognised key is a typo found in one
// second instead of a hook that never fires.
func parseHooks(raw []byte) (hookConfig, error) {
	var file struct {
		Hooks hookConfig `yaml:"hooks"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return file.Hooks, nil
}

// hookTrustRecord is one trusted file: the hash that was approved, and when,
// for the human reading the store by hand.
type hookTrustRecord struct {
	Hash      string `json:"hash"`
	TrustedAt string `json:"trustedAt"`
}

// loadHookTrust reads every saved hook approval. No store yet is the
// ordinary first-run case, not an error.
func loadHookTrust() (map[string]hookTrustRecord, error) {
	dir, err := trustDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, HookTrustFile))
	if os.IsNotExist(err) {
		return map[string]hookTrustRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	store := map[string]hookTrustRecord{}
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, err
	}
	return store, nil
}

// saveHookTrust records one approval, creating ~/.nacelle/ if trust.json
// has not needed it before now.
func saveHookTrust(store map[string]hookTrustRecord, path, hash string) error {
	dir, err := trustDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	store[path] = hookTrustRecord{Hash: hash, TrustedAt: time.Now().UTC().Format(time.RFC3339)}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, HookTrustFile), raw, 0o644)
}
