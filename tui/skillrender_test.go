package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderSkillsIsEmptyWithNothingFound(t *testing.T) {
	if got := renderSkills(nil); got != "" {
		t.Errorf("rendered = %q, want empty with no skills", got)
	}
}

func TestRenderSkillsListsNameDescriptionAndPath(t *testing.T) {
	got := renderSkills([]skill{{name: "pdf-tools", description: "Extracts text.", path: "/x/SKILL.md"}})

	for _, want := range []string{"pdf-tools", "Extracts text.", "/x/SKILL.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered = %q, want it to mention %q", got, want)
		}
	}
}

// Both halves of skillNotice are independent facts and either can be true
// alone — a save failure is not the same problem as an unreviewed
// directory, and conflating them into one condition would hide whichever
// one did not happen to be checked first.
func TestSkillNoticeReportsASaveFailureSeparatelyFromSkippedSkills(t *testing.T) {
	notice := skillNotice(nil, errors.New("disk full"))

	if !strings.Contains(notice, "disk full") {
		t.Errorf("notice = %q, want the save error mentioned", notice)
	}
	if strings.Contains(notice, "not trusted") {
		t.Errorf("notice = %q, want no skipped-skills text when nothing was skipped", notice)
	}
}

func TestSkillNoticeIsEmptyWithNothingToReport(t *testing.T) {
	if got := skillNotice(nil, nil); got != "" {
		t.Errorf("notice = %q, want empty with nothing skipped and nothing failed to save", got)
	}
}
