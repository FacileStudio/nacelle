package main

import (
	"os"
	"path/filepath"
	"strings"
)

// extraSkills reads every skill under each of dirs — another tool's own
// skills folder, most often, named so its skills reach the model without
// moving or copying anything. No trust decision applies, for the same
// reason globalSkills has none: naming a directory here is something only
// the person running nacelle, on their own machine, can do in the first
// place.
func extraSkills(dirs []string) []skill {
	var found []skill
	for _, dir := range dirs {
		found = append(found, skillsIn(expandHome(dir))...)
	}
	return found
}

// expandHome resolves a leading "~" the way a shell would. A flag's own
// argument never needs this — the shell already expanded it before nacelle
// saw it — but ~/.nacelle.yml and an environment variable set by anything
// that isn't a shell (a service manager's Environment=, for one) go through
// no shell at all, so the same "~/.claude/skills" would silently work from
// one source and not another without this.
func expandHome(dir string) string {
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	return filepath.Join(home, strings.TrimPrefix(dir, "~"))
}
