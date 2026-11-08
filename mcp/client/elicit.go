package client

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Elicitation is a server asking for a person's input before it finishes a
// call: on MCP 2026-07-28 a tools/call answered input_required, on older
// versions an elicitation/create request sent mid-call.
//
// Server is the configured server name and Tool the server's own name for
// the tool being called, with the Arguments the model sent. Request is what
// the server asked, with its message and the schema an answer must match.
// Tool and Arguments are empty when an older server asks outside a call this
// client can attribute it to.
type Elicitation struct {
	Server    string
	Tool      string
	Arguments map[string]any
	Request   *sdk.ElicitParams
}

// Answer is the person's reply. The zero value declines, so a host that
// cannot reach a person refuses by default. Content is the accepted form's
// values and must match Request.RequestedSchema.
type Answer struct {
	Accept  bool
	Content map[string]any
}

// Elicit answers one [Elicitation]. The call it belongs to is replayed with
// the answer and the server decides what a decline means, usually that the
// action does not happen. An error fails the call.
type Elicit func(ctx context.Context, request Elicitation) (Answer, error)

type callKey struct{}

// inflight is the call an elicitation belongs to, carried on the context of
// tools/call so the SDK's multi-round-trip retry hands it back to elicitor.
type inflight struct {
	tool      string
	arguments map[string]any
}

// withCall marks ctx as belonging to one call of tool.
func withCall(ctx context.Context, tool string, arguments map[string]any) context.Context {
	return context.WithValue(ctx, callKey{}, inflight{tool: tool, arguments: arguments})
}

// elicitor adapts a host's [Elicit] to the SDK's elicitation handler, which
// the SDK calls for each input request and then retries the call with every
// answer as inputResponses.
func elicitor(server string, hook Elicit) func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
	return func(ctx context.Context, request *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
		call, _ := ctx.Value(callKey{}).(inflight)
		answer, err := hook(ctx, Elicitation{Server: server, Tool: call.tool, Arguments: call.arguments, Request: request.Params})
		if err != nil {
			return nil, err
		}
		if !answer.Accept {
			return &sdk.ElicitResult{Action: "decline"}, nil
		}
		return &sdk.ElicitResult{Action: "accept", Content: answer.Content}, nil
	}
}
