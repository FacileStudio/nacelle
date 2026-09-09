package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome expands a leading ~ or ~user to the corresponding home directory.
// If the path doesn't start with ~, it is returned unchanged.
func ExpandHome(name string) string {
	if !strings.HasPrefix(name, "~") {
		return name
	}
	if name == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return name
		}
		return home
	}
	if strings.HasPrefix(name, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return name
		}
		return filepath.Join(home, name[2:])
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return name
	}
	userPart := name[1:]
	return filepath.Join(home, userPart)
}

// AbsRoot always returns an absolute path. If the input path starts with ~,
// it is expanded first. If it's already absolute, it's returned as-is.
func AbsRoot(name string) (string, error) {
	name = ExpandHome(name)
	if filepath.IsAbs(name) {
		return filepath.Abs(name)
	}

	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// clean normalises a model-supplied path into an absolute path.
//
// When the model passes an absolute path, it is resolved to absolute form —
// the model sees paths that look absolute from banner/environment and sends
// them back. When the absolute path points outside the working directory, it
// is returned as absolute (the caller is responsible for any further checks).
//
// When the model passes a relative path, it is resolved relative to the set's
// base directory (cfg.Root).
func clean(name, root string) (string, error) {
	original := strings.TrimSpace(name)
	trimmed := ExpandHome(original)
	if trimmed == "" {
		return "", fmt.Errorf("no path given")
	}

	if filepath.IsAbs(trimmed) {

		abs, err := AbsRoot(trimmed)
		if err != nil {
			return "", err
		}
		if abs == "/" || abs == "/." {
			return "", fmt.Errorf("%q is the root directory, not a file", name)
		}
		return abs, nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(filepath.Join(absRoot, trimmed))
	if cleaned == absRoot || cleaned == "." {
		return "", fmt.Errorf("%q is the root directory, not a file", name)
	}
	return cleaned, nil
}

// cleanDir normalises a directory path.
//
// Absolute paths under root are resolved to absolute form. Relative paths are
// resolved relative to root. Returns an absolute path always.
func cleanDir(name, root string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	if filepath.IsAbs(trimmed) {
		return AbsRoot(trimmed)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(absRoot, trimmed)), nil
}
