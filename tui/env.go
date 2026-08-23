package main

import (
	"os"
	"strconv"
	"strings"
)

// EnvPrefix is what every setting's environment variable starts with.
const EnvPrefix = "NACELLE_"

// fromEnv is the settings layer the environment supplies.
//
// These are overrides and nothing else. The suite has already shipped the
// alternative once: a CLI read its variables only inside a development branch
// that ignored the config file entirely, so what its README called overrides
// were really a second, mutually exclusive mode nobody could tell apart from
// the first.
//
// Sources.MCP is the one setting with no variable of its own, and that is a
// decision rather than an omission. It names files, and the two places a
// person writes a file path down already cover both halves of the question:
// ~/.nacelle.yml for the servers wanted every run, -mcp for the ones wanted
// this one. A third spelling would be a third thing to document and a third
// place to look when a server did not start.
func fromEnv() Config {
	return Config{
		Backend:       os.Getenv(EnvPrefix + "BACKEND"),
		Model:         os.Getenv(EnvPrefix + "MODEL"),
		Root:          os.Getenv(EnvPrefix + "ROOT"),
		System:        os.Getenv(EnvPrefix + "SYSTEM"),
		Bash:          envBool(EnvPrefix + "BASH"),
		ApproveTools:  envBool(EnvPrefix + "APPROVE_TOOLS"),
		MaxIterations: envInt(EnvPrefix + "MAX_ITERATIONS"),
		Sources:       Sources{SkillDirs: envList(EnvPrefix + "SKILL_DIRS")},
		Reasoning: Reasoning{
			Effort:   os.Getenv(EnvPrefix + "EFFORT"),
			Thinking: envBool(EnvPrefix + "THINKING"),
			Budget:   envInt64(EnvPrefix + "REASONING_BUDGET"),
		},
		Web: Web{
			Search: envString(EnvPrefix + "SEARCH"),
			Fetch:  envBool(EnvPrefix + "FETCH"),
		},
		Discovery: Discovery{
			Mycelium:         envBool(EnvPrefix + "MYCELIUM"),
			ProjectContext: envBool(EnvPrefix + "PROJECT_CONTEXT"),
			Skills:         envBool(EnvPrefix + "SKILLS"),
			TrustSkills:    envBool(EnvPrefix + "TRUST_SKILLS"),
		},
	}
}

// envBool reads a toggle, returning nil when the variable is unset or is not
// something strconv recognises.
//
// A misspelt value is treated as unmentioned rather than as false: the setting
// then falls through to the layer below, which is what the writer of
// NACELLE_BASH=yes meant, and a silent false would have been the opposite.
func envBool(name string) *bool {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}
	return &value
}

// envString reads a setting whose empty value means something, returning nil
// only when the variable is genuinely absent.
//
// This is where it parts company with envBool and envInt, which treat an empty
// value as unset: an empty bool says nothing, but NACELLE_SEARCH= is a person
// saying "not this run" and has to outrank the config file rather than fall
// through to it.
func envString(name string) *string {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	return &raw
}

// envInt reads a count, with the same treatment of an unreadable value.
func envInt(name string) *int {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

// envInt64 is envInt in the width a token count is measured in.
//
// Two of these rather than one generic reader because they answer to two
// different fields: MaxIterations counts requests and is an int, the reasoning
// budget counts tokens and is the int64 nacelle.Thinking carries. Narrowing to
// int here only to widen again at the call site is a conversion that silently
// loses a ceiling on a 32-bit build, which is exactly the kind of bug nobody
// finds by reading the config file back.
func envInt64(name string) *int64 {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

// envList reads a colon-separated list, the same separator PATH itself uses
// for the same kind of value, returning nil rather than a one-element slice
// holding "" when the variable is unset or empty.
func envList(name string) []string {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return nil
	}
	return strings.Split(raw, ":")
}
