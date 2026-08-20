package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// The dropdown has to get out of the way once the name is spelled out in
// full, because while it is up it wins enter ahead of ask(): a typed-out
// "/clear" answered the first enter by re-filling the prompt with what was
// already there and only sent on the second.
func TestRefreshMenuClosesOnceACommandIsTypedOutInFull(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/clear")

	m.refreshMenu()

	if m.menu.open() {
		t.Errorf("filtered = %+v, want the menu closed with nothing left to complete", m.menu.filtered)
	}
}

// One more character is back to filtering, so closing on an exact match
// never hides a longer name that happens to start with a shorter one.
func TestRefreshMenuReopensWhenTypingPastAnExactMatch(t *testing.T) {
	m := sized()
	m.menu.items = []menuItem{{value: "/skill:review"}, {value: "/skill:review-pr"}}

	m.prompt.SetValue("/skill:review")
	m.refreshMenu()
	if m.menu.open() {
		t.Fatal("menu stayed open on an exact match")
	}

	m.prompt.SetValue("/skill:review-")
	m.refreshMenu()
	if !m.menu.open() {
		t.Error("menu did not come back once the line no longer named a candidate outright")
	}
}

// key() is where the two paths actually meet: with the menu closed, enter
// belongs to ask() again and the line goes.
func TestKeyEnterSendsAFullyTypedCommandInsteadOfRepickingIt(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/clear")
	m.refreshMenu()

	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := m.prompt.Value(); got != "" {
		t.Errorf("prompt = %q after enter, want the command sent rather than picked again", got)
	}
}
