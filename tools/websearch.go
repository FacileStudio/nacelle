package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FacileStudio/nacelle"
)

// Defaults for the web search tool.
const (
	// DefaultSearchResults is how many hits reach the model.
	//
	// Deliberately not something the model chooses. A count in the tool
	// schema is a count the model will occasionally set to fifty, and
	// fifty snippets is most of a context window spent on the results of
	// one question rather than on answering it.
	DefaultSearchResults = 8

	// DefaultSearchTimeout bounds one search. A metasearch engine is only
	// as quick as the slowest upstream it is still waiting on, so this is
	// long by the standards of a single HTTP call and short by the
	// standards of someone watching a prompt.
	DefaultSearchTimeout = 15 * time.Second

	// maxSearchBody caps what is read from the instance, which is the one
	// defence available against an endpoint that answers with something
	// other than what it promised. Two orders of magnitude above a real
	// response.
	maxSearchBody = 4 << 20

	// searchAgent identifies these requests to the instance being asked.
	// Self-hosted instances are small and their operators read logs; an
	// unlabelled client is a support question waiting to happen.
	searchAgent = "nacelle/websearch"
)

// WebSearch builds the tool that searches the web through a SearXNG instance.
//
// # Why there is no default endpoint
//
// Empty means no tool, no error, exactly as [Jardin] means when jardin is not
// installed. That is not timidity about defaults, it is the only correct
// answer: this package ships in a public repository, so a hardcoded instance
// would point every stranger's queries at one operator's machine, and put
// their queries in that operator's logs. There is also no neutral choice to
// make — the public SearXNG instances rate-limit, and most serve HTML only.
// The endpoint is a deployment fact, and deployment facts belong to whoever
// is deploying.
//
// # Why the model cannot name the instance
//
// The endpoint is a constructor argument, never a tool argument. This is the
// same rule the package comment states for Config.Root and for the same
// reason: a model that can nominate the host a request goes to has been handed
// the boundary along with the thing inside it, and every internal address the
// process can reach is then one tool call away.
//
// # What comes back is not trusted
//
// Results are text written by strangers, arriving in a channel the model
// reads as instructions when it is careless. Nothing here can fix that, and
// pretending otherwise by filtering would only make the exposure harder to
// see. It is named so a caller deciding what else to mount alongside it —
// bash, writes, an approval gate — is deciding with it in view.
//
// # A note on confinement
//
// Every other tool in this package is confined to one directory. This one
// reaches the network on purpose, so the package comment's guarantees about
// [os.Root] describe its neighbours and not it.
func WebSearch(endpoint string) ([]nacelle.Tool, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, nil
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("nacelle/tools: the search endpoint %q is not a URL: %w", endpoint, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("nacelle/tools: the search endpoint %q needs an http:// or https:// scheme", endpoint)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("nacelle/tools: the search endpoint %q names no host", endpoint)
	}

	engine := &searxng{endpoint: parsed, client: &http.Client{Timeout: DefaultSearchTimeout}}
	return buildAll(engine.searchTool)
}

// searxng is one instance, and the client that reaches it.
type searxng struct {
	endpoint *url.URL
	client   *http.Client
}

// searchResponse is the part of SearXNG's answer worth decoding.
//
// The wire shape carries about twenty fields per result — template, img_src,
// iframe_src, audio_src, thumbnail, positions, open_group and the rest — and
// naming only four is what keeps a decode from breaking when that list
// changes, which it does between releases.
//
// UnresponsiveEngines is [json.RawMessage] rather than the [][]string the
// wire happens to hold today, because only its length is read and a strict
// type here would turn a change in a diagnostic field into a failed search.
type searchResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
	UnresponsiveEngines []json.RawMessage `json:"unresponsive_engines"`
}

// webSearchInput is what the model fills in.
//
// The doubled backslash is load-bearing, because two things unescape this
// string in turn. invopop/jsonschema splits the tag on unescaped commas, so
// prose is cut at its first comma and the model reads only the clause before
// it. That split is reached through reflect.StructTag.Get, which unquotes
// first — and \, is not a valid Go escape, so writing one there blanks the
// description entirely rather than halving it. \\, survives both, and
// TestWebSearchDescriptionSurvivesItsComma fails on either mistake.
type webSearchInput struct {
	Query string `json:"query" jsonschema:"required,description=What to look for\\, phrased as you would type it into a search engine"`
}

