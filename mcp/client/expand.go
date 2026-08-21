package client

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// expand resolves ${VAR} and ${VAR:-default} in one field.
//
// Only the braced spelling is a reference. os.Expand would also read a bare
// $word, and these fields carry passwords, connection strings and grep
// patterns — a literal dollar in one of those is far more likely than an
// intended reference without braces, and silently eating it produces a
// credential that is wrong in a way nothing prints.
//
// A reference with no value and no default is an error rather than an empty
// string, which is where this deliberately parts company with the clients
// whose format this is. Expanding an unset ${TOKEN} to nothing sends
// "Authorization: Bearer " and buys a 401 that names neither the variable nor
// the file. Empty on purpose has a spelling of its own: ${TOKEN:-}.
func expand(server, text string) (string, error) {
	var out strings.Builder
	missing := map[string]bool{}

	for {
		open := strings.Index(text, "${")
		if open < 0 {
			break
		}
		end := strings.Index(text[open:], "}")
		if end < 0 {
			break
		}

		out.WriteString(text[:open])
		out.WriteString(resolve(text[open+2:open+end], missing))
		text = text[open+end+1:]
	}
	out.WriteString(text)

	if len(missing) > 0 {
		return "", fmt.Errorf(
			"nacelle/mcp/client: server %q refers to %s, which %s not set — give %s a value, or a default with ${NAME:-fallback}",
			server, strings.Join(slices.Sorted(maps.Keys(missing)), ", "),
			plural(len(missing), "is", "are"), plural(len(missing), "it", "them"))
	}
	return out.String(), nil
}

// resolve reads one reference, recording a name it cannot answer for.
//
// Unset and empty are the same case, which is what ":-" means everywhere it
// is borrowed from: a variable exported as the empty string is a variable
// somebody forgot to fill in, not a deliberate empty value.
func resolve(reference string, missing map[string]bool) string {
	name, fallback, hasDefault := strings.Cut(reference, ":-")
	if value := os.Getenv(name); value != "" {
		return value
	}
	if hasDefault {
		return fallback
	}
	missing[name] = true
	return ""
}

// plural picks the word that agrees with a count, so the error reads as a
// sentence whether one variable is missing or four.
func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// expandAll resolves every item of a list, keeping nil nil so that an absent
// args array does not become an empty one the server has to ignore.
func expandAll(server string, items []string) ([]string, error) {
	if items == nil {
		return nil, nil
	}
	expanded := make([]string, 0, len(items))
	for _, item := range items {
		resolved, err := expand(server, item)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, resolved)
	}
	return expanded, nil
}

// expandMap resolves every value, leaving the keys alone: a variable named by
// a variable is a puzzle nobody asked this format to solve.
func expandMap(server string, items map[string]string) (map[string]string, error) {
	if items == nil {
		return nil, nil
	}
	expanded := make(map[string]string, len(items))
	for key, value := range items {
		resolved, err := expand(server, value)
		if err != nil {
			return nil, err
		}
		expanded[key] = resolved
	}
	return expanded, nil
}
