package tools

import (
	"os"
	"path/filepath"
)

func commandPath() string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(path) {
		seen[dir] = true
	}
	for _, dir := range userBins() {
		if !seen[dir] {
			path += string(os.PathListSeparator) + dir
			seen[dir] = true
		}
	}
	return path
}
