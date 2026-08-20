package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// servingPage answers every request with one body and content type, and hands
// back a way to see what was asked.
//
// The fetcher's client is the test server's own rather than the guarded one,
// because the guard refuses loopback on purpose — see
// TestTheGuardedClientRefusesLoopbackEvenWhenSomethingIsListening, which is
// where that behaviour is tested instead of worked around.
func servingPage(t *testing.T, status int, contentType, body string) (*fetcher, *url.URL, func() *http.Request) {
	t.Helper()

	var asked *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.Clone(context.Background())
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "120")
		}
		w.WriteHeader(status)
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("writing the canned page: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	target, err := parseFetchURL(server.URL)
	if err != nil {
		t.Fatalf("parseFetchURL(%q): %v", server.URL, err)
	}
	return &fetcher{client: server.Client()}, target, func() *http.Request { return asked }
}

// fetched runs one fetch against a canned page and returns what the model
// would read.
func fetched(t *testing.T, status int, contentType, body string) (string, error) {
	t.Helper()

	page, target, _ := servingPage(t, status, contentType, body)
	return page.read(context.Background(), target)
}

func TestParseFetchURLRefusesWhatIsNotAWebPage(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/x", "gopher://x", "not a url", "https://"} {
		if _, err := parseFetchURL(raw); err == nil {
			t.Errorf("parseFetchURL(%q) succeeded, want a refusal", raw)
		}
	}
}

const noisyPage = `<html><head><title>Context</title>
<style>body{color:red}</style><script>var tracking = "should never be read";</script></head>
<body><nav><a href="/home">Home</a><a href="/about">About</a></nav>
<h1>Package context</h1>
<p>Package context defines the Context type.</p>
<ul><li>WithCancel</li><li>WithTimeout</li></ul>
<p>See the <a href="/doc/faq">FAQ</a> and <a href="https://go.dev/blog">the blog</a>.</p>
<footer>Copyright forever</footer></body></html>`

func TestWebFetchRendersAPageAsSomethingWorthReading(t *testing.T) {
	out, err := fetched(t, http.StatusOK, "text/html; charset=utf-8", noisyPage)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for _, want := range []string{"# Package context", "Package context defines the Context type.", "- WithCancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"should never be read", "color:red", "Copyright forever", "About"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output contains %q, which should have been stripped\n---\n%s", unwanted, out)
		}
	}
}

// A relative href is useless to a model that has to call this tool again with
// it, so links come back absolute.
func TestWebFetchMakesLinksAbsolute(t *testing.T) {
	out, err := fetched(t, http.StatusOK, "text/html", noisyPage)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "](http") || !strings.Contains(out, "/doc/faq)") {
		t.Errorf("output = %q, want the relative FAQ link resolved against the page URL", out)
	}
	if !strings.Contains(out, "[the blog](https://go.dev/blog)") {
		t.Errorf("output = %q, want an absolute link kept as markdown", out)
	}
}

// Cloudflare's Markdown for Agents and Vercel's equivalent convert at the edge
// when asked, for a documented ~80% fewer tokens. Asking costs nothing where
// it is unsupported, so the only way to get it wrong is not to ask.
func TestWebFetchAsksForMarkdownAndSaysWhatItIs(t *testing.T) {
	page, target, asked := servingPage(t, http.StatusOK, "text/html", "<p>hi</p>")
	if _, err := page.read(context.Background(), target); err != nil {
		t.Fatalf("read: %v", err)
	}

	accept := asked().Header.Get("Accept")
	if !strings.HasPrefix(accept, "text/markdown") {
		t.Errorf("Accept = %q, want markdown preferred first", accept)
	}
	agent := asked().Header.Get("User-Agent")
	if !strings.Contains(agent, "nacelle") {
		t.Errorf("User-Agent = %q, want this client named honestly rather than a browser spoofed", agent)
	}
}

func TestWebFetchReturnsMarkdownWithoutTouchingIt(t *testing.T) {
	body := "# Title\n\nA paragraph with **bold** in it.\n"
	out, err := fetched(t, http.StatusOK, "text/markdown", body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "**bold**") {
		t.Errorf("output = %q, want markdown passed through as it arrived", out)
	}
}

func TestWebFetchRefusesWhatItCannotRead(t *testing.T) {
	_, err := fetched(t, http.StatusOK, "image/png", "\x89PNG\r\n")
	if err == nil {
		t.Fatal("read succeeded on a PNG, want a refusal")
	}
	if !strings.Contains(err.Error(), "image/png") {
		t.Errorf("error = %q, want it to name the type it could not read", err)
	}
}

// An anti-bot layer answers a good request with 403 and a page saying so. Read
// as-is that is "forbidden to everyone"; the model then reports the page does
// not exist instead of trying another source.
func TestWebFetchNamesTheAntiBotLayerOnA403(t *testing.T) {
	_, err := fetched(t, http.StatusForbidden, "text/html", "<h1>Access denied</h1>")
	if err == nil {
		t.Fatal("read succeeded on a 403, want an error")
	}
	if !strings.Contains(err.Error(), "anti-bot") || !strings.Contains(err.Error(), "another source") {
		t.Errorf("error = %q, want it to explain the refusal and say what to do instead", err)
	}
}

func TestWebFetchPassesOnHowLongToWaitAfterA429(t *testing.T) {
	_, err := fetched(t, http.StatusTooManyRequests, "text/plain", "slow down")
	if err == nil {
		t.Fatal("read succeeded on a 429, want an error")
	}
	if !strings.Contains(err.Error(), "120") || !strings.Contains(err.Error(), "not retry immediately") {
		t.Errorf("error = %q, want Retry-After passed on with the instruction not to hammer", err)
	}
}

// A page whose body is built by JavaScript arrives as a shell. Returning ""
// would read as a page that exists and says nothing, which is worse than the
// truth because the model cannot tell it needs a different approach.
func TestWebFetchExplainsAPageThatRendersItselfWithJavaScript(t *testing.T) {
	out, err := fetched(t, http.StatusOK, "text/html",
		`<html><body><div id="root"></div><script>render()</script></body></html>`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "JavaScript") {
		t.Errorf("output = %q, want an empty render explained rather than returned blank", out)
	}
}

func TestWebFetchSaysWhereThePageCameFrom(t *testing.T) {
	out, err := fetched(t, http.StatusOK, "text/html", "<p>body text</p>")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(out, "http") {
		t.Errorf("output = %q, want it to open with the URL it was read from", out)
	}
}
