package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// fakeInstance stands in for SearXNG, answering every request with one
// canned body and recording the last request it was asked.
type fakeInstance struct {
	server *httptest.Server
	asked  *http.Request
}

// searchAgainst builds the tool pointed at a fake instance answering with
// status and body. It is the whole reason the endpoint is a constructor
// argument rather than something this package resolves for itself: a test
// gets to be the search engine.
func searchAgainst(t *testing.T, status int, body string) (nacelle.Tool, *fakeInstance) {
	t.Helper()

	instance := &fakeInstance{}
	instance.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instance.asked = r
		w.WriteHeader(status)
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("writing the canned answer: %v", err)
		}
	}))
	t.Cleanup(instance.server.Close)

	built, err := WebSearch(instance.server.URL)
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if len(built) != 1 {
		t.Fatalf("WebSearch built %d tools, want exactly 1", len(built))
	}
	return built[0], instance
}

// run calls the tool the way a backend would.
func run(t *testing.T, tool nacelle.Tool, query string) (string, error) {
	t.Helper()

	input, err := json.Marshal(webSearchInput{Query: query})
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	return tool.Run(context.Background(), input)
}

// resultsJSON is an instance answering with n results.
func resultsJSON(n int) string {
	results := make([]string, 0, n)
	for i := range n {
		results = append(results, fmt.Sprintf(
			`{"title":"Result %[1]d","url":"https://example.com/%[1]d","content":"Snippet %[1]d"}`, i+1))
	}
	return `{"results":[` + strings.Join(results, ",") + `]}`
}

func TestWebSearchIsAbsentWithoutAnEndpointAndThatIsNotAnError(t *testing.T) {
	for _, endpoint := range []string{"", "   "} {
		tools, err := WebSearch(endpoint)
		if err != nil {
			t.Fatalf("WebSearch(%q): %v, want no error when there is simply nothing configured", endpoint, err)
		}
		if tools != nil {
			t.Errorf("WebSearch(%q) = %v, want nil when there is nothing to build", endpoint, tools)
		}
	}
}

func TestWebSearchRefusesAnEndpointItCouldNeverReach(t *testing.T) {
	for _, endpoint := range []string{"ftp://searx.example", "furet.example/search", "https://"} {
		if _, err := WebSearch(endpoint); err == nil {
			t.Errorf("WebSearch(%q) built a tool, want a refusal at construction", endpoint)
		}
	}
}

func TestWebSearchAsksTheInstanceForJSON(t *testing.T) {
	tool, instance := searchAgainst(t, http.StatusOK, resultsJSON(1))

	if _, err := run(t, tool, "go generics"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := instance.asked.URL.Path; got != "/search" {
		t.Errorf("path = %q, want /search", got)
	}
	query := instance.asked.URL.Query()
	if got := query.Get("format"); got != "json" {
		t.Errorf("format = %q, want json — without it SearXNG answers HTML", got)
	}
	if got := query.Get("q"); got != "go generics" {
		t.Errorf("q = %q, want the query as typed", got)
	}
}

func TestWebSearchRendersTitleURLAndSnippet(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusOK,
		`{"results":[{"title":"context - Go Packages","url":"https://pkg.go.dev/context",`+
			`"content":"Package context\n  defines the Context type."}]}`)

	out, err := run(t, tool, "context")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, want := range []string{"context - Go Packages", "https://pkg.go.dev/context"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	if !strings.Contains(out, "Package context defines the Context type.") {
		t.Errorf("output = %q, want the snippet collapsed onto one line", out)
	}
}

func TestWebSearchCapsWhatReachesTheModel(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusOK, resultsJSON(DefaultSearchResults+7))

	out, err := run(t, tool, "anything")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if last := fmt.Sprintf("Result %d", DefaultSearchResults); !strings.Contains(out, last) {
		t.Errorf("output = %q, want it to reach %q", out, last)
	}
	if over := fmt.Sprintf("Result %d", DefaultSearchResults+1); strings.Contains(out, over) {
		t.Errorf("output contains %q, want no more than %d results", over, DefaultSearchResults)
	}
}

func TestWebSearchSaysNothingWasFoundRatherThanFailing(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusOK, `{"results":[]}`)

	out, err := run(t, tool, "asdfqwerzxcv")
	if err != nil {
		t.Fatalf("Run: %v, want an empty web to be an answer rather than an error", err)
	}
	if !strings.Contains(out, "no results") {
		t.Errorf("output = %q, want it to say no results", out)
	}
}

// A dead metasearch engine and a web with no answer produce the same empty
// list, and reporting the first as the second is the model stating an outage
// as fact.
func TestWebSearchDistinguishesAnEmptyWebFromASilentInstance(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusOK,
		`{"results":[],"unresponsive_engines":[["google","timeout"],["ddg","timeout"]]}`)

	out, err := run(t, tool, "anything")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "2 of the instance's engines did not respond") {
		t.Errorf("output = %q, want it to name the engines that never answered", out)
	}
}

// The single most likely misconfiguration: SearXNG ships with json missing
// from search.formats, so a working instance answers a working query with a
// web page. The error has to name the setting or nobody finds it.
func TestWebSearchNamesTheSettingWhenTheInstanceAnswersHTML(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusOK, "<!DOCTYPE html><html><body>results</body></html>")

	_, err := run(t, tool, "anything")
	if err == nil {
		t.Fatal("Run succeeded on an HTML answer, want an error")
	}
	if !strings.Contains(err.Error(), "search.formats") {
		t.Errorf("error = %q, want it to name search.formats in settings.yml", err)
	}
}

func TestWebSearchNamesTheLimiterOnAForbidden(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusForbidden, "forbidden")

	_, err := run(t, tool, "anything")
	if err == nil {
		t.Fatal("Run succeeded on a 403, want an error")
	}
	if !strings.Contains(err.Error(), "limiter") {
		t.Errorf("error = %q, want it to point at the limiter rather than read as an outage", err)
	}
}

func TestWebSearchNamesTheBaseURLOnANotFound(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusNotFound, "not found")

	_, err := run(t, tool, "anything")
	if err == nil {
		t.Fatal("Run succeeded on a 404, want an error")
	}
	if !strings.Contains(err.Error(), "base URL") {
		t.Errorf("error = %q, want it to point at the endpoint rather than read as an outage", err)
	}
}

// invopop/jsonschema cuts a description at the first unescaped comma and says
// nothing, so a tag written as prose reaches the model as its first clause.
// The description is the only thing the model chooses a tool by, which makes
// this the cheapest possible bug to ship and the hardest to notice.
func TestWebSearchDescriptionSurvivesItsComma(t *testing.T) {
	tool, _ := searchAgainst(t, http.StatusOK, resultsJSON(1))

	properties, ok := tool.Schema()["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema = %v, want properties on it", tool.Schema())
	}
	query, ok := properties["query"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %v, want a query among them", properties)
	}

	described, _ := query["description"].(string)
	if !strings.Contains(described, "search engine") {
		t.Errorf("description = %q, want the whole sentence: a tag comma needs a backslash before it", described)
	}
}

func TestWebSearchRefusesAnEmptyQuery(t *testing.T) {
	tool, instance := searchAgainst(t, http.StatusOK, resultsJSON(1))

	if _, err := run(t, tool, "   "); err == nil {
		t.Error("Run succeeded on a blank query, want a refusal")
	}
	if instance.asked != nil {
		t.Error("a blank query reached the instance, want it refused before the request")
	}
}
