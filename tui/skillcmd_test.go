package main

import (
	"context"
	"iter"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// bySkillName is the one place two skills of the same name from different
// directories get resolved — first one found wins, the same rule
// loadSkills already lives with for a collision between ~/.agents/skills
// and a trusted project directory.
func TestBySkillNameKeepsTheFirstOnACollision(t *testing.T) {
	byName := bySkillName([]skill{
		{name: "deploy", path: "/first/SKILL.md"},
		{name: "deploy", path: "/second/SKILL.md"},
	})

	if got := byName["deploy"].path; got != "/first/SKILL.md" {
		t.Errorf("path = %q, want the first skill found kept", got)
	}
}

func TestSkillCommandNamesListsEverySkillSlashPrefixed(t *testing.T) {
	names := skillCommandNames(bySkillName([]skill{{name: "a"}, {name: "b"}}))

	if want := []string{"/skill:a", "/skill:b"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// The whole point of /skill:name over waiting on the model: what it sends
// is the skill's own instructions, not a description of them.
func TestSkillPromptSendsTheSkillsFullBody(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: deploy\ndescription: ships the app")
	s := skill{name: "deploy", path: filepath.Join(dir, "SKILL.md")}

	got, err := skillPrompt(s, "")
	if err != nil {
		t.Fatalf("skillPrompt: %v", err)
	}
	if !strings.Contains(got, "Do the thing.") {
		t.Errorf("prompt = %q, want the skill body's own instructions", got)
	}
	if strings.Contains(got, "User:") {
		t.Errorf("prompt = %q, want no User: line when nothing followed the name", got)
	}
}

// pi's own /skill:name appends what followed the name as "User: <args>", so
// /skill:pdf-tools extract can tell the skill what to do, not just that it
// applies — this is the one place nacelle matches that shape on purpose.
func TestSkillPromptAppendsArgsAsAUserLine(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: deploy\ndescription: ships the app")
	s := skill{name: "deploy", path: filepath.Join(dir, "SKILL.md")}

	got, err := skillPrompt(s, "to staging")
	if err != nil {
		t.Fatalf("skillPrompt: %v", err)
	}
	if !strings.HasSuffix(got, "User: to staging") {
		t.Errorf("prompt = %q, want it to end with the args as a User: line", got)
	}
}

func TestSkillPromptFailsOnAMissingFile(t *testing.T) {
	if _, err := skillPrompt(skill{path: filepath.Join(t.TempDir(), "gone", "SKILL.md")}, ""); err == nil {
		t.Error("skillPrompt succeeded reading a file that was never there")
	}
}

// answeringStub is a minimal nacelle.Backend, just enough to build a real
// *nacelle.Agent for a test — /skill:name has to be provable end to end,
// not just that it builds the right string, or the one thing it exists to
// do (start a run) goes untested.
type answeringStub struct{ received nacelle.Request }

func (s *answeringStub) Name() string                       { return "stub" }
func (s *answeringStub) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (s *answeringStub) Stream(_ context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	s.received = request
	return func(yield func(nacelle.Event, error) bool) {
		yield(nacelle.Event{Kind: nacelle.KindDone}, nil)
	}
}
func (s *answeringStub) CountTokens(_ context.Context, request nacelle.Request) (int64, error) {
	s.received = request
	return 0, nil
}

// /skill:name is the one command that does start a run — proven here by
// checking the exact thing every other command test checks the absence of.
func TestSlashSkillStartsARunWithTheSkillsBodyAsTheQuestion(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: deploy\ndescription: ships the app")
	s := skill{name: "deploy", path: filepath.Join(dir, "SKILL.md")}

	agent, err := nacelle.New(nacelle.Config{Backend: &answeringStub{}, System: "test"})
	if err != nil {
		t.Fatalf("nacelle.New: %v", err)
	}
	m := newModel(agent, "test · model", []skill{s})
	m.resize(tea.WindowSizeMsg{Width: 80, Height: 24})
	t.Cleanup(m.run.cancel)

	m.prompt.SetValue("/skill:deploy to staging")
	m.ask()

	if !m.run.busy {
		t.Fatal("/skill:deploy did not start a run")
	}
	if len(m.conversation) != 1 {
		t.Fatalf("conversation = %v, want exactly the expanded skill sent", m.conversation)
	}
	sent := m.conversation[0].Parts[0].(nacelle.Text).Text
	if !strings.Contains(sent, "Do the thing.") || !strings.HasSuffix(sent, "User: to staging") {
		t.Errorf("sent = %q, want the skill body plus the args as a User: line", sent)
	}
}

// A typo'd skill name is exactly as much a mistake as a typo'd command
// name, and gets the same treatment: reported, never sent to the model.
func TestSlashSkillReportsAnUnknownSkillWithoutStartingARun(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/skill:nope")

	printed := printedBy(m.ask())

	if m.run.busy {
		t.Error("an unknown skill started a run")
	}
	echo, reply := strings.Index(printed, "/skill:nope"), strings.Index(printed, "unknown skill")
	if reply < 0 || !strings.Contains(printed, "nope") {
		t.Fatalf("printed = %q, want a line naming the unknown skill", printed)
	}
	if echo < 0 || echo > reply {
		t.Errorf("printed = %q, want the echoed input above the reply to it", printed)
	}
}
