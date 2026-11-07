package tools

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// matchGlob reports whether name matches pattern, with ** matching any number
// of path segments.
//
// The standard library's filepath.Match has no **, and ** is the segment
// models reach for first — a glob tool without it forces a caller to know the
// directory depth in advance, which is exactly what they were searching to
// find out.
func matchGlob(pattern, name string) bool {
	var matchStar func([]string, []string) bool
	matchStar = func(p, n []string) bool {
		if len(p) == 0 {
			return true
		}
		for i := 0; i <= len(n); i++ {
			if matchSegments(p, n[i:], matchStar) {
				return true
			}
		}
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"), matchStar)
}

// matchSegments matches path segments against pattern segments. matchStar is
// the closure that handles a ** segment, which recurses back into
// matchSegments, so the two call each other rather than duplicating the loop.
func matchSegments(pattern, name []string, matchStar func([]string, []string) bool) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			return matchStar(pattern[1:], name)
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := filepath.Match(pattern[0], name[0]); err != nil || !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// collect walks the directory a model named, or the working directory when it
// named none, and hands keep every file the glob matches. An empty glob
// matches every file.
func (s *Set) collect(path, glob string, keep func(name string)) error {
	dir, err := cleanDir(path, s.dir, s.pathIsolation)
	if err != nil {
		return err
	}
	glob = relativise(glob, dir)
	return s.walk(dir, func(name, rel string, _ fs.DirEntry) error {
		if glob == "" || matchGlob(glob, name) || matchGlob(glob, rel) {
			keep(name)
		}
		return nil
	})
}
