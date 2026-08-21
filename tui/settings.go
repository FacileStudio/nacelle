package main

// Web is what this client may reach over the network on the model's behalf.
type Web struct {
	// Search is the base URL of a SearXNG instance the model may query,
	// and empty means no web search at all — the default, since any
	// instance shipped as one would be somebody else's machine.
	//
	// A pointer for the reason the toggles are, and not for the reason the
	// plain strings are not: empty is a real answer here, so a layer
	// saying "no search this run" and a layer saying nothing about search
	// are different instructions. As a plain string they were the same
	// one, and `-search ""` was a silent no-op.
	Search *string `yaml:"search"`

	// Fetch lets the model read one web page by URL.
	//
	// On by default, unlike bash and unlike search. It cannot change
	// anything, it reaches only the public internet — see
	// tools.WebFetch — and it is what makes a search result more than a
	// sentence. Off is for the case the guard cannot cover: a fetched page
	// is written by a stranger and read by the model as instructions, so
	// it can ask for another URL with something from the conversation in
	// its query string. With bash on that channel already exists; with
	// bash off, this is it.
	Fetch *bool `yaml:"fetch"`
}

// Discovery is every toggle for something this client finds on its own
// rather than being told: mycelium's tools, project and global context,
// skills. See Config's own doc comment for why it is a separate type at
// all.
type Discovery struct {
	Mycelium         *bool `yaml:"mycelium"`
	ProjectContext *bool `yaml:"project_context"`
	Skills         *bool `yaml:"skills"`
	TrustSkills    *bool `yaml:"trust_skills"`
}

// Sources is every list of paths this client only ever reads because somebody
// named it: another tool's skills folder, another tool's MCP server list.
//
// It is Discovery's other half rather than a second copy of it. Discovery is
// switches for what nacelle goes looking for on its own; nothing in here is
// found, so nothing in here needs a switch — leaving the list empty is already
// off. It is a type of its own for the mechanical reason Config's own doc
// comment gives, and for the readable one that the two halves answer different
// questions about a launch.
type Sources struct {
	// SkillDirs are directories of skills read alongside ~/.agents/skills.
	SkillDirs []string `yaml:"skill_dirs"`

	// MCP are files in the mcpServers format — .mcp.json and the rest of
	// its siblings — whose servers are started at launch so the model can
	// call their tools like any other.
	//
	// A list rather than one path, and read in the order given, because
	// client.Load merges the files it is handed by server name with the
	// later one winning. A personal list in ~/.nacelle.yml and a project's
	// named with -mcp therefore layer the way every client in this
	// ecosystem layers its own scopes, instead of one silently erasing the
	// other.
	//
	// That is also why this is the one list Config.merge accumulates
	// instead of replacing, where SkillDirs alongside it is replaced. A
	// second -skill-dir is another place to look and the flag can sensibly
	// stand in for the file's list; a second -mcp is another server, and
	// having it silently switch off the nine already configured would be
	// the opposite of what typing it means.
	MCP []string `yaml:"mcp"`
}
