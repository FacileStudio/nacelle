package tools

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// writeLink emits the finished anchor, dropping the URL when it would only
// repeat the text and keeping the text when there is no usable URL.
func (w *textWriter) writeLink() {
	text := strings.TrimSpace(w.link.String())
	w.link.Reset()

	rendered := text
	if w.href != "" && text != "" && text != w.href {
		rendered = "[" + text + "](" + w.href + ")"
	}
	if rendered == "" {
		return
	}
	if needsSpace(w.out.String(), rendered) {
		w.out.WriteString(" ")
	}
	w.out.WriteString(rendered)
}

// text adds a run of characters, collapsing whitespace outside a pre block
// because HTML indentation is not the author's spacing and costs tokens to
// carry.
func (w *textWriter) text(raw string) {
	target := &w.out
	if w.href != "" {
		target = &w.link
	}
	if w.pre > 0 {
		target.WriteString(raw)
		return
	}
	if collapsed := strings.Join(strings.Fields(raw), " "); collapsed != "" {
		if needsSpace(target.String(), collapsed) {
			target.WriteString(" ")
		}
		target.WriteString(collapsed)
	}
}

// resolve turns a page-relative href into one the model can fetch on its own.
func (w *textWriter) resolve(href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if w.base != nil {
		parsed = w.base.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

// done is the finished page, with the blank lines the writers above scattered
// freely reduced to the ones a reader would keep.
func (w *textWriter) done() string {
	text := strings.ReplaceAll(w.out.String(), "\r\n", "\n")
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// needsSpace reports whether the next run wants a space in front of it.
//
// Both ends have a say. Nothing is wanted at the start of a line or after one
// already there, and nothing is wanted before punctuation that closes what
// came before it — a link followed by a comma is "text, more", never
// "text , more", and that pairing is common enough in prose that getting it
// wrong is visible on every page.
func needsSpace(before, next string) bool {
	if before == "" || next == "" {
		return false
	}
	switch before[len(before)-1] {
	case '\n', ' ', '-':
		return false
	}
	return !strings.ContainsRune(",.;:!?)]}'\"", rune(next[0]))
}

// attribute reads one attribute off a token, or "" when it is absent.
func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}
