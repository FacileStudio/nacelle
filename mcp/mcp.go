// Package mcp describes the MCP servers an agent may reach.
//
// It holds configuration and credentials, and deliberately implements none of
// the protocol: the Claude API connects to a remote MCP server itself, so the
// client half would be a reimplementation of something the model already does
// on the other side of the request.
package mcp

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
