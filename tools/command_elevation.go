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

func checkCommandElevation(command string) error {
	for field := range strings.FieldsSeq(command) {
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
