package mcp_test

import (
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle/mcp"
)

// A well-formed list is the case that has to keep working, and an empty one is
// the case every agent that configures no MCP server at all takes on every
// single run.
func TestAServerListWithoutProblemsIsAccepted(t *testing.T) {
	for name, servers := range map[string][]mcp.Server{
		"no servers": nil,
		"empty list": {},
		"two distinct servers": {
			{Name: "docs", URL: "https://docs.example/mcp"},
			{Name: "tickets", URL: "https://tickets.example/mcp", Token: "secret"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mcp.Validate(servers); err != nil {
				t.Errorf("Validate(%v) = %v, want nil", servers, err)
			}
		})
	}
}

// A nameless server cannot be addressed by the toolset entry that looks it up,
// so it is configuration that can never do anything.
func TestAServerWithNoNameIsRefused(t *testing.T) {
	err := mcp.Validate([]mcp.Server{{URL: "https://docs.example/mcp"}})
	if err == nil {
		t.Fatal("Validate accepted a server with no name")
	}
	if !strings.Contains(err.Error(), "no name") {
		t.Errorf("error = %q, want it to say the name is missing", err)
	}
}

// The refusal has to name the server, because an operator reading it is
// looking at a list and needs to know which entry to fix.
func TestAServerWithNoURLIsRefusedAndSaysWhichOne(t *testing.T) {
	err := mcp.Validate([]mcp.Server{
		{Name: "docs", URL: "https://docs.example/mcp"},
		{Name: "tickets"},
	})
	if err == nil {
		t.Fatal("Validate accepted a server with no URL")
	}
	if !strings.Contains(err.Error(), "tickets") {
		t.Errorf("error = %q, want it to name the server it refused", err)
	}
}

// The case the package's own doc comment calls the interesting one: the
// toolset entry finds a server by name, so the second of two servers sharing
// one is unreachable in a way that reads as the server being down. Catching it
// at configuration time is the difference between a clear refusal and an
// afternoon spent debugging a healthy server.
func TestTwoServersSharingANameAreRefused(t *testing.T) {
	err := mcp.Validate([]mcp.Server{
		{Name: "docs", URL: "https://one.example/mcp"},
		{Name: "docs", URL: "https://two.example/mcp"},
	})
	if err == nil {
		t.Fatal("Validate accepted two servers named the same thing")
	}
	if !strings.Contains(err.Error(), "docs") {
		t.Errorf("error = %q, want it to name the collision", err)
	}
}

// Server.Token is a bearer credential, and a validation error is the one thing
// here guaranteed to reach a log. Formatting a Server with %v rather than
// naming its fields would put the token in that log, which is why this pins
// the absence rather than trusting the current format verb to stay.
func TestAValidationErrorNeverCarriesTheToken(t *testing.T) {
	const token = "sk-do-not-log-me"
	for name, servers := range map[string][]mcp.Server{
		"missing URL": {{Name: "docs", Token: token}},
		"duplicate name": {
			{Name: "docs", URL: "https://one.example/mcp", Token: token},
			{Name: "docs", URL: "https://two.example/mcp", Token: token},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := mcp.Validate(servers)
			if err == nil {
				t.Fatal("Validate accepted a list it should have refused")
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("error = %q, want the token kept out of it", err)
			}
		})
	}
}

// A server missing both a name and a URL is reported as nameless, because an
// error naming a server it cannot name is worse than the narrower one.
func TestTheMissingNameIsReportedBeforeTheMissingURL(t *testing.T) {
	err := mcp.Validate([]mcp.Server{{}})
	if err == nil {
		t.Fatal("Validate accepted an empty server")
	}
	if !strings.Contains(err.Error(), "no name") {
		t.Errorf("error = %q, want the missing name reported first", err)
	}
}
