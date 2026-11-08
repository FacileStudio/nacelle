package client

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/FacileStudio/nacelle"
)

// A server that marks a tool readOnlyHint lets the loop run it beside other
// read-only calls; one that says nothing gets no such trust.
func TestReadOnlyHintBecomesAReadOnlySpec(t *testing.T) {
	answer := func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return text("ok"), nil, nil
	}
	mount := func(server *sdk.Server) {
		sdk.AddTool(server, &sdk.Tool{Name: "look", Description: "Look.", Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, answer)
		sdk.AddTool(server, &sdk.Tool{Name: "touch", Description: "Touch."}, answer)
	}
	tools := bridgeTools(t, Command{Name: "s"}, mount)

	if !find(t, tools, "s_look").Spec().ReadOnly {
		t.Error("a tool the server marked readOnlyHint is not read-only")
	}
	if find(t, tools, "s_touch").Spec().ReadOnly {
		t.Error("a tool with no annotation was trusted as read-only")
	}
}

// Every annotation reaches the Spec, resolved against the protocol defaults:
// a tool that says nothing is destructive and open-world.
func TestAnnotationsBecomeHints(t *testing.T) {
	answer := func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return text("ok"), nil, nil
	}
	closed := false
	mount := func(server *sdk.Server) {
		sdk.AddTool(server, &sdk.Tool{Name: "file", Description: "File.", Annotations: &sdk.ToolAnnotations{
			DestructiveHint: &closed, IdempotentHint: true, OpenWorldHint: &closed,
		}}, answer)
		sdk.AddTool(server, &sdk.Tool{Name: "send", Description: "Send."}, answer)
	}
	tools := bridgeTools(t, Command{Name: "s"}, mount)

	if got, want := *find(t, tools, "s_file").Spec().Hints, (nacelle.Hints{Idempotent: true}); got != want {
		t.Errorf("s_file hints = %+v, want %+v", got, want)
	}
	if got, want := *find(t, tools, "s_send").Spec().Hints, (nacelle.Hints{Destructive: true, OpenWorld: true}); got != want {
		t.Errorf("s_send hints = %+v, want the protocol defaults %+v", got, want)
	}
}
