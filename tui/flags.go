package main

import (
	"flag"
	"strings"
)

// pathList collects one -skill-dir or -mcp flag per occurrence. flag.String
// would only keep the last one; this is the standard flag.Value escape hatch
// for a flag meant to repeat.
//
// It was dirList until -mcp arrived naming a file rather than a directory. One
// type for both is not tidiness: the two flags have to agree on what repeating
// means, and a second copy of these four lines is where they would quietly
// stop agreeing.
type pathList []string

func (p *pathList) String() string     { return strings.Join(*p, ":") }
func (p *pathList) Set(v string) error { *p = append(*p, v); return nil }

// declared is every flag this command accepts, holding the pointer `flag`
// fills each one in through — kept together so fromFlags can hand the whole
// set to typedSetters rather than threading ten variables through it by hand.
//
// The embedded groups mirror the ones Config has, for the reason Config has
// them: every field on them is still reached as f.mycelium or f.effort, not as
// f.discoveryFlags.mycelium, and grouping only keeps this struct's own field
// count from growing by one every time one of those lists does.
type declared struct {
	backend, model, root, system *string
	reasoningFlags
	webFlags
	bash, approveTools *bool
	iterations         *int
	sourceFlags
	discoveryFlags
}

// reasoningFlags is declared's half of Reasoning, one flag pointer per field
// there.
type reasoningFlags struct {
	effort   *string
	thinking *bool
	budget   *int64
}

// webFlags is declared's half of Web — one flag pointer per field there.
type webFlags struct {
	search *string
	fetch  *bool
}

// sourceFlags is declared's half of Sources — one flag pointer per field
// there, and both of them repeat.
type sourceFlags struct {
	skillDirs, mcp *pathList
}

// discoveryFlags is declared's half of Discovery — one flag pointer per
// field there.
type discoveryFlags struct {
	mycelium, projectContext, skills, trustSkills *bool
}

// declareFlags registers every flag against fallback's values and returns
// where `flag.Parse` will leave its answers.
func declareFlags(fallback Config) declared {
	return declared{
		sourceFlags:    declareSources(),
		backend:        flag.String("backend", fallback.Backend, "anthropic or openrouter"),
		model:          flag.String("model", fallback.Model, "model id, defaulting to the backend's own"),
		root:           flag.String("root", fallback.Root, "directory the file tools may reach"),
		system:         flag.String("system", fallback.System, "system prompt"),
		reasoningFlags: declareReasoning(fallback),
		webFlags:       declareWeb(fallback),
		bash:           flag.Bool("bash", *fallback.Bash, "let the model run commands"),
		approveTools: flag.Bool("approve-tools", *fallback.ApproveTools,
			"ask before every tool call runs, y/a/n; off by default, every call runs unasked"),
		iterations: flag.Int("max-iterations", *fallback.MaxIterations, "how many times the model may be asked"),
		discoveryFlags: discoveryFlags{
			mycelium: flag.Bool("mycelium", *fallback.Mycelium,
				"let the model run mycelium flows and search its memory, when mycelium is installed"),
			projectContext: flag.Bool("project-context", *fallback.ProjectContext,
				"read CLAUDE.md and AGENTS.md from root upward into the system prompt"),
			skills: flag.Bool("skills", *fallback.Skills,
				"tell the model about skills found in ~/.agents/skills and trusted .agents/skills directories"),
			trustSkills: flag.Bool("trust-skills", *fallback.TrustSkills,
				"trust every .agents/skills directory found under root this run, and remember the decision"),
		},
	}
}

