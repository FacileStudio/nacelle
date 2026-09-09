package tools

import (
	"fmt"
	"strings"
)

var escapeCommands = map[string]struct{}{
	"cd":         {},
	"pushd":      {},
	"popd":       {},
	"dirs":       {},
	"hash":       {},
	"history":    {},
	"type":       {},
	"command":    {},
	"exec":       {},
	"eval":       {},
	"source":     {},
	".":          {},
	"set":        {},
	"alias":      {},
	"unalias":    {},
	"export":     {},
	"readonly":   {},
	"local":      {},
	"declare":    {},
	"typeset":    {},
	"ulimit":     {},
	"umask":      {},
	"trap":       {},
	"bash":       {},
	"sh":         {},
	"zsh":        {},
	"csh":        {},
	"tcsh":       {},
	"ksh":        {},
	"fish":       {},
}

func checkCommandEscapes(command string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}

	if blocked(fields[0]) {
		return fmt.Errorf("%q is blocked under strict confinement", fields[0])
	}

	if cmd := chainedEscape(fields); cmd != "" {
		return fmt.Errorf("%q is blocked under strict confinement", cmd)
	}

	return nil
}

func blocked(first string) bool {
	_, ok := escapeCommands[first]
	return ok
}

func chainedEscape(fields []string) string {
	for _, field := range fields {
		if !isChainSeparator(field) {
			continue
		}

		next := nextAfter(fields, field)
		if next != "" && blocked(next) {
			return next
		}
	}
	return ""
}

func isChainSeparator(token string) bool {
	switch token {
	case ";", "&&", "||", "|", "&":
		return true
	}
	return false
}

func nextAfter(fields []string, token string) string {
	for i, field := range fields {
		if field == token && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}
