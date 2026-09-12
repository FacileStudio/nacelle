package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
)

var escapeCommands = map[string]struct{}{
	"cd":       {},
	"pushd":    {},
	"popd":     {},
	"dirs":     {},
	"hash":     {},
	"history":  {},
	"type":     {},
	"command":  {},
	"exec":     {},
	"eval":     {},
	"source":   {},
	".":        {},
	"set":      {},
	"alias":    {},
	"unalias":  {},
	"export":   {},
	"readonly": {},
	"local":    {},
	"declare":  {},
	"typeset":  {},
	"ulimit":   {},
	"umask":    {},
	"trap":     {},
	"bash":     {},
	"sh":       {},
	"zsh":      {},
	"csh":      {},
	"tcsh":     {},
	"ksh":      {},
	"fish":     {},
}

var elevationCommands = map[string]struct{}{
	"sudo":       {},
	"doas":       {},
	"su":         {},
	"pkexec":     {},
	"docker":     {},
	"nsenter":    {},
	"unshare":    {},
	"chroot":     {},
	"setpriv":    {},
	"runuser":    {},
	"machinectl": {},
	"ksu":        {},
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

func checkCommandElevation(command string) error {
	for _, field := range strings.Fields(command) {
		if elevates(field) {
			return fmt.Errorf("%q attempts privilege elevation and is blocked (security.deny_elevation): you lack permission to raise privileges; do not retry this command — report the blocker in your result and finish", field)
		}
		if setuidRoot(field) {
			return fmt.Errorf("%q attempts privilege elevation (setuid root) and is blocked (security.deny_elevation): you lack permission to raise privileges; do not retry this command — report the blocker in your result and finish", field)
		}
	}
	return nil
}

func elevates(field string) bool {
	for _, word := range elevationWords(field) {
		if _, ok := elevationCommands[filepath.Base(word)]; ok {
			return true
		}
	}
	return false
}

func elevationWords(field string) []string {
	return strings.FieldsFunc(field, func(r rune) bool {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			return false
		case r == '_', r == '.', r == '/':
			return false
		default:
			return true
		}
	})
}

func setuidRoot(field string) bool {
	path := field
	if !strings.Contains(field, "/") {
		found, err := exec.LookPath(field)
		if err != nil {
			return false
		}
		path = found
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode()&os.ModeSetuid != 0 && sys.Uid == 0
}
