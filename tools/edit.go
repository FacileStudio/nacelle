package tools

import (
	"bytes"
	"fmt"
	"strings"
)

// replace swaps old for new, requiring exactly one match unless all is set,
// in which case every occurrence is replaced and at least one must exist.
//
// The uniqueness rule is the whole safety property of this tool. A match count
// of zero means the model is editing a file it has misremembered, and a count
// above one means it is about to change places it never looked at — both are
// caught here, at no cost, where the alternative is a silent corruption the
// next reader inherits. Refusing also teaches the model the fix: send more
// surrounding context.
func replace(content, old, new string, all bool) (string, error) {
	if old == "" {
		return "", fmt.Errorf("the text to replace is empty; use write_file to create a file")
	}
	if old == new {
		return "", fmt.Errorf("the replacement is identical to the original")
	}

	switch count := strings.Count(content, old); {
	case count > 0 && all:
		return strings.ReplaceAll(content, old, new), nil
	case count == 1:
		return strings.Replace(content, old, new, 1), nil
	case count == 0:
		return "", fmt.Errorf("that exact text is not in the file%s", nearMiss(content, old))
	default:
		return "", fmt.Errorf("that text appears %d times; include more surrounding lines so it matches only the one you mean, or set replace_all", count)
	}
}

// nearMiss explains a failed match when the cause is likely whitespace.
//
// A model that has retyped a line rather than copying it usually differs only
// in indentation, and "not found" sends it round the same loop again.
func nearMiss(content, old string) string {
	if strings.Contains(squeeze(content), squeeze(old)) {
		return " — the text is there but the whitespace differs; copy it exactly as read_file showed it, without the line-number column"
	}
	return ""
}

// squeeze collapses every run of whitespace so two texts can be compared
// ignoring layout.
func squeeze(text string) string {
	return string(bytes.Join(bytes.Fields([]byte(text)), []byte(" ")))
}
