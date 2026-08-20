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
// rather than being told: jardin's tools, project and global context,
// skills. See Config's own doc comment for why it is a separate type at
// all.
type Discovery struct {
	Jardin         *bool `yaml:"jardin"`
	ProjectContext *bool `yaml:"project_context"`
	Skills         *bool `yaml:"skills"`
	TrustSkills    *bool `yaml:"trust_skills"`
}
