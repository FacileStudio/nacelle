package main

// merge overwrites every setting the layer above actually mentions, and leaves
// the rest alone. SkillDirs reads "mentioned" the same way mergeStrings reads
// an empty string: nothing yet lets a layer clear it on purpose, so an empty
// slice only ever means a layer that never mentioned it.
//
// MCP is the one list that accumulates rather than replaces — Hooks now
// accumulates with it, for the same reason. Sources.MCP is
// where the reason is written down, because it is a fact about what that list
// is for and not about how layers are resolved.
func (c *Config) merge(over Config) {
	c.mergeStrings(over)
	c.mergeToggles(over)
	if over.MaxIterations != nil {
		c.MaxIterations = over.MaxIterations
	}
	if over.Budget != nil {
		c.Budget = over.Budget
	}
	if over.Search != nil {
		c.Search = over.Search
	}
	if over.Fetch != nil {
		c.Fetch = over.Fetch
	}
	if len(over.SkillDirs) > 0 {
		c.SkillDirs = over.SkillDirs
	}
	c.MCP = append(c.MCP, over.MCP...)
	c.Hooks = append(c.Hooks, over.Hooks...)
}

// mergeStrings overwrites every string setting over actually mentions. Empty
// is "not mentioned" for these — none has a meaningful empty value, which is
// exactly what makes a string safe to use unlike the toggles below.
func (c *Config) mergeStrings(over Config) {
	if over.Backend != "" {
		c.Backend = over.Backend
	}
	if over.Model != "" {
		c.Model = over.Model
	}
	if over.Effort != "" {
		c.Effort = over.Effort
	}
	if over.Root != "" {
		c.Root = over.Root
	}
	if over.System != "" {
		c.System = over.System
	}
}

// mergeToggles overwrites every *bool setting over actually mentions. These
// stay pointers rather than joining mergeStrings' plain-value treatment
// because a layer saying nothing and a layer saying false are different
// answers a bool cannot tell apart.
func (c *Config) mergeToggles(over Config) {
	if over.Bash != nil {
		c.Bash = over.Bash
	}
	if over.Thinking != nil {
		c.Thinking = over.Thinking
	}
	if over.Mycelium != nil {
		c.Mycelium = over.Mycelium
	}
	if over.ProjectContext != nil {
		c.ProjectContext = over.ProjectContext
	}
	if over.Skills != nil {
		c.Skills = over.Skills
	}
	if over.TrustSkills != nil {
		c.TrustSkills = over.TrustSkills
	}
	if over.TrustHooks != nil {
		c.TrustHooks = over.TrustHooks
	}
	if over.ApproveTools != nil {
		c.ApproveTools = over.ApproveTools
	}
}
