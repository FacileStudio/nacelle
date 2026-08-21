package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
)

// Load reads server definitions from files in the mcpServers format, the one
// Claude Code, Claude Desktop, Cursor and the rest of this ecosystem already
// write.
//
// Reading somebody else's format rather than inventing one is the whole
// point. A person adopting this client has these files already, and the
// alternative is asking them to keep a second copy of the same list in a
// nacelle-shaped syntax — which is the duplication -skill-dir exists to
// avoid for skills, arrived at from the same direction.
//
//	{
//	  "mcpServers": {
//	    "git":  {"command": "mcp-server-git", "args": ["--repo", "."]},
//	    "docs": {"type": "http", "url": "https://mcp.example.com/mcp",
//	             "headers": {"Authorization": "Bearer ${DOCS_TOKEN}"}}
//	  }
//	}
//
// Later files win. Passing a personal list and then a project's does what the
// order says, which is the precedence every client in this ecosystem gives
// its own scopes, and the servers come back sorted by name so that two runs
// over one configuration build the same tool set in the same order.
func Load(paths ...string) ([]Server, error) {
	merged := map[string]entry{}
	for _, path := range paths {
		if path == "" {
			return nil, fmt.Errorf(
				"nacelle/mcp/client: one of the config paths is empty, which is what an unset " +
					"shell variable expands to — check the argument that was meant to name a file")
		}
		found, err := read(path)
		if err != nil {
			return nil, err
		}
		maps.Copy(merged, found)
	}

	servers := make([]Server, 0, len(merged))
	for _, name := range slices.Sorted(maps.Keys(merged)) {
		if merged[name].Disabled {
			continue
		}
		server, err := merged[name].server(name)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

// entry is one server as the mcpServers format spells it. Both halves are on
// one struct because that is how the format is written, not because they
// belong together — server() sorts them out into the two types that do.
//
// cwd and disabled are here because other clients write them and this one has
// somewhere to put them: a working directory, and a server left in the file
// but switched off. Every other key those clients invent is refused rather
// than ignored, and the permission-shaped ones — autoApprove, alwaysAllow —
// are the reason that is the right way round. Ignoring a key whose whole
// purpose is to narrow what a server may do would leave someone believing
// they had restricted it. This client spells that AllowedTools, and says so
// rather than nodding along.
type entry struct {
	Type     string            `json:"type"`
	Command  string            `json:"command"`
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	Dir      string            `json:"cwd"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Disabled bool              `json:"disabled"`
}

// read decodes one file: tolerant about what surrounds mcpServers, strict
// about what is inside it.
//
// The split is the whole of the decision. These files are shared between
// clients and carry keys this one has no opinion on — "$schema", and VS
// Code's "inputs" among them — so refusing a file for a sibling key would
// reject configurations that are perfectly good everywhere else. Inside a
// server entry the calculus inverts: there are six keys, a misspelling of one
// silently produces a server started without the environment or the arguments
// it needed, and the failure surfaces as a tool that misbehaves rather than as
// a file that is wrong. So "comand" is an error that names itself, and the
// error names the server as well as the file, because these objects look
// alike and a person with nine of them needs to know which.
func read(path string) (map[string]entry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("nacelle/mcp/client: reading %s: %w", path, err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("nacelle/mcp/client: reading %s: %w", path, err)
	}
	declared, ok := top["mcpServers"]
	if !ok {
		return nil, fmt.Errorf("nacelle/mcp/client: %s has no mcpServers object", path)
	}

	var named map[string]json.RawMessage
	if err := json.Unmarshal(declared, &named); err != nil {
		return nil, fmt.Errorf("nacelle/mcp/client: reading the mcpServers object in %s: %w", path, err)
	}

	servers := make(map[string]entry, len(named))
	for name, blob := range named {
		decoder := json.NewDecoder(bytes.NewReader(blob))
		decoder.DisallowUnknownFields()
		var declaration entry
		if err := decoder.Decode(&declaration); err != nil {
			return nil, fmt.Errorf("nacelle/mcp/client: %s: server %q: %w", path, name, err)
		}
		servers[name] = declaration
	}
	return servers, nil
}

// server turns one entry into the type its transport calls for.
//
// An absent type means stdio, which is what every client here does and what
// the great majority of these files rely on. A url with no type is refused
// rather than guessed at: reading it as http would make a file that works
// here and nowhere else, and the fix is one key the error names.
func (e entry) server(name string) (Server, error) {
	if !callable.MatchString(name) {
		return nil, fmt.Errorf(
			"nacelle/mcp/client: server %q cannot be named to the model, which allows only %s", name, callable)
	}

	switch e.Type {
	case "", "stdio":
		return e.command(name)
	case "http", "streamableHttp", "streamable-http":
		return e.remote(name)
	case "sse", "ws":
		return nil, fmt.Errorf(
			"nacelle/mcp/client: server %q asks for the %s transport, which this client does not speak — "+
				"MCP replaced it with Streamable HTTP, so try \"type\": \"http\" against the same URL", name, e.Type)
	default:
		return nil, fmt.Errorf("nacelle/mcp/client: server %q asks for an unknown transport %q", name, e.Type)
	}
}

// command builds the stdio half, and says so when the entry is plainly a
// remote one that forgot to say which transport it wanted.
func (e entry) command(name string) (Server, error) {
	if e.Command == "" && e.URL != "" {
		return nil, fmt.Errorf(
			"nacelle/mcp/client: server %q has a url and no type, which reads as a stdio server with no "+
				"executable — add \"type\": \"http\"", name)
	}

	path, err := expand(name, e.Command)
	if err != nil {
		return nil, err
	}
	args, err := expandAll(name, e.Args)
	if err != nil {
		return nil, err
	}
	env, err := expandMap(name, e.Env)
	if err != nil {
		return nil, err
	}
	dir, err := expand(name, e.Dir)
	if err != nil {
		return nil, err
	}
	return Command{Name: name, Path: path, Args: args, Env: env, Dir: dir}, nil
}

// remote builds the HTTP half.
func (e entry) remote(name string) (Server, error) {
	endpoint, err := expand(name, e.URL)
	if err != nil {
		return nil, err
	}
	headers, err := expandMap(name, e.Headers)
	if err != nil {
		return nil, err
	}
	return Remote{Name: name, URL: endpoint, Headers: headers}, nil
}
