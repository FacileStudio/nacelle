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

// checkOutsideRoot returns an error when abs resolves outside absRoot under the
// given name and root context.
func checkOutsideRoot(absRoot, abs, name, root string) error {
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return fmt.Errorf("%q is outside the working directory %q", name, root)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is outside the working directory %q", name, root)
	}
	return nil
}

// checkNotRoot returns an error when abs is the root directory.
func checkNotRoot(abs, name string) error {
	if abs == "/" || abs == "/." {
		return fmt.Errorf("%q is the root directory, not a file", name)
	}
	return nil
}

// resolveCleanPath turns a model-supplied path into an absolute path, rejecting
// the root directory.
func resolveCleanPath(trimmed string, absRoot, name string) (string, error) {
	if filepath.IsAbs(trimmed) {
		abs, err := AbsRoot(trimmed)
		if err != nil {
			return "", err
		}
		if err := checkNotRoot(abs, name); err != nil {
			return "", err
		}
		return abs, nil
	}

	cleaned := filepath.Clean(filepath.Join(absRoot, trimmed))
	if cleaned == absRoot || cleaned == "." {
		return "", fmt.Errorf("%q is the root directory, not a file", name)
	}
	return cleaned, nil
}

// resolveDirPath turns a directory path into an absolute path.
func resolveDirPath(trimmed string, absRoot string) (string, error) {
	if filepath.IsAbs(trimmed) {
		return AbsRoot(trimmed)
	}
	return filepath.Clean(filepath.Join(absRoot, trimmed)), nil
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
//
// When strict is true, paths that resolve outside root are rejected, catching
// both absolute escapes and ../ traversal on relative inputs.
func clean(name, root string, strict bool) (string, error) {
	original := strings.TrimSpace(name)
	trimmed := ExpandHome(original)
	if trimmed == "" {
		return "", fmt.Errorf("no path given")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	abs, err := resolveCleanPath(trimmed, absRoot, name)
	if err != nil {
		return "", err
	}

	if strict {
		if err := checkOutsideRoot(absRoot, abs, name, root); err != nil {
			return "", err
		}
	}

	return abs, nil
}

// cleanDir normalises a directory path.
//
// Absolute paths under root are resolved to absolute form. Relative paths are
// resolved relative to root. Returns an absolute path always. The empty string
// resolves to root itself, which means "list the working directory".
//
// When strict is true, paths that resolve outside root are rejected.
func cleanDir(name, root string, strict bool) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		return abs, nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	abs, err := resolveDirPath(trimmed, absRoot)
	if err != nil {
		return "", err
	}

	if strict {
		if err := checkOutsideRoot(absRoot, abs, name, root); err != nil {
			return "", err
		}
	}

	return abs, nil
}
