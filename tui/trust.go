package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// TrustFile is where a decision to load a project's skills is remembered,
// so it only has to be made once per project rather than once per run.
const TrustFile = "trust.json"

// trustDir is where TrustFile lives — the first thing this package puts
// under ~/.nacelle/, which stays otherwise empty until something needs it.
// A trust decision does not belong in ~/.agents/, the shared cross-vendor
// path skills.go also reads from: that path is common ground between every
// AGENTS.md-aware tool, and what nacelle has chosen to trust on this
// machine is nacelle's own state, not something another tool should read
// or overwrite.
func trustDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nacelle"), nil
}

// trustRecord is one directory's trust decision. TrustedAt is kept for a
// human reading the file by hand; nothing in this package reads it back.
type trustRecord struct {
	TrustedAt string `json:"trustedAt"`
}

// loadTrust reads every saved decision, keyed by canonical directory path.
// A missing file is not an error — most projects have never been trusted —
// and is the same empty map a freshly created store would decode to. No
// home directory to hold one is the same ordinary case, not a failure —
// see configPath's own doc comment for why this package treats it that way
// everywhere else too.
func loadTrust() (map[string]trustRecord, error) {
	dir, err := trustDir()
	if err != nil {
		return map[string]trustRecord{}, nil //nolint:nilerr
	}
	raw, err := os.ReadFile(filepath.Join(dir, TrustFile))
	if os.IsNotExist(err) {
		return map[string]trustRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	store := map[string]trustRecord{}
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, err
	}
	return store, nil
}

// saveTrust writes every decision back, creating ~/.nacelle/ the first time
// anything is trusted.
func saveTrust(store map[string]trustRecord) error {
	dir, err := trustDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, TrustFile), raw, 0o600)
}

// trusted reports whether a directory has a saved decision.
func trusted(store map[string]trustRecord, dir string) bool {
	_, ok := store[dir]
	return ok
}

// trust records a directory as trusted, mutating store in place so the
// caller can trust several directories before one saveTrust call.
func trust(store map[string]trustRecord, dir string) {
	store[dir] = trustRecord{TrustedAt: time.Now().UTC().Format(time.RFC3339)}
}
