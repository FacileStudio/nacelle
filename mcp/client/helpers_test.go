package client

import (
	"context"
	"errors"
	"maps"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/FacileStudio/nacelle"
)

// The variables the subprocess half of these tests speaks through.
//
// helperEnv is what turns this test binary into an MCP server, secretEnv is
// the sentinel the parent sets and the child must never see, tokenEnv is what
// Command.Env is expected to deliver, and pidEnv is how a child that is about
// to be killed tells the test which process to look for afterwards.
const (
	helperEnv  = "NACELLE_MCP_HELPER"
	secretEnv  = "NACELLE_MCP_SECRET"
	tokenEnv   = "NACELLE_MCP_TOKEN"
	pidEnv     = "NACELLE_MCP_PIDFILE"
	helperName = "helper"
)

// TestMain re-execs this binary as a real MCP server over stdio.
//
// The os/exec helper-process pattern, one rung earlier than usual: the branch
// is taken in TestMain rather than in a TestHelperProcess, because that is
// before flag parsing and before -test.paniconexit0 is armed, so the child's
// os.Exit(0) is an exit rather than a panic. It also means the child needs no
// -test.run argument, and so cannot be confused by the flags a developer
// happened to run `go test` with.
//
// Committing a second binary would test less: the point of this path is that
// the environment handling and the reaping work against a process that really
// was forked, and a fixture binary would still have to be built by something.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "" {
		os.Exit(m.Run())
	}
	if err := serveHelper(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// serveHelper is the MCP server the subprocess tests connect to.
//
// It returns nil on the ordinary ending, which is the parent closing stdin, so
// a non-zero exit here is a real failure and the test that started it sees one
// rather than a server that quietly stopped answering.
func serveHelper() error {
	if path := os.Getenv(pidEnv); path != "" {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			return err
		}
	}
	server := sdk.NewServer(&sdk.Implementation{Name: helperName, Version: "0"}, nil)
	register(server)
	return server.Run(context.Background(), &sdk.StdioTransport{})
}

type echoInput struct {
	Text string `json:"text" jsonschema:"what to echo back"`
}

// register mounts the tools both halves of these tests share, so that the
// in-memory server and the subprocess one answer identically and a difference
// between them is a difference in the transport rather than in the fixture.
func register(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "Echo the text back."},
		func(_ context.Context, _ *sdk.CallToolRequest, in echoInput) (*sdk.CallToolResult, any, error) {
			return text(in.Text), nil, nil
		})
	sdk.AddTool(server, &sdk.Tool{Name: "environment", Description: "Report two variables."},
		func(_ context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
			return text(secretEnv + "=" + os.Getenv(secretEnv) + " " + tokenEnv + "=" + os.Getenv(tokenEnv)), nil, nil
		})
}

// text is the one-block result most fixture tools return.
func text(body string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: body}}}
}

// serve connects a client to an in-memory server, which is how everything
// except the environment and the reaping is tested: no process, no ports, and
// a real protocol handshake either way.
//
// The wait on the way out is what keeps the server's goroutines from
// outliving the test and turning up in another one's race report. Its error is
// dropped rather than asserted on, because a client that hangs up mid-call —
// which the timeout test does on purpose — ends the server session with an EOF
// that is the expected ending and not a failure.
func serve(t *testing.T, mount func(*sdk.Server)) *sdk.ClientSession {
	t.Helper()

	serverSide, clientSide := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	mount(server)

	serverSession, err := server.Connect(t.Context(), serverSide, nil)
	if err != nil {
		t.Fatalf("connecting the server: %v", err)
	}
	clientSession, err := sdk.NewClient(&sdk.Implementation{Name: "nacelle", Version: "0"}, nil).
		Connect(t.Context(), clientSide, nil)
	if err != nil {
		t.Fatalf("connecting the client: %v", err)
	}
	t.Cleanup(func() {
		defer func() { _ = serverSession.Wait() }()
		if err := clientSession.Close(); err != nil {
			t.Errorf("closing the client session: %v", err)
		}
	})
	return clientSession
}

// bridgeTools is serve plus the bridge, for the tests that only care about
// what came out the far end.
func bridgeTools(t *testing.T, command Command, mount func(*sdk.Server)) []nacelle.Tool {
	t.Helper()

	built, err := bridge(t.Context(), serve(t, mount), command, DefaultCallTimeout, map[string]bool{})
	if err != nil {
		t.Fatalf("bridge(%q) = %v, want it to succeed", command.Name, err)
	}
	return built
}

// find returns the bridged tool by its composed name.
func find(t *testing.T, tools []nacelle.Tool, name string) nacelle.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("no tool named %q in %v", name, names(tools))
	return nil
}

// names is what a failure message prints instead of a slice of interfaces.
func names(tools []nacelle.Tool) []string {
	found := make([]string, 0, len(tools))
	for _, tool := range tools {
		found = append(found, tool.Name())
	}
	return found
}

// blocking is a server whose one tool never answers, for the timeout tests.
//
// It gives up after a few seconds rather than waiting on ctx alone, so that a
// cancellation this package failed to propagate costs a failed test instead of
// a hung suite.
func blocking(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "wait", Description: "Never answers."},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			return text("answered after all"), nil, nil
		})
}

// helperCommand points a Command at this very test binary, which TestMain
// turns into an MCP server.
func helperCommand(t *testing.T, name string, env map[string]string) Command {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("finding the test binary: %v", err)
	}
	full := map[string]string{helperEnv: "1"}
	maps.Copy(full, env)
	return Command{Name: name, Path: executable, Env: full, Timeout: 30 * time.Second}
}

// pidOf reads the pid a helper subprocess recorded on its way up.
func pidOf(t *testing.T, path string) int {
	t.Helper()

	recorded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the pid file: %v", err)
	}
	pid, err := strconv.Atoi(string(recorded))
	if err != nil {
		t.Fatalf("parsing the pid %q: %v", recorded, err)
	}
	return pid
}

// waitGone reports whether a pid has been reaped, which is what "Close did not
// leak a process" means concretely: signal zero finds nothing to signal.
func waitGone(pid int) bool {
	for range 100 {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
