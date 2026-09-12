package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type elevationCase struct {
	command string
	want    string
}

func elevationCases() []elevationCase {
	return []elevationCase{
		{"sudo ls", "sudo"},
		{"ls; sudo x", "sudo"},
		{"ls && sudo x", "sudo"},
		{"ls || sudo x", "sudo"},
		{"ls | sudo x", "sudo"},
		{"ls & sudo x", "sudo"},
		{"go build;sudo x", "build;sudo"},
		{"env sudo x", "sudo"},
		{"\\sudo", "sudo"},
		{"/usr/bin/sudo ls", "/usr/bin/sudo"},
		{"sudo -S x < f", "sudo"},
		{"$(sudo x)", "sudo"},
		{"`sudo x`", "sudo"},
		{"doas ls", "doas"},
		{"su -c 'whoami'", "su"},
		{"pkexec sh", "pkexec"},
		{"docker -u root x", "docker"},
		{"machinectl shell x", "machinectl"},
		{"ksu -e x", "ksu"},
		{"setpriv --reuid=0 x", "setpriv"},
		{"runuser -u root x", "runuser"},
		{"nsenter -t 1 x", "nsenter"},
		{"unshare -m x", "unshare"},
		{"chroot /tmp x", "chroot"},
	}
}

func newElevationSet(t *testing.T, deny bool) *Set {
	t.Helper()
	set, err := New(Config{Root: t.TempDir(), AllowBash: true, DenyElevation: deny})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { set.Close() })
	return set
}

func TestDenyElevationBlocksElevationCommands(t *testing.T) {
	set := newElevationSet(t, true)

	for _, tc := range elevationCases() {
		out, err := set.run(context.Background(), tc.command, DefaultCommandTimeout, nil)
		if err == nil {
			t.Errorf("%q ran under DenyElevation", tc.command)
			continue
		}
		if out != "" {
			t.Errorf("denying %q still produced output %q", tc.command, out)
		}
		if !strings.Contains(err.Error(), "privilege elevation") {
			t.Errorf("%q denied with %v, want a privilege elevation error", tc.command, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q denied with %v, want the offending token %q", tc.command, err, tc.want)
		}
	}
}

func TestDenyElevationGate(t *testing.T) {
	off := newElevationSet(t, false)
	out, err := off.run(context.Background(), "echo sudo", DefaultCommandTimeout, nil)
	if err != nil {
		t.Errorf("DenyElevation off blocked a harmless command: %v", err)
	}
	if strings.TrimSpace(out) != "sudo" {
		t.Errorf("out = %q, want the command to have run", out)
	}

	on := newElevationSet(t, true)
	out, err = on.run(context.Background(), "echo sudo", DefaultCommandTimeout, nil)
	if err == nil || !strings.Contains(err.Error(), "privilege elevation") {
		t.Errorf("DenyElevation on let %q through: %q, %v", "echo sudo", out, err)
	}
}

func TestDenyElevationOffRunsAHarmlessCommand(t *testing.T) {
	set := newElevationSet(t, false)

	out, err := set.run(context.Background(), "echo harmless", DefaultCommandTimeout, nil)
	if err != nil {
		t.Errorf("DenyElevation off blocked a harmless command: %v", err)
	}
	if strings.TrimSpace(out) != "harmless" {
		t.Errorf("out = %q, want the command to have run", out)
	}
}

func TestDenyElevationHasNoChainLengthCutoff(t *testing.T) {
	deny := newElevationSet(t, true)
	parts := make([]string, 0, 121)
	for i := 1; i <= 120; i++ {
		parts = append(parts, fmt.Sprintf("cmd%d", i))
	}
	parts = append(parts, "sudo x")
	command := strings.Join(parts, "; ")

	out, err := deny.run(context.Background(), command, DefaultCommandTimeout, nil)
	if err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Errorf("the 120-command chain was not denied: %q, %v", out, err)
	}

	plain := newElevationSet(t, false)
	parts[len(parts)-1] = "true"
	plainCommand := strings.Join(parts, "; ")
	out, err = plain.run(context.Background(), plainCommand, DefaultCommandTimeout, nil)
	if err != nil || strings.Contains(out, "timed out") {
		t.Errorf("DenyElevation off did not run the chain: %q, %v", out, err)
	}
}

func TestSetuidRootBinaryIsDenied(t *testing.T) {
	deny := newElevationSet(t, true)
	path := setuidRootBinary(t)

	out, err := deny.run(context.Background(), path, DefaultCommandTimeout, nil)
	if err == nil || !strings.Contains(err.Error(), "setuid root") {
		t.Errorf("%s is setuid root: deny = %q, %v; want a setuid root error", path, out, err)
	}
}

func TestSetuidRootDetectorFindsTheBinary(t *testing.T) {
	path := setuidRootBinary(t)
	if !setuidRoot(path) {
		t.Errorf("setuidRoot(%s) = false, want the detector to recognise it", path)
	}
}

func TestSetuidCheckSkipsNonSetuid(t *testing.T) {
	set := newElevationSet(t, true)
	src, err := exec.LookPath("true")
	if err != nil {
		t.Fatalf("LookPath true: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	path := filepath.Join(t.TempDir(), "true-copy")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("writing the copy: %v", err)
	}
	if err := os.Chmod(path, 0o4755); err != nil {
		t.Fatalf("setting the setuid bit: %v", err)
	}

	out, err := set.run(context.Background(), path, DefaultCommandTimeout, nil)
	if err != nil {
		t.Fatalf("a setuid copy owned by this user was denied: %v", err)
	}
	if out != "" && out != "(no output)" {
		t.Errorf("the copy printed %q", out)
	}
}

func setuidRootBinary(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		"/usr/bin/passwd",
		"/usr/bin/chsh",
		"/usr/bin/chfn",
		"/usr/bin/gpasswd",
		"/usr/bin/newgrp",
		"/usr/bin/mount",
		"/usr/bin/umount",
		"/usr/bin/fusermount3",
	} {
		if setuidRoot(candidate) {
			return candidate
		}
	}
	t.Skip("no setuid-root binary on this system to test against")
	return ""
}
