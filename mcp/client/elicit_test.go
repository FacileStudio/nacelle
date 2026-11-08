package client

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sendInput struct {
	To string `json:"to"`
}

// confirming is a tool that sends only once a person accepted, through MRTR
// input_required on 2026-07-28 and the SDK's legacy shim before it.
func confirming(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "send", Description: "Send a mail."}, send)
}

// send asks for a confirmation until the call carries one.
func send(_ context.Context, request *sdk.CallToolRequest, in sendInput) (*sdk.CallToolResult, any, error) {
	reply, ok := request.Params.InputResponses["confirm"].(*sdk.ElicitResult)
	if !ok {
		ask := &sdk.ElicitParams{Message: "Send to " + in.To + "?"}
		return &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"confirm": ask}}, nil, nil
	}
	if reply.Action != "accept" {
		return text("kept as a draft"), nil, nil
	}
	return text("sent to " + in.To), nil, nil
}

// asked records every elicitation a host was handed.
type asked struct {
	mu   sync.Mutex
	seen []Elicitation
}

func (a *asked) hook(answer Answer) Elicit {
	return func(_ context.Context, request Elicitation) (Answer, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.seen = append(a.seen, request)
		return answer, nil
	}
}

// sendWith calls app_send through a host hook that gives answer.
func sendWith(t *testing.T, config stub, answer Answer) (string, *asked) {
	t.Helper()

	endpoint, _ := serveStub(t, config, confirming)
	host := &asked{}
	tools := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint, Elicit: host.hook(answer)}).Tools()
	reply, err := callTool(t.Context(), find(t, tools, "app_send"), json.RawMessage(`{"to":"ana"}`))
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	return reply, host
}

// An accepted confirmation replays the call with inputResponses, and the host
// saw the server, the tool, the arguments and the server's question.
func TestAnApprovedConfirmationRunsTheCall(t *testing.T) {
	reply, host := sendWith(t, stub{}, Answer{Accept: true})

	if reply != "sent to ana" {
		t.Errorf("Run = %q, want the send to go through", reply)
	}
	if len(host.seen) != 1 {
		t.Fatalf("the host was asked %d times, want once", len(host.seen))
	}
	got := host.seen[0]
	if got.Server != "app" || got.Tool != "send" || got.Arguments["to"] != "ana" || got.Request.Message != "Send to ana?" {
		t.Errorf("elicitation = %+v, want server, tool, arguments and message", got)
	}
}

// A decline goes back to the server, which decides what it means.
func TestADeclinedConfirmationReachesTheServer(t *testing.T) {
	for _, config := range []stub{{}, {stateful: true}} {
		if reply, _ := sendWith(t, config, Answer{}); reply != "kept as a draft" {
			t.Errorf("Run = %q, want the server's answer to a decline", reply)
		}
	}
}

// Elicitation is advertised only by a client whose host can answer it, which
// is what tells a server to take its no-confirmation path otherwise.
func TestElicitationIsAdvertisedOnlyWithAHook(t *testing.T) {
	probe := func(server *sdk.Server) {
		sdk.AddTool(server, &sdk.Tool{Name: "can", Description: "Report elicitation support."},
			func(_ context.Context, request *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
				capabilities := request.ClientCapabilities()
				return text(strconv.FormatBool(capabilities != nil && capabilities.Elicitation != nil)), nil, nil
			})
	}
	endpoint, _ := serveStub(t, stub{}, probe)
	for want, hook := range map[string]Elicit{"true": (&asked{}).hook(Answer{}), "false": nil} {
		tools := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint, Elicit: hook}).Tools()
		if got, err := callTool(t.Context(), find(t, tools, "app_can"), nil); err != nil || got != want {
			t.Errorf("elicitation advertised = %q, %v, want %s", got, err, want)
		}
	}
}
