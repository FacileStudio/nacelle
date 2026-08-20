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
func fromEnv() Config {
	return Config{
		Backend:       os.Getenv(EnvPrefix + "BACKEND"),
		Model:         os.Getenv(EnvPrefix + "MODEL"),
		Effort:        os.Getenv(EnvPrefix + "EFFORT"),
		Root:          os.Getenv(EnvPrefix + "ROOT"),
		System:        os.Getenv(EnvPrefix + "SYSTEM"),
		Bash:          envBool(EnvPrefix + "BASH"),
		Thinking:      envBool(EnvPrefix + "THINKING"),
		ApproveTools:  envBool(EnvPrefix + "APPROVE_TOOLS"),
		MaxIterations: envInt(EnvPrefix + "MAX_ITERATIONS"),
		SkillDirs:     envList(EnvPrefix + "SKILL_DIRS"),
		Discovery: Discovery{
			Jardin:         envBool(EnvPrefix + "JARDIN"),
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
