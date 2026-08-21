package client

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The namespace is unconditional. A tool named for the server it came from
// only whenever a second server happens to be configured is a tool whose name
// depends on someone else's configuration, and a system prompt naming it is
// then right in one deployment and wrong in the next.
func TestEveryToolIsNamespacedByItsServer(t *testing.T) {
	tools := bridgeTools(t, Command{Name: "helper"}, register)

	got := names(tools)
	want := map[string]bool{"helper_echo": true, "helper_environment": true}
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %d of them", got, len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("tool %q, want one of %v", name, want)
		}
	}
}

// The allow-list is the narrow-it-down control mcp.Server.AllowedTools already
// documents, and it has to mean the same thing here or an operator moving a
// server from remote to stdio silently widens what the model may call.
func TestAnAllowListLeavesEverythingElseBehind(t *testing.T) {
	tools := bridgeTools(t, Command{Name: "helper", AllowedTools: []string{"echo"}}, register)

	if got := names(tools); len(got) != 1 || got[0] != "helper_echo" {
		t.Errorf("tools = %v, want only helper_echo", got)
	}
}

// Truncating instead would map two tools onto one name, which is how a model
// calls the read tool and gets the write one. The refusal has to name the
// tool, because that is the only way an operator knows which server to rename.
func TestAComposedNameTooLongForTheAPIsIsRefusedRatherThanCut(t *testing.T) {
	server := strings.Repeat("s", 61)

	_, err := bridge(t.Context(), serve(t, register), details{name: server, timeout: DefaultCallTimeout}, map[string]bool{})
	if err == nil {
		t.Fatal("bridge accepted a name of 66 characters")
	}
	if !strings.Contains(err.Error(), server+"_echo") && !strings.Contains(err.Error(), server+"_environment") {
		t.Errorf("error = %q, want it to name the tool it refused", err)
	}
}

// A dot is legal in MCP and illegal in both model APIs, so a server whose
// name carries one produces tools no request will accept. Catching it here
// costs a startup error; not catching it costs a 400 mid-run.
func TestANameTheModelAPIsWouldRejectIsRefused(t *testing.T) {
	_, err := bridge(t.Context(), serve(t, register), details{name: "my.server", timeout: DefaultCallTimeout}, map[string]bool{})
	if err == nil {
		t.Fatal("bridge accepted a name containing a dot")
	}
}

// Two servers can compose to one name without either being wrong on its own:
// server "a_b" with tool "c" and server "a" with tool "b_c" both arrive as
// a_b_c. nacelle.validateTools would catch it later, but by then the error
// names a tool and not the servers that produced it.
func TestTwoServersComposingToTheSameToolNameAreRefused(t *testing.T) {
	taken := map[string]bool{}
	mount := func(server *sdk.Server) {
		sdk.AddTool(server, &sdk.Tool{Name: "c", Description: "First."},
			func(_ context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
				return text("first"), nil, nil
			})
	}
	if _, err := bridge(t.Context(), serve(t, mount), details{name: "a_b", timeout: DefaultCallTimeout}, taken); err != nil {
		t.Fatalf("bridging the first server: %v", err)
	}

	second := func(server *sdk.Server) {
		sdk.AddTool(server, &sdk.Tool{Name: "b_c", Description: "Second."},
			func(_ context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
				return text("second"), nil, nil
			})
	}
	_, err := bridge(t.Context(), serve(t, second), details{name: "a", timeout: DefaultCallTimeout}, taken)
	if err == nil {
		t.Fatal("bridge accepted two tools composing to a_b_c")
	}
	if !strings.Contains(err.Error(), "a_b_c") {
		t.Errorf("error = %q, want it to name the collision", err)
	}
}

// Description is what the model chooses a tool by, and a server that fills in
// only Title has still written the sentence.
func TestATitleStandsInForAMissingDescription(t *testing.T) {
	mount := func(server *sdk.Server) {
		sdk.AddTool(server, &sdk.Tool{Name: "titled", Title: "Look something up."},
			func(_ context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
				return text("ok"), nil, nil
			})
	}
	tools := bridgeTools(t, Command{Name: "s"}, mount)

	if got := find(t, tools, "s_titled").Description(); got != "Look something up." {
		t.Errorf("Description() = %q, want the title", got)
	}
}

// The happy path: arguments the model wrote reach the server, and the text it
// returned reaches the model.
func TestArgumentsReachTheServerAndTheAnswerComesBack(t *testing.T) {
	tools := bridgeTools(t, Command{Name: "helper"}, register)

	got, err := find(t, tools, "helper_echo").Run(t.Context(), json.RawMessage(`{"text":"round trip"}`))
	if err != nil {
		t.Fatalf("Run = %v, want it to succeed", err)
	}
	if got != "round trip" {
		t.Errorf("Run = %q, want %q", got, "round trip")
	}
}

// nacelle.Tool.Run documents an error as something handed back to the model,
// which is exactly what a tool error is for: the model wrote the arguments and
// is usually the one who can fix them.
func TestAToolErrorComesBackAsAGoError(t *testing.T) {
	mount := func(server *sdk.Server) {
		sdk.AddTool(server, &sdk.Tool{Name: "fail", Description: "Always fails."},
			func(_ context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
				return nil, nil, errors.New("the record does not exist")
			})
	}
	tools := bridgeTools(t, Command{Name: "s"}, mount)

	got, err := find(t, tools, "s_fail").Run(t.Context(), nil)
	if err == nil {
		t.Fatalf("Run = %q, want an error", got)
	}
	if !strings.Contains(err.Error(), "the record does not exist") {
		t.Errorf("error = %q, want the server's message in it", err)
	}
}

// Arguments that are not an object are refused here rather than by the server,
// so the model reads a message written for it.
func TestArgumentsThatAreNotAnObjectAreRefusedBeforeTheCall(t *testing.T) {
	tools := bridgeTools(t, Command{Name: "helper"}, register)

	if _, err := find(t, tools, "helper_echo").Run(t.Context(), json.RawMessage(`["round trip"]`)); err == nil {
		t.Fatal("Run accepted a JSON array as arguments")
	}
}

// A server that accepts a call and never answers must not hold the agent's
// goroutine for the life of the process. The whole run would otherwise read as
// a model that stopped thinking.
func TestAServerThatNeverAnswersLosesTheCallAndNotTheAgent(t *testing.T) {
	built, err := bridge(t.Context(), serve(t, blocking), details{name: "s", timeout: 50 * time.Millisecond}, map[string]bool{})
	if err != nil {
		t.Fatalf("bridge = %v, want it to succeed", err)
	}

	waiting := find(t, built, "s_wait")
	done := make(chan error, 1)
	go func() {
		_, err := waiting.Run(context.Background(), nil)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Run = %v, want it to report the deadline", err)
		}
		if !strings.Contains(err.Error(), "s_wait") {
			t.Errorf("Run = %q, want the model to be told which tool went quiet", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run outlived a 50ms timeout by five seconds")
	}
}
