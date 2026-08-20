package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// skill is one capability catalogued from a SKILL.md — enough for the model
// to decide whether it applies, and where to read the rest.
//
// Only name and description ever reach the system prompt; the rest of a
// skill's directory (scripts, references, assets) is read on demand by the
// model's own read_file call once it decides the skill is worth using. This
// package never reads past the frontmatter itself.
type skill struct {
	name, description, path string
}

// skillFrontmatter is the two fields this package requires, out of every
// field the Agent Skills specification allows (license, compatibility,
// metadata, allowed-tools, disable-model-invocation). Both name and
// description are required by the spec; the rest are validated by neither
// the spec's own reference tooling nor this package in a way that blocks
// loading, so they are not decoded here at all — a field this package never
// reads cannot go stale when the spec adds one.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// skillsResult is what loading skills produces: text for the system prompt,
// a note for the person watching when a project has skills sitting there
// unloaded because nobody has trusted them yet, and the skills themselves —
// so a caller that wants to run one directly, not just tell the model it
// exists, does not have to load them a second time.
type skillsResult struct {
	system string
	notice string
	skills []skill
}

// loadSkills assembles every skill nacelle will tell the model about: every
// skill under ~/.agents/skills/, every skill under a trusted .agents/
// skills/ found walking up from root, and every skill under extraDirs
// (-skill-dir / NACELLE_SKILL_DIRS / skill_dirs — another tool's own skills
// folder, most often). trustNew marks every project-local skills directory
// found on this run as trusted before deciding what loads, which is what
// -trust-skills asks for.
func loadSkills(root string, trustNew bool, extraDirs []string) skillsResult {
	var found []skill
	found = append(found, globalSkills()...)
	found = append(found, extraSkills(extraDirs)...)

	containers := projectSkillContainers(root)
	store, err := loadTrust()
	if err != nil {
		store = map[string]trustRecord{}
	}

	var saveErr error
	if trustNew {
		for _, dir := range containers {
			trust(store, dir)
		}
		if len(containers) > 0 {
			saveErr = saveTrust(store)
		}
	}

	var skipped []string
	for _, dir := range containers {
		if !trusted(store, dir) {
			skipped = append(skipped, dir)
			continue
		}
		found = append(found, skillsIn(dir)...)
	}

	return skillsResult{system: renderSkills(found), notice: skillNotice(skipped, saveErr), skills: found}
}

// globalSkills reads every skill under ~/.agents/skills/ — the same
// cross-vendor path skills.go's sibling in context.go reads ~/.agents/
// AGENTS.md from. No trust decision applies: this is the user's own
// machine, and nothing here crossed a boundary the user did not control.
func globalSkills() []skill {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return skillsIn(filepath.Join(home, ".agents", "skills"))
}

// projectSkillContainers walks from root to the filesystem root, returning
// every .agents/skills directory found along the way — the containers
// requiring trust, not yet the skills inside them.
//
// This walks to the filesystem root rather than stopping at a git repo
// boundary, unlike some other tools' equivalent walk. Consistency with the
// AGENTS.md/CLAUDE.md walk this package already does (context.go) was
// judged more valuable than an exact match to any one other tool, and
// detecting a git root would be one more thing this package has to get
// right for a boundary the trust gate makes low-stakes anyway: a directory
// with nothing to find costs one stat call.
func projectSkillContainers(root string) []string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}

	var containers []string
	for dir := abs; ; {
		candidate := filepath.Join(dir, ".agents", "skills")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			containers = append(containers, candidate)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return containers
		}
		dir = parent
	}
}

// skillsIn finds every skill under one directory, recursively: any
// directory holding a SKILL.md is a skill, and its own subtree — scripts,
// references, assets, or a directory that happens to be named like it could
// hold another skill — is not searched further once it has matched.
//
// dir not existing at all is the ordinary case for most of the locations
// this is called with (~/.agents/skills on a machine that has not adopted
// the convention, a project with no .agents/skills of its own) and is
// treated the same as a single unreadable entry inside an otherwise real
// directory: both are nothing to report, not a reason to fail.
func skillsIn(dir string) []skill {
	var found []skill
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil //nolint:nilerr
		}
		manifest := filepath.Join(path, "SKILL.md")
		if _, err := os.Stat(manifest); err != nil {
			return nil //nolint:nilerr
		}
		if s, ok := parseSkill(manifest); ok {
			found = append(found, s)
		}
		return filepath.SkipDir
	}); err != nil {
		return nil
	}
	return found
}

// parseSkill reads one SKILL.md's frontmatter. A file with no frontmatter,
// frontmatter that is not valid YAML, or a missing name or description is
// not a skill this package can present to the model — the Agent Skills
// specification requires both, and a catalog entry with nothing to search
// on is worse than not being catalogued at all.
func parseSkill(path string) (skill, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return skill{}, false
	}

	block, ok := frontmatter(string(raw))
	if !ok {
		return skill{}, false
	}

	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(block), &meta); err != nil {
		return skill{}, false
	}
	if meta.Name == "" || meta.Description == "" {
		return skill{}, false
	}
	return skill{name: meta.Name, description: meta.Description, path: path}, true
}

// frontmatter extracts the YAML block between a file's opening and closing
// --- markers, reporting whether one was found at all.
func frontmatter(content string) (string, bool) {
	if !strings.HasPrefix(content, "---\n") {
		return "", false
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

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
// skillNotice reports two different things a person needs to hear, either
// of which can be true alone: skills sitting unloaded because nobody
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
