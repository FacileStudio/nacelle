package tools

import (
	"os"
	"path/filepath"
)

// minimalEnv is what a command inherits when the caller names nothing.
//
// HOME because enough tools fail without one, and PATH so that commands
// resolve. Everything else a service holds in its environment — API keys,
// database URLs, session secrets — is exactly what a model must not be able
// to print, so none of it crosses.
//
// PATH is taken from this process's own environment rather than invented,
// which is what keeps the commands' view of the machine continuous with the
// shell nacelle was started from: an interactive shell has mise shims and
// ~/.local/bin on it, and a model told to run `mise run test` needs those.
// The standard user bin directories are appended for the case where nacelle
// itself was launched from a minimal environment, so the floor never drops
// below what the old hardcoded default covered.
func minimalEnv() []string {
	env := []string{"PATH=" + commandPath()}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

// commandPath is the PATH commands run with: this process's own, extended
// with the user bin directories it does not already name. Order is kept —
// earlier entries win resolution, and reordering someone's PATH changes more
// than reach.
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

// userBins is the directories personal installs land in when nothing else
// put them on PATH. Both are conventions with real packages behind them:
// ~/.local/bin is where pip --user, mise and most installers drop binaries,
// $HOME/bin is the older equivalent some shells still default to.
func userBins() []string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin")}
}
