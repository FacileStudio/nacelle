package client

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
