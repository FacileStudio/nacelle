package main

import (
	"fmt"
	"strings"
)

// renderSkills catalogues every skill for the system prompt: name,
// description, and where to read the rest. Nothing here is a promise the
// model will use any of it — this is the progressive-disclosure half, the
// short summary that is always in context so the model can decide whether
// the long version is worth a read_file call.
func renderSkills(skills []skill) string {
	if len(skills) == 0 {
		return ""
	}
	var body strings.Builder
	body.WriteString("\n\n## Skills available\n\n")
	body.WriteString("Each is a directory with more detail in its SKILL.md. Read one with " +
		"read_file when its description matches what you are doing, then follow it.\n\n")
	for _, s := range skills {
		fmt.Fprintf(&body, "- **%s** (%s): %s\n", s.name, s.path, s.description)
	}
	return body.String()
}

// skillNotice tells the person running nacelle when a project has skills
// sitting unloaded because nobody has trusted them yet. Skills can carry
// instructions to run arbitrary scripts, unlike the plain instruction text
// projectContext reads — that is the whole difference, and the reason this
// gate exists where that one deliberately does not.
//
// It reports two different things a person needs to hear, either of which
// can be true alone: skills sitting unloaded because nobody
// trusted them, and a trust decision made this run that failed to persist
// — which without a word about it would leave someone passing -trust-skills
// again next time, unable to tell that from it simply not having worked.
func skillNotice(skipped []string, saveErr error) string {
	var lines []string
	if len(skipped) > 0 {
		suffix := "ies"
		if len(skipped) == 1 {
			suffix = "y"
		}
		lines = append(lines, fmt.Sprintf("%d project skill director%s found but not trusted, so nothing under %s "+
			"loaded. Review the contents, then rerun with -trust-skills to load them and remember the decision.",
			len(skipped), suffix, strings.Join(skipped, ", ")))
	}
	if saveErr != nil {
		lines = append(lines, fmt.Sprintf("trust was granted for this run but could not be saved (%v), "+
			"so -trust-skills will be needed again next time.", saveErr))
	}
	return strings.Join(lines, " ")
}
