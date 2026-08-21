package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkill creates dir/SKILL.md with the given frontmatter body (the
// text between the --- markers) and a placeholder instruction body.
func writeSkill(t *testing.T, dir, frontmatterBody string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "---\n" + frontmatterBody + "\n---\n\n# Instructions\n\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// The Agent Skills specification requires both, and a catalog entry with
// nothing to search on would be worse than no entry at all.
func TestParseSkillRequiresNameAndDescription(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]string{
		"missing description": "name: my-skill",
		"missing name":        "description: does a thing",
		"neither":             "license: MIT",
	}
	for label, body := range cases {
		t.Run(label, func(t *testing.T) {
			writeSkill(t, dir, body)
			if _, ok := parseSkill(filepath.Join(dir, "SKILL.md")); ok {
				t.Errorf("%s: parsed as a skill, want rejected", label)
			}
		})
	}
}

func TestParseSkillAcceptsValidFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: pdf-tools\ndescription: Extracts text from PDF files.")

	s, ok := parseSkill(filepath.Join(dir, "SKILL.md"))
	if !ok {
		t.Fatal("a valid SKILL.md was rejected")
	}
	if s.name != "pdf-tools" || s.description != "Extracts text from PDF files." {
		t.Errorf("skill = %+v, want the frontmatter's own name and description", s)
	}
}

// A file that never opens a frontmatter block at all is a plain markdown
// file, not a broken skill — the two have to be told apart the same way a
// missing name is, not treated as a parse failure worth surfacing.
func TestParseSkillRejectsAFileWithNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Just a heading\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, ok := parseSkill(filepath.Join(dir, "SKILL.md")); ok {
		t.Error("a file with no frontmatter parsed as a skill")
	}
}

// A skill's own subdirectories — scripts, references, assets — are its
// private file space, not more skills to discover. A nested SKILL.md
// planted one level inside an already-matched skill must not surface as a
// second skill.
func TestSkillsInDoesNotDescendIntoAMatchedSkillsOwnSubtree(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "outer-skill"), "name: outer-skill\ndescription: the one that should be found")
	writeSkill(t, filepath.Join(root, "outer-skill", "nested"), "name: nested-skill\ndescription: should never surface")

	found := skillsIn(root)

	if len(found) != 1 {
		t.Fatalf("found = %+v, want exactly the outer skill, not the one nested inside it", found)
	}
	if found[0].name != "outer-skill" {
		t.Errorf("found = %+v, want outer-skill", found)
	}
}

// Skills can sit side by side, unlike the nested case above — a sibling
// directory is still fair game after an earlier one matched.
func TestSkillsInFindsMultipleSiblingSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "name: skill-a\ndescription: first")
	writeSkill(t, filepath.Join(root, "b"), "name: skill-b\ndescription: second")

	found := skillsIn(root)

	if len(found) != 2 {
		t.Fatalf("found = %+v, want both sibling skills", found)
	}
}

// A directory that does not exist at all — ~/.agents/skills on a machine
// that has never adopted the convention — is the ordinary case, not an
// error the walk should surface as one.
func TestSkillsInToleratesAMissingDirectory(t *testing.T) {
	if found := skillsIn(filepath.Join(t.TempDir(), "does-not-exist")); found != nil {
		t.Errorf("found = %+v, want nil for a directory that was never there", found)
	}
}

// ~/.agents/skills is the same cross-vendor path context_test.go already
// isolates HOME for — no trust decision applies to it, so this only has to
// prove it is actually read.
func TestGlobalSkillsReadsFromTheHomeAgentsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSkill(t, filepath.Join(home, ".agents", "skills", "brave-search"),
		"name: brave-search\ndescription: Web search via an API.")

	found := globalSkills()

	if len(found) != 1 || found[0].name != "brave-search" {
		t.Errorf("found = %+v, want the one global skill", found)
	}
}

// Every .agents/skills between root and the filesystem root is a separate
// trust boundary — a monorepo's root and a package deep inside it can both
// have their own, and both have to be found for the caller to decide on.
func TestProjectSkillContainersFindsEveryLevel(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "packages", "api")
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sub, ".agents", "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	containers := projectSkillContainers(sub)

	if len(containers) != 2 {
		t.Fatalf("containers = %v, want both the root's and the package's", containers)
	}
}

// Once ~/.agents/skills exists — which mycelium's `agents` adapter now creates
// — $HOME is an ancestor of nearly every root anyone runs from, so the walk
// up reaches the global directory on essentially every run. It must not be
// offered as a project container: globalSkills already loaded it, ungated.
//
// This is written with root *underneath* home on purpose. The other tests in
// this file put root in a sibling temp directory, which is why none of them
// could see this: the walk never passed through the home they had set.
func TestProjectSkillContainersSkipsTheGlobalDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSkill(t, filepath.Join(home, ".agents", "skills", "shared"),
		"name: shared\ndescription: a global skill")
	root := filepath.Join(home, "Code", "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if containers := projectSkillContainers(root); len(containers) != 0 {
		t.Fatalf("containers = %v, want ~/.agents/skills left out", containers)
	}

	untrusting := loadSkills(root, false, nil)
	if untrusting.notice != "" {
		t.Errorf("notice = %q, want nothing reported as untrusted", untrusting.notice)
	}
	if len(untrusting.skills) != 1 {
		t.Errorf("skills = %+v, want the one global skill", untrusting.skills)
	}

	trusting := loadSkills(root, true, nil)
	if len(trusting.skills) != 1 {
		t.Errorf("skills = %+v, want the global skill once, not once per path that found it", trusting.skills)
	}
}

// The whole reason a trust gate exists here and not on plain context files:
// a project's .agents/skills can carry instructions to run arbitrary
// scripts, so it does not load until something has decided to trust it.
func TestLoadSkillsSkipsUntrustedProjectSkills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, ".agents", "skills", "risky"),
		"name: risky\ndescription: not yet reviewed")

	result := loadSkills(root, false, nil)

	if strings.Contains(result.system, "risky") {
		t.Errorf("system = %q, want the untrusted skill left out", result.system)
	}
	if !strings.Contains(result.notice, "not trusted") {
		t.Errorf("notice = %q, want it to say the skill was found but not trusted", result.notice)
	}
}

// -trust-skills does two things in one pass: it loads what it finds this
// run, and it remembers the decision so the next run does not need the flag
// again. Both have to be true, not just the first.
func TestLoadSkillsWithTrustNewLoadsAndPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, ".agents", "skills", "reviewed"),
		"name: reviewed\ndescription: looked at it, seems fine")

	trusting := loadSkills(root, true, nil)
	if !strings.Contains(trusting.system, "reviewed") {
		t.Fatalf("system = %q, want the skill loaded on the trusting run", trusting.system)
	}

	again := loadSkills(root, false, nil)
	if !strings.Contains(again.system, "reviewed") {
		t.Errorf("system = %q, want the skill still loaded on a later run with no flag, from the saved decision", again.system)
	}
	if again.notice != "" {
		t.Errorf("notice = %q, want no notice once the directory is already trusted", again.notice)
	}
}
