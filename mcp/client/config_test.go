package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts one config on disk and hands back its path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return path
}

// The format's own two shapes, read from the file a person already has.
func TestLoadReadsBothTransportsFromOneFile(t *testing.T) {
	servers, err := Load(write(t, `{
	  "mcpServers": {
	    "git":  {"command": "/usr/bin/mcp-server-git", "args": ["--repo", "."], "env": {"TZ": "UTC"}},
	    "docs": {"type": "http", "url": "https://example.invalid/mcp",
	             "headers": {"Authorization": "Bearer t"}}
	  }
	}`))
	if err != nil {
		t.Fatalf("Load = %v, want two servers", err)
	}
	if len(servers) != 2 {
		t.Fatalf("Load returned %d servers, want 2", len(servers))
	}

	docs, ok := servers[0].(Remote)
	if !ok {
		t.Fatalf("servers[0] = %T, want the Remote first — they come back sorted by name", servers[0])
	}
	if docs.URL != "https://example.invalid/mcp" || docs.Headers["Authorization"] != "Bearer t" {
		t.Errorf("docs = %+v, want the url and header from the file", docs)
	}

	git, ok := servers[1].(Command)
	if !ok {
		t.Fatalf("servers[1] = %T, want a Command", servers[1])
	}
	if git.Path != "/usr/bin/mcp-server-git" || len(git.Args) != 2 || git.Env["TZ"] != "UTC" {
		t.Errorf("git = %+v, want the command, args and env from the file", git)
	}
}

// Later files win, which is what the precedence in every client of this
// format means and what -mcp personal.json -mcp project.json has to do.
func TestALaterFileOverridesAnEarlierServerOfTheSameName(t *testing.T) {
	first := write(t, `{"mcpServers": {"git": {"command": "/usr/bin/old"}}}`)
	second := write(t, `{"mcpServers": {"git": {"command": "/usr/bin/new"}}}`)

	servers, err := Load(first, second)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("Load returned %d servers, want the one, overridden", len(servers))
	}
	if got := servers[0].(Command).Path; got != "/usr/bin/new" {
		t.Errorf("git.Path = %q, want the later file to win", got)
	}
}

// Everything the format lets a person get wrong, refused with the fix in the
// message rather than at the first call that needed the server.
func TestLoadRefusesWhatItCannotRunAndSaysWhy(t *testing.T) {
	for name, fixture := range map[string]struct{ body, wants string }{
		"a url with no type":   {`{"mcpServers":{"d":{"url":"https://e.invalid/mcp"}}}`, `"type": "http"`},
		"the sse transport":    {`{"mcpServers":{"d":{"type":"sse","url":"https://e.invalid"}}}`, "Streamable HTTP"},
		"a websocket":          {`{"mcpServers":{"d":{"type":"ws","url":"wss://e.invalid"}}}`, "Streamable HTTP"},
		"an unknown transport": {`{"mcpServers":{"d":{"type":"carrier-pigeon"}}}`, "unknown transport"},
		"a name the model cannot call": {`{"mcpServers":{"my.server":{"command":"/usr/bin/true"}}}`,
			"cannot be named to the model"},
		"a misspelled key": {`{"mcpServers":{"d":{"comand":"/usr/bin/true"}}}`, "comand"},
		"no mcpServers":    {`{"servers":{}}`, "no mcpServers object"},
		"not json at all":  {`nope`, "reading"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, fixture.body))
			if err == nil {
				t.Fatal("Load accepted a config it should have refused")
			}
			if !strings.Contains(err.Error(), fixture.wants) {
				t.Errorf("Load = %v, want it to mention %q", err, fixture.wants)
			}
		})
	}
}

// A key this client has no opinion on is not an error: these files are shared
// between clients, and "$schema" or VS Code's "inputs" alongside mcpServers is
// a perfectly good configuration everywhere else.
func TestKeysBesideMcpServersAreLeftAlone(t *testing.T) {
	servers, err := Load(write(t, `{
	  "$schema": "https://example.invalid/schema.json",
	  "inputs": [{"id": "token", "type": "promptString"}],
	  "mcpServers": {"git": {"command": "/usr/bin/mcp-server-git"}}
	}`))
	if err != nil {
		t.Fatalf("Load = %v, want the sibling keys ignored", err)
	}
	if len(servers) != 1 {
		t.Errorf("Load returned %d servers, want 1", len(servers))
	}
}

// A file that is not there is the operator's own typo and has to name itself.
func TestLoadNamesAFileItCannotRead(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err == nil || !strings.Contains(err.Error(), "absent.json") {
		t.Errorf("Load = %v, want it to name the file", err)
	}
}

// Other clients write cwd and disabled, and this one has somewhere to put
// both — a file that uses them has to load rather than be refused for keys
// that are perfectly good everywhere else.
func TestCwdAndDisabledAreHonoured(t *testing.T) {
	servers, err := Load(write(t, `{
	  "mcpServers": {
	    "git":  {"command": "/usr/bin/mcp-server-git", "cwd": "/srv/repo"},
	    "old":  {"command": "/usr/bin/retired", "disabled": true}
	  }
	}`))
	if err != nil {
		t.Fatalf("Load = %v, want cwd and disabled understood", err)
	}
	if len(servers) != 1 {
		t.Fatalf("Load returned %d servers, want the disabled one left out", len(servers))
	}
	if got := servers[0].(Command).Dir; got != "/srv/repo" {
		t.Errorf("git.Dir = %q, want the cwd from the file", got)
	}
}

// A key that narrows what a server may do is refused, never ignored: nodding
// along to autoApprove would leave someone believing they had restricted a
// server this client had in fact left wide open.
func TestAPermissionKeyFromAnotherClientIsRefusedRatherThanIgnored(t *testing.T) {
	for _, key := range []string{"autoApprove", "alwaysAllow"} {
		t.Run(key, func(t *testing.T) {
			_, err := Load(write(t, `{"mcpServers":{"d":{"command":"/usr/bin/true","`+key+`":["write"]}}}`))
			if err == nil {
				t.Fatal("Load quietly ignored a permission setting")
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("Load = %v, want it to name %q", err, key)
			}
		})
	}
}

// An empty path is refused by saying so, because os.ReadFile's own account of
// it — `reading : open : no such file or directory` — names no file at all,
// and the way a person arrives here is -mcp "$VAR" with VAR unset.
func TestAnEmptyPathIsRefusedBySayingSo(t *testing.T) {
	_, err := Load("")
	if err == nil {
		t.Fatal("Load accepted an empty path")
	}
	if !strings.Contains(err.Error(), "empty") || !strings.Contains(err.Error(), "shell variable") {
		t.Errorf("Load = %v, want it to name the empty path and the likely cause", err)
	}
}
