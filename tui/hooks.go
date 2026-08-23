package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/FacileStudio/nacelle"
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
		var hook nacelle.Hook
		if spec.Async {
			hook = nacelle.Async(execHook(spec))
		} else {
			hook = nacelle.WithTimeout(spec.duration(), execHook(spec))
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

	specs, err := parseHooks(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", path, err)
	}
	hooks, err := buildHooks(specs)
	if err != nil {
		return nil, "", fmt.Errorf("in %s: %w", path, err)
	}

	hash := contentHash(raw)
	trusted, err := hookIsTrusted(path, hash, trustNew)
	if err != nil {
		return nil, "", err
	}
	if !trusted {
		return nil, fmt.Sprintf(
			"This project defines hooks in %s (%d lines of commands that run on every tool call) and they are not trusted yet.\n"+
				"Read them, then restart with -trust-hooks to approve this version.", HooksFile, bytes.Count(raw, []byte("\n"))), nil
	}
	return hooks, "", nil
}

// hookIsTrusted reports whether this exact file content has been approved,
// remembering it now if trustNew asks. Validation runs before this is ever
// reached, so a file recorded here is a file that parsed — trusting first
// and failing later would pin broken content and turn every launch after it
// into a parse error instead of a question.
func hookIsTrusted(path, hash string, trustNew bool) (bool, error) {
	store, err := loadHookTrust()
	if err != nil {
		return false, err
	}
	if record, seen := store[path]; seen && record.Hash == hash {
		return true, nil
	}
	if !trustNew {
		return false, nil
	}
	return true, saveHookTrust(store, path, hash)
}
