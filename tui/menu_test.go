package main

import (
	"strings"
	"testing"
)

func TestFuzzyMatchFindsAnOrderPreservingSubsequence(t *testing.T) {
	if !fuzzyMatch("hunk-review", "hkrev") {
		t.Error(`fuzzyMatch("hunk-review", "hkrev") = false, want true — h,k,r,e,v all appear in order`)
	}
	if strings.Contains("hunk-review", "hkrev") {
		t.Fatal("test is not exercising the subsequence path — hkrev is a literal substring")
	}
}

func TestFuzzyMatchRejectsOutOfOrderCharacters(t *testing.T) {
	if fuzzyMatch("review", "vre") {
		t.Error(`fuzzyMatch("review", "vre") = true, want false — v comes after r and e in "review"`)
	}
}

// The ranking a typed "/skill:rev" needs: a real prefix match ("review")
// ahead of a name that only contains the letters in order somewhere inside
// it ("hunk-review") — both are real matches, but one is a better one.
func TestMatchRankPrefersPrefixOverSubstringOverFuzzy(t *testing.T) {
	cases := []struct {
		name      string
		candidate string
		query     string
		want      int
	}{
		{"prefix", "/skill:review", "/skill:rev", 0},
		{"substring", "/skill:facile-review", "review", 1},
		{"fuzzy", "/skill:hunk-review", "hkrev", 2},
		{"no match", "/skill:filet", "xyz", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchRank(c.candidate, c.query); got != c.want {
				t.Errorf("matchRank(%q, %q) = %d, want %d", c.candidate, c.query, got, c.want)
			}
		})
	}
}

func TestFilterMenuRanksBestMatchesFirst(t *testing.T) {
	items := []menuItem{
		{value: "/skill:hunk-review"},
		{value: "/skill:review"},
		{value: "/skill:facile-review"},
	}

	got := filterMenu(items, "/skill:rev")

	if len(got) != 3 || got[0].value != "/skill:review" {
		t.Fatalf("filterMenu order = %v, want the prefix match first", got)
	}
}

// Just "/" typed is the whole point of opening the dropdown on the slash
// alone — an empty query has to mean "everything," not "nothing yet."
func TestFilterMenuWithEmptyQueryReturnsEverything(t *testing.T) {
	items := []menuItem{{value: "/clear"}, {value: "/help"}}

	got := filterMenu(items, "")

	if len(got) != len(items) {
		t.Errorf("filterMenu(items, \"\") = %v, want every item", got)
	}
}

func TestMenuItemsListsCommandsBeforeSkillsWithDescriptions(t *testing.T) {
	items := menuItems(map[string]skill{"deploy": {name: "deploy", description: "ships the app"}})

	if len(items) != len(commands)+1 {
		t.Fatalf("menuItems = %+v, want every command plus the one skill", items)
	}
	last := items[len(items)-1]
	if last.value != "/skill:deploy" || last.description != "ships the app" {
		t.Errorf("last item = %+v, want the skill, with its own description", last)
	}
	for _, it := range items[:len(items)-1] {
		if it.description != "" {
			t.Errorf("command %q carried a description %q, want none", it.value, it.description)
		}
	}
}

func TestCommandMenuOpenRequiresFilteredItemsAndNotDismissed(t *testing.T) {
	cases := []struct {
		name string
		mm   commandMenu
		want bool
	}{
		{"nothing filtered", commandMenu{}, false},
		{"filtered but dismissed", commandMenu{filtered: []menuItem{{value: "/clear"}}, dismissed: true}, false},
		{"filtered and not dismissed", commandMenu{filtered: []menuItem{{value: "/clear"}}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.mm.open(); got != c.want {
				t.Errorf("open() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCommandMenuHeightCapsAtMaxMenuItems(t *testing.T) {
	filtered := make([]menuItem, maxMenuItems+5)
	mm := commandMenu{filtered: filtered}

	if got := mm.height(); got != maxMenuItems {
		t.Errorf("height() = %d, want it capped at %d", got, maxMenuItems)
	}
}
