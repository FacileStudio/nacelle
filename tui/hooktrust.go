package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

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

// contentHash is what the trust store keys on: one byte changed is a
// different file.
func contentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
