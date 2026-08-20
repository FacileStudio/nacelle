package tools

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/FacileStudio/nacelle"
)

// skipped are directories never worth walking. They are large, generated, and
// searching them buries the answer rather than finding more of it.
var skipped = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".svelte-kit": true, ".next": true,
	"__pycache__": true, ".venv": true, "coverage": true,
}

type globInput struct {
	Pattern string `json:"pattern" jsonschema:"required,description=A glob such as **/*.go or cmd/**. ** matches any number of directories"`
}

// globTool builds the file finder.
func (s *Set) globTool() (nacelle.Tool, error) {
	return nacelle.NewTool("find_files",
		"List files matching a glob, relative to the working directory. Use it to learn what exists before reading anything. Generated directories such as .git, node_modules and vendor are skipped.",
		func(_ context.Context, in globInput) (string, error) {
			pattern := strings.TrimSpace(strings.TrimPrefix(in.Pattern, "/"))
			if pattern == "" {
				return "", fmt.Errorf("no pattern given")
			}

			var found []string
			err := s.walk(func(name string, _ fs.DirEntry) error {
				if matchGlob(pattern, name) {
					found = append(found, name)
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			if len(found) == 0 {
				return fmt.Sprintf("no files match %s", pattern), nil
			}
			sort.Strings(found)
			return truncate(strings.Join(found, "\n"), s.maxOutput), nil
		})
}

type grepInput struct {
	Pattern string `json:"pattern" jsonschema:"required,description=A Go regular expression to search for"`
	Glob    string `json:"glob,omitempty" jsonschema:"description=Only search files matching this glob — for example **/*.go"`
}

// grepTool builds the content search.
func (s *Set) grepTool() (nacelle.Tool, error) {
	return nacelle.NewTool("search_files",
		"Search file contents with a regular expression, returning matching lines with their file and line number. Narrow it with a glob when you know the file type. Use this to find where something is defined or used, rather than reading files one at a time.",
		func(_ context.Context, in grepInput) (string, error) {
			expression, err := regexp.Compile(in.Pattern)
			if err != nil {
				return "", fmt.Errorf("that is not a valid regular expression: %w", err)
			}
			glob := strings.TrimSpace(strings.TrimPrefix(in.Glob, "/"))

			var matches []string
			err = s.walk(func(name string, _ fs.DirEntry) error {
				if glob == "" || matchGlob(glob, name) {
					s.grepFile(name, expression, &matches)
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			if len(matches) == 0 {
				return fmt.Sprintf("no matches for %s", in.Pattern), nil
			}
			return truncate(strings.Join(matches, "\n"), s.maxOutput), nil
		})
}

// grepFile appends the matching lines of one file.
//
// It reads whole files and skips anything that is not text, which is what
// keeps a binary out of the model's context: a match inside a compiled object
// is never the answer to the question that was asked. A file it cannot read is
// skipped on the same terms — one unreadable file is not a reason to fail a
// search across the rest of the tree.
//
// It reports nothing because there is nothing to report: neither of those is
// an error, and a search that cannot fail should not oblige every caller to
// check whether it did.
func (s *Set) grepFile(name string, expression *regexp.Regexp, matches *[]string) {
	raw, err := s.root.ReadFile(name)
	if err != nil || isBinary(raw) {
		return
	}
	for number, line := range strings.Split(string(raw), "\n") {
		if expression.MatchString(line) {
			*matches = append(*matches, fmt.Sprintf("%s:%d: %s", name, number+1, strings.TrimSpace(line)))
		}
	}
}

// walk visits every file under the root, skipping generated directories.
//
// It walks root.FS() rather than the real filesystem, so the traversal itself
// cannot follow a symlink out of the tree.
func (s *Set) walk(visit func(name string, entry fs.DirEntry) error) error {
	return fs.WalkDir(s.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an entry the walk cannot open is skipped, not fatal
		}
		if entry.IsDir() {
			if name != "." && (skipped[entry.Name()] || strings.HasPrefix(entry.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		return visit(name, entry)
	})
}

// isBinary reports whether data looks like something a model should not read.
// A NUL byte in the first few hundred bytes is the same heuristic grep uses.
func isBinary(data []byte) bool {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return false
}

// matchGlob reports whether name matches pattern, with ** matching any number
// of path segments.
//
// The standard library's filepath.Match has no **, and ** is the segment
// models reach for first — a glob tool without it forces a caller to know the
// directory depth in advance, which is exactly what they were searching to
// find out.
func matchGlob(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchSegments matches path segments against pattern segments.
func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			return matchDoubleStar(pattern[1:], name)
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// matchDoubleStar tries the rest of the pattern at every remaining position,
// which is what lets ** stand for zero or more segments.
func matchDoubleStar(pattern, name []string) bool {
	if len(pattern) == 0 {
		return true
	}
	for i := 0; i <= len(name); i++ {
		if matchSegments(pattern, name[i:]) {
			return true
		}
	}
	return false
}
