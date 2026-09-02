package tools

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/FacileStudio/nacelle"
)

// resolvePath tries to make an absolute path relative to root, returning the
// relative segment when the path sits under root. When it is outside, the
// caller gets an error and should fall back to stripping the leading slash,
// letting os.Root be the authoritative boundary.
func resolvePath(name, root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absName, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absName)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q is outside the working directory", name)
	}
	return rel, nil
}

// clean normalises a model-supplied path into one os.Root will accept.
//
// When the model passes an absolute path that sits inside the working
// directory, it is resolved to a relative one — the model sees paths that
// look absolute from banner/environment and sends them back, and refusing or
// silently stripping the leading slash costs a turn. When the absolute path
// points outside root, it is tried anyway (os.Root refuses it at the kernel
// boundary, so the denial is authoritative, not guesswork from string
// prefixes).
//
// root is the working directory the tools are confined to.
func clean(name, root string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("no path given")
	}
	if path.IsAbs(trimmed) {
		if rel, err := resolvePath(trimmed, root); err == nil {
			return rel, nil
		}
		trimmed = strings.TrimPrefix(trimmed, "/")
	}
	cleaned := path.Clean(strings.TrimPrefix(trimmed, "/"))
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("%q is the root directory, not a file", name)
	}
	return cleaned, nil
}

type readInput struct {
	Path   string `json:"path" jsonschema:"required,description=Path to the file relative to the working directory"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=First line to show. Counting starts at 1. Omit to start at the beginning"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=How many lines to show. Omit for as many as fit"`
}

// readTool builds the file reader.
func (s *Set) readTool() (nacelle.Tool, error) {
	return nacelle.NewTool("read_file",
		"Read a file from the working directory. Returns the contents with line numbers, so you can quote a line back exactly. Use it before editing anything.",
		func(_ context.Context, in readInput) (string, error) {
			name, err := clean(in.Path, s.dir)
			if err != nil {
				return "", err
			}
			raw, err := s.root.ReadFile(name)
			if err != nil {
				return "", err
			}
			if !utf8.Valid(raw) {
				return "", fmt.Errorf("%s is not text", name)
			}
			s.read.record(name)
			return numbered(string(raw), in.Offset, in.Limit, s.maxRead), nil
		})
}

// numbered renders a slice of a file with line numbers.
//
// Line numbers are what let a model refer to a place in the file, and what
// make its answer checkable against the same file opened by a person.
func numbered(content string, offset, limit, maxBytes int) string {
	lines := strings.Split(content, "\n")
	if offset > 0 {
		if offset > len(lines) {
			return fmt.Sprintf("[the file has %d lines; line %d is past the end]", len(lines), offset)
		}
		lines = lines[offset-1:]
	} else {
		offset = 1
	}
	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}

	var out strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&out, "%6d\t%s\n", offset+i, line)
	}
	return truncate(out.String(), maxBytes)
}

type writeInput struct {
	Path    string `json:"path" jsonschema:"required,description=Path to the file relative to the working directory"`
	Content string `json:"content" jsonschema:"required,description=The complete new contents of the file"`
}

// writeTool builds the file writer.
func (s *Set) writeTool() (nacelle.Tool, error) {
	return nacelle.NewTool("write_file",
		"Create a file, or replace one entirely. The content you give is the whole file, not a fragment. To change part of an existing file use edit_file instead, which cannot silently discard the rest.",
		func(_ context.Context, in writeInput) (string, error) {
			name, err := clean(in.Path, s.dir)
			if err != nil {
				return "", err
			}
			if dir := path.Dir(name); dir != "." {
				if err := s.root.MkdirAll(dir, 0o755); err != nil {
					return "", err
				}
			}
			if err := s.root.WriteFile(name, []byte(in.Content), 0o644); err != nil {
				return "", err
			}
			s.read.record(name)
			return fmt.Sprintf("wrote %s (%d bytes)", name, len(in.Content)), nil
		})
}

type editInput struct {
	Path string `json:"path" jsonschema:"required,description=Path to the file relative to the working directory"`
	Old  string `json:"old" jsonschema:"required,description=The exact text to replace. Include enough surrounding lines to make it unique in the file"`
	New  string `json:"new" jsonschema:"required,description=The text to put in its place"`
}

// editTool builds the exact-match editor.
func (s *Set) editTool() (nacelle.Tool, error) {
	return nacelle.NewTool("edit_file",
		"Replace an exact piece of text in a file. The old text must appear exactly once: include the lines around it until it does. Read the file first.",
		func(_ context.Context, in editInput) (string, error) {
			name, err := clean(in.Path, s.dir)
			if err != nil {
				return "", err
			}
			if !s.read.seen(name) {
				return "", fmt.Errorf("read %s before editing it", name)
			}
			raw, err := s.root.ReadFile(name)
			if err != nil {
				return "", err
			}

			edited, err := replaceOnce(string(raw), in.Old, in.New)
			if err != nil {
				return "", fmt.Errorf("%s: %w", name, err)
			}
			if err := s.root.WriteFile(name, []byte(edited), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s", name), nil
		})
}
