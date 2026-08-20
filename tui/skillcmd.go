package main

import (
	"os"
	"sort"

	tea "charm.land/bubbletea/v2"
)

// bySkillName keys every loaded skill by its own name, the shape
// /skill:name needs to be a lookup rather than a scan every time it's
// typed. Collisions keep the first skill found, the same rule
// loadSkills/skillNotice already lives with for name collisions between
// ~/.agents/skills, a trusted project directory and -skill-dir — nothing
// here is a second policy on top of that one.
func bySkillName(skills []skill) map[string]skill {
	byName := make(map[string]skill, len(skills))
	for _, s := range skills {
		if _, exists := byName[s.name]; !exists {
			byName[s.name] = s
		}
	}
	return byName
}

// skillCommandNames lists every loaded skill as a "/skill:name" suggestion,
// sorted the same way commandNames is.
func skillCommandNames(skills map[string]skill) []string {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, "/skill:"+name)
	}
	sort.Strings(names)
	return names
}

// runSkill is what /skill:name resolves to: read the skill's own SKILL.md
// and send it as the question, rather than waiting on the model to decide
// on its own that the skill applies and read it via read_file. Unlike
// clear/help/quit this does start a run — the whole point of naming a
// skill directly is to have the model act on it.
func runSkill(s skill, args string) command {
	return func(m *model) tea.Cmd {
		text, err := skillPrompt(s, args)
		if err != nil {
			m.say(fromClient, "reading "+s.path+": "+err.Error())
			return nil
		}
		return m.send(text)
	}
}

// skillPrompt builds what /skill:name actually sends: the skill's own
// SKILL.md content, with anything typed after the name appended the way
// pi's own /skill:name does it — "User: <args>" — so /skill:pdf-tools
// extract can tell the skill what to do, not just that it applies.
func skillPrompt(s skill, args string) (string, error) {
	body, err := os.ReadFile(s.path)
	if err != nil {
		return "", err
	}
	text := string(body)
	if args != "" {
		text += "\n\nUser: " + args
	}
	return text, nil
}
