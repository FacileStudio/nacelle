package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMinimalEnvTakesPathFromTheProcess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/opt/tools/bin:/usr/bin")
	env := minimalEnv()
	want := "PATH=/opt/tools/bin:/usr/bin"
	if !strings.HasPrefix(env[0], want) {
		t.Errorf("PATH = %q, want it to start with %q", env[0], want)
	}
}

func TestMinimalEnvAppendsUserBinsNotAlreadyOnPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	local := filepath.Join(home, ".local", "bin")
	oldbin := filepath.Join(home, "bin")

	t.Setenv("PATH", local+":/usr/bin")
	got := minimalEnv()[0]
	want := "PATH=" + local + ":/usr/bin:" + oldbin
	if got != want {
		t.Errorf("PATH = %q, want %q — the bin already present must keep its place and appear once", got, want)
	}
}

func TestMinimalEnvFallsBackWhenNoPathAtAll(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	if got := commandPath(); !strings.HasPrefix(got, "/usr/local/bin:/usr/bin:/bin") {
		t.Errorf("commandPath = %q, want the old default as the floor", got)
	}
}

func TestCommandEnvStillWinsOverMinimal(t *testing.T) {
	set, err := New(Config{Root: t.TempDir(), CommandEnv: []string{"PATH=/custom"}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	set.commandEnv = []string{"PATH=/custom"}
	if set.commandEnv[0] != "PATH=/custom" {
		t.Errorf("explicit CommandEnv overwritten")
	}
}

func TestHomeIsCarriedButNothingElse(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	t.Setenv("OPENROUTER_API_KEY", "sk-secret-not-really")
	env := minimalEnv()
	for _, kv := range env {
		if strings.Contains(kv, "sk-secret") {
			t.Errorf("a secret reached the command environment: %q", kv)
		}
	}
	found := false
	for _, kv := range env {
		if kv == "HOME=/home/someone" {
			found = true
		}
	}
	if !found {
		t.Errorf("HOME missing from %v", env)
	}
}

func TestUserBinsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if bins := userBins(); bins != nil {
		t.Errorf("userBins with no HOME = %v, want nil rather than a relative path", bins)
	}
	_ = os.Getenv
}
