package nacelle

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// newSessionID is a wall-clock stamp plus a random suffix, so two sessions
// opened within the same second cannot collide.
func newSessionID(now time.Time) (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("nacelle: transcript id: %w", err)
	}
	return now.Format("20060102-150405") + "-" + hex.EncodeToString(b[:]), nil
}

// gzipOldSessions compresses every plain .jsonl older than maxAge in place,
// leaving <name>.jsonl.gz behind and removing the original.
func gzipOldSessions(dir string, now time.Time, maxAge time.Duration) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("nacelle: transcript scan: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		src := filepath.Join(dir, entry.Name())
		if err := gzipFile(src, src+".gz"); err != nil {
			return err
		}
		if err := os.Remove(src); err != nil {
			return fmt.Errorf("nacelle: transcript rotate: %w", err)
		}
	}
	return nil
}

// gzipFile writes src into dst as a single gzip stream.
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("nacelle: transcript rotate: %w", err)
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("nacelle: transcript rotate: %w", err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
	if err != nil {
		return fmt.Errorf("nacelle: transcript rotate: %w", err)
	}

	w := gzip.NewWriter(out)
	_, copyErr := io.Copy(w, in)
	closeErr := w.Close()
	if cerr := out.Close(); copyErr == nil {
		copyErr = cerr
	}
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("nacelle: transcript rotate: %w", copyErr)
	}
	return nil
}