// searchTool builds the search.
func (s *searxng) searchTool() (nacelle.Tool, error) {
	return nacelle.NewTool("web_search",
		"Search the web and get back the top results as title, URL and a short snippet. "+
			"Use it for anything current or outside the working directory: documentation, releases, "+
			"error messages, upstream issues, whether a library still exists. "+
			"Snippets are one or two sentences and are not the page — treat them as a way to find "+
			"the right URL, not as the answer.",
		func(ctx context.Context, in webSearchInput) (string, error) {
			query := strings.TrimSpace(in.Query)
			if query == "" {
				return "", fmt.Errorf("no query given")
			}
			found, err := s.search(ctx, query)
			if err != nil {
				return "", err
			}
			return render(found, query), nil
		})
}

// search asks the instance one question.
func (s *searxng) search(ctx context.Context, query string) (*searchResponse, error) {
	asked := s.endpoint.JoinPath("search")
	asked.RawQuery = url.Values{"q": {query}, "format": {"json"}}.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asked.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building the search request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", searchAgent)

	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reaching the search instance at %s: %w", s.endpoint.Host, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, statusError(response.StatusCode, s.endpoint.Host)
	}

	var body searchResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxSearchBody)).Decode(&body); err != nil {
		return nil, fmt.Errorf("the instance at %s answered with something that is not JSON, "+
			"which usually means json is missing from search.formats in its settings.yml: %w", s.endpoint.Host, err)
	}
	return &body, nil
}

// statusError says what a refusal most likely was.
//
// Both special cases are configuration rather than an outage, and both read
// as an outage without a sentence saying otherwise. 403 is SearXNG's limiter,
// which is on by default and rejects clients that do not look like browsers,
// so the first request from a new machine is the one that meets it. 404 is
// most often an endpoint set to the URL copied out of a browser's address
// bar, which already ends in /search and becomes /search/search here.
func statusError(status int, host string) error {
	switch status {
	case http.StatusForbidden:
		return fmt.Errorf("the instance at %s refused the search (403), "+
			"which is usually its limiter: set limiter: false in settings.yml, or allow this client", host)
	case http.StatusNotFound:
		return fmt.Errorf("the instance at %s has nothing at that path (404): "+
			"the endpoint wants the instance's base URL, not its /search page", host)
	default:
		return fmt.Errorf("the instance at %s answered %s", host, http.StatusText(status))
	}
}

// render turns the answer into the few lines the model should read.
func render(body *searchResponse, query string) string {
	if len(body.Results) == 0 {
		return nothingFound(body, query)
	}

	results := body.Results
	if len(results) > DefaultSearchResults {
		results = results[:DefaultSearchResults]
	}

	var out strings.Builder
	for i, result := range results {
		fmt.Fprintf(&out, "%d. %s\n   %s\n", i+1, oneLine(result.Title), result.URL)
		if snippet := oneLine(result.Content); snippet != "" {
			fmt.Fprintf(&out, "   %s\n", snippet)
		}
	}
	return truncate(strings.TrimRight(out.String(), "\n"), DefaultMaxOutputBytes)
}

// nothingFound distinguishes a web with no answer from an instance that could
// not go and look.
//
// They read identically in an empty result list and they are opposite
// problems: one is an answer, the other is an outage the model would
// otherwise report as fact. This package refuses that trade everywhere else —
// see Event.Stop — and it is no more acceptable here.
func nothingFound(body *searchResponse, query string) string {
	if down := len(body.UnresponsiveEngines); down > 0 {
		return fmt.Sprintf("no results for %q, and %d of the instance's engines did not respond, "+
			"so this may be the search instance rather than the web", query, down)
	}
	return fmt.Sprintf("no results for %q", query)
}

// oneLine collapses whitespace so one result stays one line. Snippets arrive
// with newlines in them often enough that not doing this turns eight results
// into an unreadable wall.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }
