package tools

import (
	"net/url"
	"strings"
	"testing"
)

func rendered(t *testing.T, page string) string {
	t.Helper()

	base, err := url.Parse("https://example.com/docs/page")
	if err != nil {
		t.Fatalf("parsing the base: %v", err)
	}
	return htmlToText(strings.NewReader(page), base)
}

// The bug this file exists for, and the reason the walk parses instead of
// tokenising.
//
// Real pages do not close all their tags — go.dev's own release notes open
// five <nav> elements and close two — because the HTML5 parsing algorithm
// closes them and browsers implement it. A tokeniser reports what was written,
// so a skip counter driven by one never comes back down and swallows every
// remaining element. The page came back holding nothing but its title, and it
// looked like a page that simply had no content.
// The shape is go.dev's own: <nav> left open inside a <header> and an
// <aside>, with the article a sibling of both. A parser closes each nav when
// its parent closes, so the article is reached normally. A tokeniser sees
// three opens and one close, leaves its skip counter at two, and returns a
// page containing nothing but the title.
func TestContentSurvivesAnUnclosedSkippedElement(t *testing.T) {
	out := rendered(t, `<html><head><title>Doc</title></head><body>
		<header><nav><a href="/a">Nav one</a></header>
		<aside><nav><a href="/b">Nav two</a></aside>
		<main><h1>The Real Heading</h1><p>The real content.</p></main>
	</body></html>`)

	if !strings.Contains(out, "The Real Heading") || !strings.Contains(out, "The real content.") {
		t.Errorf("output = %q, want the page after an unclosed <nav> to survive", out)
	}
	if strings.Contains(out, "Nav one") || strings.Contains(out, "Nav two") {
		t.Errorf("output = %q, want navigation still dropped", out)
	}
}

func TestHeadingsListsAndCodeKeepTheirShape(t *testing.T) {
	out := rendered(t, `<h2>Setup</h2><p>Do this.</p>
		<ol><li>First</li><li>Second</li></ol>
		<pre><code>go build ./...
go test ./...</code></pre>`)

	for _, want := range []string{"## Setup", "- First", "- Second", "go build ./...\ngo test ./..."} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

// Whitespace inside <pre> is the author's, everywhere else it is the HTML
// author's indentation and costs tokens to carry.
func TestIndentationIsDroppedOutsidePreAndKeptInside(t *testing.T) {
	out := rendered(t, "<p>one     two\n\n\n   three</p><pre>  kept\n    indent</pre>")

	if !strings.Contains(out, "one two three") {
		t.Errorf("output = %q, want prose whitespace collapsed", out)
	}
	if !strings.Contains(out, "  kept\n    indent") {
		t.Errorf("output = %q, want pre formatting preserved", out)
	}
}

func TestLinksResolveAgainstThePageAndDropWhatCannotBeFollowed(t *testing.T) {
	out := rendered(t, `<p><a href="../up">up</a> <a href="/root">root</a> <a href="#here">anchor</a>
		<a href="javascript:void(0)">script</a> <a href="https://other.example/x">other</a></p>`)

	for _, want := range []string{"[up](https://example.com/up)", "[root](https://example.com/root)",
		"[other](https://other.example/x)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "javascript:") || strings.Contains(out, "#here") {
		t.Errorf("output = %q, want unfollowable links reduced to their text", out)
	}
	for _, text := range []string{"anchor", "script"} {
		if !strings.Contains(out, text) {
			t.Errorf("output = %q, want %q kept as plain text", out, text)
		}
	}
}

// Prose reads "text, more" and never "text , more". The pairing is common
// enough that getting it wrong shows up on every page.
func TestPunctuationAfterALinkDoesNotGetASpace(t *testing.T) {
	out := rendered(t, `<p>arrives in <a href="/when">February</a>, six months after <a href="/prev">1.25</a>.</p>`)

	if !strings.Contains(out, "), six months") {
		t.Errorf("output = %q, want no space between a link and the comma after it", out)
	}
	if !strings.Contains(out, "arrives in [February]") {
		t.Errorf("output = %q, want a space kept before a link that follows a word", out)
	}
}

func TestScriptAndStyleNeverReachTheModel(t *testing.T) {
	out := rendered(t, `<html><head><style>.a{color:red}</style>
		<script>var secret = "tracking payload";</script></head>
		<body><p>Just this.</p></body></html>`)

	for _, unwanted := range []string{"tracking payload", "color:red", "var secret"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output contains %q\n---\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "Just this.") {
		t.Errorf("output = %q, want the prose kept", out)
	}
}
