package tools

import (
	"os"
	"path/filepath"
)

func userBins() []string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin")}
}
