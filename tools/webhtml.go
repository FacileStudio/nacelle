package tools

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// maxHTMLDepth stops a walk before a pathologically nested page exhausts the
// stack. Real pages are tens of levels deep; nothing legitimate is near this.
const maxHTMLDepth = 256

// skippedElements have their whole subtree dropped.
//
// Two kinds, for two reasons. script, style, noscript, svg, canvas, template
// and iframe carry no prose, and their contents are the single biggest source
// of noise in a naively stripped page — a minified bundle costs thousands of
// tokens and says nothing. nav, header, footer, aside and form carry prose
// that is on every page of the site rather than about the page asked for,
// which is the same reason a reader's eye skips them.
var skippedElements = map[string]bool{
	"script": true, "style": true, "noscript": true, "svg": true,
	"canvas": true, "template": true, "iframe": true, "nav": true,
	"header": true, "footer": true, "aside": true, "form": true,
	"button": true, "select": true,
}

// blockElements end the line they are on. The list is what changes the shape
// of a page rather than everything that is technically block-level: a div per
// word is common enough that treating every one as a paragraph break produces
// a page of single words.
var blockElements = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true,
	"ul": true, "ol": true, "table": true, "tr": true, "blockquote": true,
	"pre": true, "hr": true, "title": true, "figure": true, "dl": true,
}

var headingLevel = map[string]int{"h1": 1, "h2": 2, "h3": 3, "h4": 4, "h5": 5, "h6": 6}

// textWriter turns a parsed page into something worth spending context on.
type textWriter struct {
	out  strings.Builder
	link strings.Builder
	base *url.URL
	href string
	pre  int
}

// htmlToText renders a page as text, keeping the structure a reader uses —
// headings, list items, code blocks — and the links, resolved against the
// page's own URL so the model can follow one without guessing a base.
//
// It parses rather than tokenises, and that is not a style preference. A
// tokeniser reports tags exactly as written, and real pages do not close all
// of theirs: go.dev's release notes open five <nav> elements and close two,
// which a tokeniser-driven skip counter reads as "still inside navigation"
// for the entire rest of the document and returns a page containing only its
// title. html.Parse runs the HTML5 tree construction algorithm, which closes
// those the way a browser does, and skipping a subtree then becomes simply
// declining to walk into it — no counter to stay stuck.
func htmlToText(body io.Reader, base *url.URL) string {
	root, err := html.Parse(body)
	if err != nil {
		return ""
	}
	w := &textWriter{base: base}
	w.walk(root, 0)
	return w.done()
}

// walk renders one node and everything under it.
func (w *textWriter) walk(node *html.Node, depth int) {
	if depth > maxHTMLDepth {
		return
	}
	if node.Type == html.TextNode {
		w.text(node.Data)
		return
	}
	if node.Type == html.ElementNode {
		if skippedElements[node.Data] {
			return
		}
		w.open(node)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		w.walk(child, depth+1)
	}
	if node.Type == html.ElementNode {
		w.shut(node.Data)
	}
}

// open starts an element.
func (w *textWriter) open(node *html.Node) {
	if w.href != "" && structural(node.Data) {
		w.writeLink()
	}
	switch {
	case node.Data == "br":
		w.out.WriteString("\n")
	case node.Data == "li":
		w.out.WriteString("\n- ")
	case headingLevel[node.Data] > 0:
		w.out.WriteString("\n\n" + strings.Repeat("#", headingLevel[node.Data]) + " ")
	case node.Data == "a":
		w.href = w.resolve(attribute(node, "href"))
	case blockElements[node.Data]:
		w.out.WriteString("\n\n")
	}
	if node.Data == "pre" {
		w.pre++
	}
}

// structural reports whether an element breaks the line, which is what makes
// it unable to sit inside a markdown link.
func structural(name string) bool {
	return blockElements[name] || headingLevel[name] > 0
}

// shut ends an element, and is where a link becomes markdown: the text had to
// be collected first, because it sits between the tag carrying the URL and the
// tag ending it.
//
// A link is flushed at every block boundary as well, not only at </a>. HTML5
// lets an anchor wrap headings and paragraphs, which is how a documentation
// index builds its cards, and buffering across one of those put the heading
// markup in the output while its text was still being collected — an orphan
// "##" on its own line, then every block's text run together inside one link.
// Flushing per block gives each its own link to the same place, which is both
// correct markdown and what a model needs to follow the card.
func (w *textWriter) shut(name string) {
	if w.href != "" && structural(name) {
		w.writeLink()
	}
	switch {
	case name == "a":
		w.writeLink()
		w.href = ""
	case name == "pre":
		if w.pre > 0 {
			w.pre--
		}
		w.out.WriteString("\n")
	case headingLevel[name] > 0 || blockElements[name]:
		w.out.WriteString("\n")
	}
}
