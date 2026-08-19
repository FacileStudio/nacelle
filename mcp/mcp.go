// Package mcp describes the MCP servers an agent may reach.
//
// It holds configuration and credentials, and deliberately implements none of
// the protocol: the Claude API connects to a remote MCP server itself, so the
// client half would be a reimplementation of something the model already does
// on the other side of the request.
package mcp

import "fmt"

// Server is one MCP server the model may call tools on.
type Server struct {
	// Name is how the model refers to this server, and how the toolset
	// entry in the request finds it. It must be unique in one agent.
	Name string

	// URL is the server's endpoint.
	URL string

	// Token is sent as the bearer credential. Leave it empty for a server
	// that needs none. It is read from configuration rather than written
	// into one: a token in a struct literal is a token in git.
	Token string

	// AllowedTools restricts what the model may call on this server. Empty
	// allows every tool the server exposes.
	//
	// Worth setting for any server that can write. The narrowest list that
	// does the job is the one that survives the server growing a
	// destructive tool later without anyone here noticing.
	AllowedTools []string
}

// Validate refuses a server list that cannot work.
//
// Duplicate names are the interesting case: the toolset entry in a request
// finds a server by name, so two servers sharing one makes the second
// unreachable in a way that looks like the server being down.
func Validate(servers []Server) error {
	seen := make(map[string]bool, len(servers))
	for _, server := range servers {
		switch {
		case server.Name == "":
			return fmt.Errorf("nacelle/mcp: a server has no name")
		case server.URL == "":
			return fmt.Errorf("nacelle/mcp: server %q has no URL", server.Name)
		case seen[server.Name]:
			return fmt.Errorf("nacelle/mcp: two servers are named %q", server.Name)
		}
		seen[server.Name] = true
	}
	return nil
}
