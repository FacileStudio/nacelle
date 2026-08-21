package client

import (
	"strings"
	"testing"
)

// The two spellings the format defines, and the one it does not.
func TestExpansionResolvesBracedReferencesOnly(t *testing.T) {
	t.Setenv("NACELLE_TEST_TOKEN", "sekrit")
	t.Setenv("NACELLE_TEST_EMPTY", "")

	for name, tc := range map[string]struct{ in, want string }{
		"a set variable":        {"${NACELLE_TEST_TOKEN}", "sekrit"},
		"inside a sentence":     {"Bearer ${NACELLE_TEST_TOKEN}!", "Bearer sekrit!"},
		"a default, unset":      {"${NACELLE_TEST_ABSENT:-fallback}", "fallback"},
		"a default, set empty":  {"${NACELLE_TEST_EMPTY:-fallback}", "fallback"},
		"an explicit empty":     {"${NACELLE_TEST_ABSENT:-}", ""},
		"a bare dollar is text": {"$NACELLE_TEST_TOKEN", "$NACELLE_TEST_TOKEN"},
		"a literal password":    {"p$$w0rd", "p$$w0rd"},
		"an unclosed brace":     {"${NACELLE_TEST_TOKEN", "${NACELLE_TEST_TOKEN"},
		"two references":        {"${NACELLE_TEST_TOKEN}/${NACELLE_TEST_TOKEN}", "sekrit/sekrit"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := expand("docs", tc.in)
			if err != nil {
				t.Fatalf("expand(%q) = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("expand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// An unset reference with no default is refused rather than expanded to
// nothing, because "Authorization: Bearer " buys a 401 that names neither the
// variable nor the file it came from.
func TestAnUnsetReferenceWithNoDefaultIsRefused(t *testing.T) {
	_, err := expand("docs", "${NACELLE_TEST_MISSING_A} ${NACELLE_TEST_MISSING_B}")
	if err == nil {
		t.Fatal("expand accepted a reference it could not resolve")
	}
	for _, want := range []string{"docs", "NACELLE_TEST_MISSING_A", "NACELLE_TEST_MISSING_B", "${NAME:-fallback}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expand = %v, want it to mention %q", err, want)
		}
	}
}

// Expansion has to reach the fields that carry credentials, not only the
// command, which is the entire reason a token is not written in the file.
func TestExpansionReachesArgsEnvAndHeaders(t *testing.T) {
	t.Setenv("NACELLE_TEST_TOKEN", "sekrit")
	t.Setenv("NACELLE_TEST_REPO", "/srv/repo")

	servers, err := Load(write(t, `{
	  "mcpServers": {
	    "git":  {"command": "/usr/bin/mcp-server-git", "args": ["--repo", "${NACELLE_TEST_REPO}"],
	             "env": {"TOKEN": "${NACELLE_TEST_TOKEN}"}},
	    "docs": {"type": "http", "url": "https://example.invalid/${NACELLE_TEST_REPO:-mcp}",
	             "headers": {"Authorization": "Bearer ${NACELLE_TEST_TOKEN}"}}
	  }
	}`))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}

	docs := servers[0].(Remote)
	if docs.Headers["Authorization"] != "Bearer sekrit" {
		t.Errorf("header = %q, want the token expanded", docs.Headers["Authorization"])
	}
	git := servers[1].(Command)
	if len(git.Args) != 2 || git.Args[1] != "/srv/repo" {
		t.Errorf("args = %q, want the repo expanded", git.Args)
	}
	if git.Env["TOKEN"] != "sekrit" {
		t.Errorf("env TOKEN = %q, want the token expanded", git.Env["TOKEN"])
	}
}

// An absent args array stays absent rather than becoming an empty one, so a
// server is handed the argv it would have been handed without this pass.
func TestAbsentListsAndMapsStayAbsent(t *testing.T) {
	args, err := expandAll("docs", nil)
	if err != nil || args != nil {
		t.Errorf("expandAll(nil) = %v, %v, want nil, nil", args, err)
	}
	env, err := expandMap("docs", nil)
	if err != nil || env != nil {
		t.Errorf("expandMap(nil) = %v, %v, want nil, nil", env, err)
	}
}
