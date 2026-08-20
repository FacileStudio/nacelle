package main

import (
	"sort"
	"strings"
)

// maxMenuItems caps how many matches the dropdown draws at once. A cap
// keeps the layout math in resize.go/layout a fixed, cheap thing to
// recompute on every keystroke rather than a number that changes with
// however many skills happen to be loaded.
const maxMenuItems = 8

// menuItem is one row the dropdown can offer: what typing it out in full
// and pressing enter would do, and — for a skill, never the three built-in
// commands — a one-line hint of what it does.
type menuItem struct {
	value       string
	description string
}

// commandMenu is the dropdown's own state: every candidate it could ever
// offer, built once at construction, and what the current prompt narrows
// that down to. dismissed is Escape's doing — cleared the moment the line
// no longer starts with '/', so it never disables the menu for the rest of
// the session, only for the command being typed right now.
type commandMenu struct {
	items     []menuItem
	filtered  []menuItem
	selected  int
	dismissed bool
}

// open reports whether the dropdown has anything to show right now.
func (mm commandMenu) open() bool {
	return !mm.dismissed && len(mm.filtered) > 0
}

// height is how many rows open() actually draws — capped the same way
// filtered's own display slice is, so layout() and viewMenu() can never
// disagree about how tall the dropdown is.
func (mm commandMenu) height() int {
	if !mm.open() {
		return 0
	}
	return min(len(mm.filtered), maxMenuItems)
}

// menuItems is the dropdown's full candidate pool: the client's own
// commands first — there are only ever three, worth seeing before a longer
// skill list — then every loaded skill, each carrying its own description.
func menuItems(skills map[string]skill) []menuItem {
	names := commandNames()
	skillNames := skillCommandNames(skills)
	items := make([]menuItem, 0, len(names)+len(skillNames))
	for _, name := range names {
		items = append(items, menuItem{value: name})
	}
	for _, name := range skillNames {
		items = append(items, menuItem{
			value:       name,
			description: skills[strings.TrimPrefix(name, "/skill:")].description,
		})
	}
	return items
}

// filterMenu narrows items to what query matches, ranked best first. An
// empty query — just "/" typed — matches everything, in menuItems' own
// order, so the dropdown opens showing the full list rather than nothing.
func filterMenu(items []menuItem, query string) []menuItem {
	if query == "" {
		return items
	}

	type scored struct {
		item menuItem
		rank int
	}
	matches := make([]scored, 0, len(items))
	for _, it := range items {
		if rank := matchRank(it.value, query); rank >= 0 {
			matches = append(matches, scored{it, rank})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].rank < matches[j].rank })

	out := make([]menuItem, len(matches))
	for i, s := range matches {
		out[i] = s.item
	}
	return out
}

// matchRank scores how well candidate matches query, lower is a better
// match and -1 means no match at all. A prefix beats a plain substring
// beats an order-preserving scatter of the same characters — the ranking a
// typed "/skill:rev" needs to put "/skill:review" ahead of
// "/skill:hunk-review" without hiding the second one entirely.
func matchRank(candidate, query string) int {
	c, q := strings.ToLower(candidate), strings.ToLower(query)
	switch {
	case strings.HasPrefix(c, q):
		return 0
	case strings.Contains(c, q):
		return 1
	case fuzzyMatch(c, q):
		return 2
	default:
		return -1
	}
}

// fuzzyMatch reports whether every byte of query appears in candidate, in
// order, not necessarily contiguous — the same rule fzf and most command
// palettes use, and the reason "/skill:rev" finds "/skill:hunk-review" at
// all: a plain prefix or substring check never would. Byte indexing is safe
// here specifically because both a command name and a skill's name (the
// Agent Skills specification's own rule) are constrained to lowercase
// ASCII, digits and hyphens — never a multi-byte rune.
func fuzzyMatch(candidate, query string) bool {
	i := 0
	for j := 0; j < len(candidate) && i < len(query); j++ {
		if candidate[j] == query[i] {
			i++
		}
	}
	return i == len(query)
}

// typedOut reports whether query already spells one of the candidates out in
// full, leaving the dropdown nothing left to complete.
//
// A list that stays up after the whole name is typed is not merely
// redundant, it eats the next keypress: the menu wins enter ahead of ask(),
// so a fully typed "/clear" answers the first enter by re-filling the prompt
// with what is already in it and only sends on the second. Closing on an
// exact match is what makes typing a command out and pressing enter do the
// obvious thing.
//
// One more character reopens it, which is what keeps this from hiding a
// longer name that starts with a shorter one — "/skill:review" closes,
// "/skill:review-" is back to filtering.
func typedOut(items []menuItem, query string) bool {
	for _, it := range items {
		if it.value == query {
			return true
		}
	}
	return false
}