// declareSources registers the two flags that name somewhere to read from,
// kept out of declareFlags for the reason declareWeb is.
//
// Neither is seeded from fallback, unlike every flag above it. Set appends, so
// a pre-filled list would grow rather than be replaced the first time the flag
// was typed, and the layer underneath is merged in by settings() anyway —
// which is the only place a config file's own list is supposed to arrive from.
func declareSources() sourceFlags {
	skillDirs, mcp := new(pathList), new(pathList)
	flag.Var(skillDirs, "skill-dir",
		"extra directory to load skills from, alongside ~/.agents/skills (repeatable); "+
			"e.g. -skill-dir ~/.claude/skills to see another tool's skills without moving them")
	flag.Var(mcp, "mcp",
		"file of MCP servers to start and hand the model the tools of (repeatable); it reads the same "+
			"mcpServers files every other client already has, e.g. -mcp ~/.claude/.mcp.json")
	return sourceFlags{skillDirs: skillDirs, mcp: mcp}
}

// declareReasoning registers the three settings that decide how hard the model
// thinks, kept out of declareFlags for the reason declareWeb is.
//
// -effort and -reasoning-budget are two spellings of one idea rather than a
// choice between them, and typing both is not a contradiction to resolve here:
// the backends disagree about which they accept, so each sends the one its own
// API understands and ignores the other.
func declareReasoning(fallback Config) reasoningFlags {
	return reasoningFlags{
		effort:   flag.String("effort", fallback.Effort, "none, minimal, low, medium, high, xhigh or max"),
		thinking: flag.Bool("thinking", *fallback.Thinking, "stream the model's reasoning"),
		budget: flag.Int64("reasoning-budget", *fallback.Budget,
			"tokens one turn may spend on reasoning; 0 sets no ceiling and leaves the backend's own default"),
	}
}

// declareWeb registers the two network settings, kept out of declareFlags so
// that function stays inside the length the gate allows.
func declareWeb(fallback Config) webFlags {
	return webFlags{
		search: flag.String("search", *fallback.Search,
			"base URL of a SearXNG instance to search the web through; empty means no web search"),
		fetch: flag.Bool("fetch", *fallback.Fetch,
			"let the model read a web page by URL; on by default"),
	}
}

// typedSetters maps each flag's name to what it does to a Config, which is
// the shape flag.Visit wants: it reports flag names, not values, and a name
// on its own cannot say where in Config it belongs.
func typedSetters(f declared) map[string]func(*Config) {
	return map[string]func(*Config){
		"backend":          func(c *Config) { c.Backend = *f.backend },
		"model":            func(c *Config) { c.Model = *f.model },
		"effort":           func(c *Config) { c.Effort = *f.effort },
		"root":             func(c *Config) { c.Root = *f.root },
		"system":           func(c *Config) { c.System = *f.system },
		"search":           func(c *Config) { c.Search = f.search },
		"fetch":            func(c *Config) { c.Fetch = f.fetch },
		"bash":             func(c *Config) { c.Bash = f.bash },
		"thinking":         func(c *Config) { c.Thinking = f.thinking },
		"mycelium":           func(c *Config) { c.Mycelium = f.mycelium },
		"project-context":  func(c *Config) { c.ProjectContext = f.projectContext },
		"skills":           func(c *Config) { c.Skills = f.skills },
		"trust-skills":     func(c *Config) { c.TrustSkills = f.trustSkills },
		"approve-tools":    func(c *Config) { c.ApproveTools = f.approveTools },
		"max-iterations":   func(c *Config) { c.MaxIterations = f.iterations },
		"reasoning-budget": func(c *Config) { c.Budget = f.budget },
		"skill-dir":        func(c *Config) { c.SkillDirs = []string(*f.skillDirs) },
		"mcp":              func(c *Config) { c.MCP = []string(*f.mcp) },
	}
}

// fromFlags is the settings layer the command line supplies.
//
// Only the flags actually typed are collected. Go's flag package cannot tell a
// flag left alone from one passed its own default value, so Visit — which
// reports exactly the ones that were set — is what stops a default from
// silently outranking the config file it is supposed to sit beneath.
func fromFlags() Config {
	f := declareFlags(defaults())
	flag.Parse()
	typed := typedSetters(f)

	var flags Config
	flag.Visit(func(flg *flag.Flag) {
		if take, known := typed[flg.Name]; known {
			take(&flags)
		}
	})
	return flags
}
