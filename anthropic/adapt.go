package anthropic

import (
	"slices"
	"strings"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// sortedByName is tools in the one order every caller of this package's tool
// schema must agree on. Tools render first in the request, which makes them
// the front of every cached prefix — two agents configured with the same
// tools in a different order would share no cache at all, and nothing in the
// result would say why. tokens.go's count has to sort them the same way, or a
// count taken in a different order counts a different prefix than the one the
// run that follows it will actually send.
func sortedByName(tools []nacelle.Tool) []nacelle.Tool {
	return slices.SortedFunc(slices.Values(tools), func(a, b nacelle.Tool) int {
		return strings.Compare(a.Name(), b.Name())
	})
}

// toolParams renders nacelle tools in the shape the streaming request takes.
//
// This is the declaration, not the execution: the API sees the name,
// description and schema, and the agent loop this package owns executes the
// call by looking up the same name in a nacelle.ToolsByName index.
func toolParams(tools []nacelle.Tool) []sdk.BetaToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	params := make([]sdk.BetaToolUnionParam, 0, len(tools))
	for _, tool := range sortedByName(tools) {
		params = append(params, sdk.BetaToolUnionParam{
			OfTool: &sdk.BetaToolParam{
				Name:        tool.Name(),
				Description: sdk.String(tool.Description()),
				InputSchema: toolInputSchema(tool),
			},
		})
	}
	return params
}

// toolInputSchema renders a tool's schema in the shape the SDK wants, whether
// the caller is the request itself or tokens.go asking what it would cost to
// declare it — neither reads anything on the tool but its name, description
// and schema, so the same conversion serves both.
func toolInputSchema(tool nacelle.Tool) sdk.BetaToolInputSchemaParam {
	schema := tool.Schema()
	input := sdk.BetaToolInputSchemaParam{}
	if properties, ok := schema["properties"]; ok {
		input.Properties = properties
	}
	if required, ok := schema["required"].([]any); ok {
		names := make([]string, 0, len(required))
		for _, name := range required {
			if text, ok := name.(string); ok {
				names = append(names, text)
			}
		}
		input.Required = names
	}
	return input
}
