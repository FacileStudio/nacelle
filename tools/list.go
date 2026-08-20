package tools

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/FacileStudio/nacelle"
)

type listInput struct {
	Path string `json:"path,omitempty" jsonschema:"description=Directory to list relative to the working directory. Omit it for the working directory itself"`
}

// listTool builds the directory lister.
//
// find_files answers "which files match this", and a model that has just
// arrived cannot write the glob yet: it walks files only, so `find_files *`
// returns the top-level files and no sign that apps/ or tui/ are there at all.
// The move that worked instead was `**/*` — every path in the tree, paid for
// out of the context window — and inferring the directories from the paths
// that happened to run through them. This is that question asked cheaply.
func (s *Set) listTool() (nacelle.Tool, error) {
	return nacelle.NewTool("list_directory",
		"List one directory: the files in it, and the subdirectories in it marked with a trailing slash. Use it to find your way around a tree you have not seen, before searching it. Generated directories such as .git, node_modules and vendor are left out, exactly as they are when searching.",
		func(_ context.Context, in listInput) (string, error) {
			name := cleanDir(in.Path)
			entries, err := s.readDir(name)
			if err != nil {
				return "", err
			}
			listed := listing(entries)
			if len(listed) == 0 {
				return fmt.Sprintf("nothing to list in %s", name), nil
			}
			return truncate(strings.Join(listed, "\n"), s.maxOutput), nil
		})
}

// cleanDir normalises a directory path where clean refuses to.
//
// clean is written for files and rejects "." outright, which is the one path a
// listing has to accept: the most useful call this tool takes is the one with
// no argument, made by a model that has nothing to name yet. The rest is
// clean's behaviour, leading slash included — os.Root is what confines this, so
// tidiness here buys nothing and refusing a rooted-looking path costs a turn.
func cleanDir(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "."
	}
	return path.Clean(strings.TrimPrefix(trimmed, "/"))
}

// readDir lists one directory through the root handle.
//
// Deliberately root.Open and not root.FS(): io/fs refuses any path holding a
// ".." as malformed before it opens anything, and that is a string check —
// the thing os.Root exists to replace. Going through the handle means "../.."
// and a symlink pointing out of the tree are both refused by the kernel at the
// component that leaves, on the same terms read_file already gets.
func (s *Set) readDir(name string) ([]fs.DirEntry, error) {
	dir, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	return dir.ReadDir(-1)
}

// listing renders the entries, marking directories and dropping the ones a
// search would never visit.
//
// The filter is walk's, on purpose. A model shown node_modules/ here will
// search it, find nothing, and have learnt something false about the tree; a
// listing that disagrees with find_files about what exists is worse than one
// that shows less. Dotfiles stay, because walk skips dot *directories* only —
// .gitignore and .golangci.yml are searchable, and are often the answer.
func listing(entries []fs.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
			continue
		}
		if skipped[entry.Name()] || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name()+"/")
	}
	sort.Strings(names)
	return names
}
