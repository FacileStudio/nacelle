package tools

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FacileStudio/nacelle"
)

// Defaults for the web fetch tool.
const (
	// DefaultFetchTimeout bounds one page. Longer than a search, because a
	// search asks one service that answers or does not, while a page can
	// be a slow origin behind a cold cache.
	DefaultFetchTimeout = 30 * time.Second

	// DefaultMaxFetchBytes caps what is read off the wire, before any
	// conversion. Generous against real pages and far below what a
	// misdirected fetch of an archive would pull.
	DefaultMaxFetchBytes = 2 << 20

	// maxFetchRedirects is where a redirect chain stops being a redirect
	// chain and starts being a loop.
	maxFetchRedirects = 5

	// fetchAgent identifies these requests honestly, with somewhere to
	// look us up.
	//
	// Deliberately not a browser string. Every current source says the
	// same thing — negotiated access is what gets an agent through, and a
	// spoofed user agent is what gets an IP banned once the fingerprint
	// behind it disagrees with the name. An operator who wants to allow
	// this can only do so if it says what it is.
	fetchAgent = "nacelle/webfetch (+https://github.com/FacileStudio/nacelle)"

	// fetchAccept asks for markdown first.
	//
	// Cloudflare's Markdown for Agents and Vercel's equivalent convert a
	// page at the edge when a client asks for text/markdown, which is a
	// documented ~80% fewer tokens than the HTML it replaces and skips the
	// conversion below entirely. Sites without it ignore the header and
	// answer HTML, so this costs nothing where it is not supported.
	fetchAccept = "text/markdown, text/html;q=0.9, text/plain;q=0.8, application/json;q=0.7, */*;q=0.1"
)

// WebFetch builds the tool that reads one web page.
//
// # What it will not reach
//
// Only http and https, and only addresses on the public internet: see
// [guardedClient] for why the check is in the dialer rather than on the URL,
// and what it is defending. Unlike every other tool here the model names the
// destination, which is exactly why that check cannot be optional.
//
// # What comes back is not trusted
//
// A fetched page is text written by a stranger, arriving in the channel the
// model reads as instructions. This is the tool that makes that concrete: a
// page can ask the model to fetch another URL with something from the
// conversation in the query string, and nothing here can tell that apart from
// the model deciding to follow a link. Mount it knowing that, and see the
// tool approval gate if the conversation will hold anything worth stealing.
func WebFetch() ([]nacelle.Tool, error) {
	page := &fetcher{client: guardedClient(DefaultFetchTimeout)}
	return buildAll(page.fetchTool)
}

type fetcher struct{ client *http.Client }

type webFetchInput struct {
	URL string `json:"url" jsonschema:"required,description=The full http or https URL of the page to read. Use a URL you already have — from web_search results or from a link on a page you fetched — rather than guessing one"`
}

// fetchTool builds the reader.
func (f *fetcher) fetchTool() (nacelle.Tool, error) {
	return nacelle.NewToolWithOptions("web_fetch",
		"Read one web page and get back its text: headings, paragraphs, lists, code blocks and links. "+
			"Use it after web_search to read a result properly — search returns a sentence per hit, this returns the page. "+
			"Navigation, scripts and styling are stripped; links are absolute so you can follow one by calling this again. "+
			"Only http and https, and only the public internet.",
		func(ctx context.Context, in webFetchInput) (string, error) {
			target, err := parseFetchURL(in.URL)
			if err != nil {
				return "", err
			}
			return f.read(ctx, target)
		},
		nacelle.ToolOptions{ReadOnly: true})
}

// parseFetchURL refuses anything that is not a public-web URL before a request
// is built, so the obvious mistakes get a sentence rather than a dial error.
func parseFetchURL(raw string) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	switch {
	case target.Scheme != "http" && target.Scheme != "https":
		return nil, fmt.Errorf("%q is not http or https, and this tool reads web pages only", raw)
	case target.Host == "":
		return nil, fmt.Errorf("%q names no host", raw)
	}
	return target, nil
}

// read fetches one page and renders it.
func (f *fetcher) read(ctx context.Context, target *url.URL) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", fmt.Errorf("building the request: %w", err)
	}
	request.Header.Set("User-Agent", fetchAgent)
	request.Header.Set("Accept", fetchAccept)
	request.Header.Set("Accept-Language", "en;q=0.9, *;q=0.5")

	response, err := f.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", target.Host, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", refusedBy(response, target.Host)
	}
	return renderBody(response, target)
}

// refusedBy explains a non-200 in the terms that decide what to do next.
//
// The status alone is misleading here more than anywhere else in this package:
// an anti-bot layer answers a perfectly good request with 403 and an HTML page
// saying so, which reads as the page being forbidden to everyone rather than
// to this client. Naming that is the difference between a model trying a
// different source and a model reporting that the page does not exist.
func refusedBy(response *http.Response, host string) error {
	switch response.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		return fmt.Errorf("%s refused this request (%d), which for an automated client usually means "+
			"its anti-bot layer rather than the page being private: try another source for the same "+
			"information, or the search result's own summary", host, response.StatusCode)
	case http.StatusTooManyRequests:
		wait := response.Header.Get("Retry-After")
		if wait == "" {
			wait = "a while"
		}
		return fmt.Errorf("%s is rate limiting this client (429); it asks for %s before the next "+
			"request, so do not retry immediately", host, wait)
	case http.StatusNotFound:
		return fmt.Errorf("%s has nothing at that path (404) — check the URL rather than retrying it", host)
	default:
		return fmt.Errorf("%s answered %d %s", host, response.StatusCode, http.StatusText(response.StatusCode))
	}
}

// renderBody turns a response into text, or says why it cannot.
func renderBody(response *http.Response, target *url.URL) (string, error) {
	kind, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		kind = "text/html"
	}
	body := io.LimitReader(response.Body, DefaultMaxFetchBytes)

	if kind == "text/html" || kind == "application/xhtml+xml" {
		return withSource(htmlToText(body, target), target), nil
	}
	if !strings.HasPrefix(kind, "text/") && !strings.HasSuffix(kind, "json") &&
		!strings.HasSuffix(kind, "xml") {
		return "", fmt.Errorf("%s answered with %s, which this tool cannot read — it renders web "+
			"pages and text, not documents or binary files", target.Host, kind)
	}

	raw, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", target.Host, err)
	}
	return withSource(strings.TrimSpace(string(raw)), target), nil
}

// withSource caps the page and says where it came from.
//
// An empty page is the one outcome that needs explaining rather than
// returning: a site that renders its content with JavaScript answers a fetch
// with a shell of markup, and "" would read to the model as a page that exists
// and says nothing, which is a worse answer than the truth.
func withSource(text string, target *url.URL) string {
	if strings.TrimSpace(text) == "" {
		return fmt.Sprintf("%s returned no readable text. The page most likely builds its content "+
			"with JavaScript, which this tool does not run — look for a documentation, API or raw "+
			"version of the same page, or use the search result's summary.", target)
	}
	return truncate(target.String()+"\n\n"+text, DefaultMaxOutputBytes)
}
