package client

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Validation runs before a single process is started, so a typo costs a clear
// error rather than a half-built tree of subprocesses that then has to be torn
// down to report it.
func TestACommandListIsRefusedBeforeAnythingIsStarted(t *testing.T) {
	for name, commands := range map[string][]Server{
		"no name": {Command{Path: "/usr/bin/true"}},
		"no executable": {
			Command{Name: "docs", Path: "/usr/bin/true"},
			Command{Name: "tickets"},
		},
		"two servers sharing a name": {
			Command{Name: "docs", Path: "/usr/bin/true"},
			Command{Name: "docs", Path: "/usr/bin/false"},
		},
		"a remote with no URL": {Remote{Name: "docs"}},
		"a remote speaking a scheme this client does not": {
			Remote{Name: "docs", URL: "ws://example.invalid/mcp"},
		},
		"a remote and a command sharing a name": {
			Command{Name: "docs", Path: "/usr/bin/true"},
			Remote{Name: "docs", URL: "https://example.invalid/mcp"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			set, err := Connect(t.Context(), commands...)
			if err == nil {
				t.Fatalf("Connect accepted a command list it should have refused; Close = %v", set.Close())
			}
			if set != nil {
				t.Errorf("Connect returned a set alongside %v, want nil", err)
			}
		})
	}
}

// An empty list is what every agent configuring no MCP server at all takes on
// every run, so it has to be a working no-op rather than an error.
func TestConnectingToNothingIsASetWithNoTools(t *testing.T) {
	set, err := Connect(t.Context())
	if err != nil {
		t.Fatalf("Connect() = %v, want nil", err)
	}
	defer func() { _ = set.Close() }()

	if got := set.Tools(); len(got) != 0 {
		t.Errorf("Tools() = %v, want none", names(got))
	}
}

// The whole path against a process that really was forked: stdin and stdout,
// the handshake, pagination, and a call whose arguments and answer cross a
// pipe rather than a channel.
func TestARealSubprocessServerIsStartedAndItsToolsWork(t *testing.T) {
	set, err := Connect(t.Context(), helperCommand(t, "helper", nil))
	if err != nil {
		t.Fatalf("Connect = %v, want it to succeed", err)
	}
	defer func() { _ = set.Close() }()

	got, err := find(t, set.Tools(), "helper_echo").Run(t.Context(), json.RawMessage(`{"text":"over a pipe"}`))
	if err != nil {
		t.Fatalf("Run = %v, want it to succeed", err)
	}
	if got != "over a pipe" {
		t.Errorf("Run = %q, want %q", got, "over a pipe")
	}
}

// Close is the only thing that will ever reap these processes, so a server
// still running after it is a leak that grows with every agent the host
// builds.
func TestCloseLeavesNoSubprocessBehind(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")

	set, err := Connect(t.Context(), helperCommand(t, "helper", map[string]string{pidEnv: pidFile}))
	if err != nil {
		t.Fatalf("Connect = %v, want it to succeed", err)
	}
	pid := pidOf(t, pidFile)

	if err := set.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	if !waitGone(pid) {
		t.Errorf("process %d is still running after Close", pid)
	}
}

// The failure that makes this worth a test: Connect returns nil, so the
// caller's own deferred Close never receives a handle, and every server that
// did start is orphaned. Failing loudly is only half of it — the other half is
// leaving nothing running.
func TestOneServerFailingToStartTakesDownTheOnesAlreadyRunning(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")

	set, err := Connect(t.Context(),
		helperCommand(t, "helper", map[string]string{pidEnv: pidFile}),
		Command{Name: "missing", Path: filepath.Join(t.TempDir(), "not-an-executable")},
	)
	if err == nil {
		t.Fatalf("Connect succeeded with a server that cannot start; Close = %v", set.Close())
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %q, want it to name the server that failed", err)
	}
	if !waitGone(pidOf(t, pidFile)) {
		t.Error("the first server is still running after Connect failed")
	}
}
