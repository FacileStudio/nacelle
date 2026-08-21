package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/FacileStudio/nacelle"
)

// What both model APIs will accept as a tool name.
//
// Anthropic and OpenAI validate against the same pattern, so a dot is illegal
// and sixty-four characters is the ceiling — and MCP allows both, which is why
// this has to be checked on the way in. Doing it here turns a 400 from a
// vendor halfway through a run into a refusal at startup that names the tool.
// The length is stated twice on purpose: the pattern is the contract, and the
// constant is what lets the error say how far over the name is instead of
// leaving an operator to count characters.
const maxNameLength = 64

var callable = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// bridge turns everything one server offers into nacelle tools.
//
// The session's iterator is used rather than a single ListTools because it
// follows the cursor: a server with more tools than fit in one page would
// otherwise expose only the first page, and the missing tools would look like
// a server that does not have them.
//
// taken carries across servers, so a collision between two servers is caught
// as readily as one inside a single server.
func bridge(ctx context.Context, session *sdk.ClientSession, command Command, timeout time.Duration, taken map[string]bool) ([]nacelle.Tool, error) {
	var bridged []nacelle.Tool
	for remote, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("nacelle/mcp/client: listing the tools of server %q: %w", command.Name, err)
		}
		if len(command.AllowedTools) > 0 && !slices.Contains(command.AllowedTools, remote.Name) {
			continue
		}

		name, err := compose(command.Name, remote.Name, taken)
		if err != nil {
			return nil, err
		}
		schema, err := schemaOf(remote.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("nacelle/mcp/client: tool %q: %w", name, err)
		}

		bridged = append(bridged, &tool{
			name:        name,
			remote:      remote.Name,
			description: describeTool(remote),
			schema:      schema,
			session:     session,
			timeout:     timeout,
		})
	}
	return bridged, nil
}

// compose namespaces one tool and refuses a name that cannot work.
//
// Nothing is truncated. A name cut to fit silently maps two distinct tools
// onto one, which is a reported production failure in awslabs/mcp and is
// worse than the refusal it was trying to avoid: the model calls what it
// thinks is the read tool and gets the write one. Renaming the server is a
// one-line fix, so the refusal names the length it needs.
func compose(server, remote string, taken map[string]bool) (string, error) {
	name := server + "_" + remote
	switch {
	case len(name) > maxNameLength:
		return "", fmt.Errorf("nacelle/mcp/client: tool %q is %d characters and the model APIs allow %d — rename the server", name, len(name), maxNameLength)
	case !callable.MatchString(name):
		return "", fmt.Errorf("nacelle/mcp/client: tool %q cannot be named to the model, which allows only %s — rename the server", name, callable)
	case taken[name]:
		return "", fmt.Errorf("nacelle/mcp/client: two tools are named %q", name)
	}
	taken[name] = true
	return name, nil
}

// describeTool is the description the model chooses this tool by.
//
// A server that supplies none leaves it empty rather than failing the
// connection, which is the one place this package does not refuse. The
// difference is who can fix it: a name collision or a bad allow-list is the
// operator's own configuration, and a missing description is a third party's
// metadata that no amount of failing loudly here will improve.
//
// Title is the fallback rather than the name, because a title is at least a
// sentence the server's author wrote for a human to read, and the name is what
// the model already has.
func describeTool(remote *sdk.Tool) string {
	if remote.Description != "" {
		return remote.Description
	}
	return remote.Title
}

// tool is one MCP tool presented as a [nacelle.Tool].
type tool struct {
	name        string
	remote      string
	description string
	schema      map[string]any
	session     *sdk.ClientSession
	timeout     time.Duration
}

func (t *tool) Name() string           { return t.name }
func (t *tool) Description() string    { return t.description }
func (t *tool) Schema() map[string]any { return t.schema }

// Run calls the tool on the server and flattens what comes back.
//
// The arguments are decoded into a map rather than forwarded as raw JSON, so
// that a model which produced an array or a bare string is told it did, here,
// instead of the server rejecting it with a message written for a different
// audience.
//
// The timeout is applied per call and not per session. A server is expected to
// be up for the life of the agent, and one slow tool must not be the reason
// every later call to a healthy server fails.
//
// An error result comes back as a Go error, which is exactly what
// nacelle.Tool.Run documents an error for: it is handed to the model, and the
// model is usually the one best placed to fix an argument and try again.
func (t *tool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	arguments := map[string]any{}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &arguments); err != nil {
			return "", fmt.Errorf("the arguments did not match the schema: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	result, err := t.session.CallTool(ctx, &sdk.CallToolParams{Name: t.remote, Arguments: arguments})
	if err != nil {
		return "", err
	}

	answer := flatten(result)
	if result.IsError {
		return "", errors.New(answer)
	}
	return answer, nil
}
