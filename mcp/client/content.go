package client

import (
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// flatten renders a tool result as the single string the model reads.
//
// Nothing is dropped in silence, and that is the whole reason this is not a
// two-line loop over the text parts. A model handed only the text of a result
// that also carried an image will reason as though the image was never there
// — it has no way to know it was edited — and will then answer confidently
// about a screenshot it never saw. tools.truncate takes the same line for the
// same reason: the notice matters more than the content it stands in for.
//
// StructuredContent is the fallback when there is no content at all. The spec
// says a server should always populate content, but one that does not is
// returning data, and answering "(no output)" to a call that produced some is
// the same silent edit by another route.
func flatten(result *sdk.CallToolResult) string {
	parts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		if rendered := describe(item); rendered != "" {
			parts = append(parts, rendered)
		}
	}
	if len(parts) == 0 {
		return structured(result.StructuredContent)
	}
	return strings.Join(parts, "\n")
}

// describe renders one content block.
//
// Only text passes through as itself. Everything else becomes a sentence
// saying what arrived, because this package's contract with the loop is a
// string: there is no channel here for an image, and inlining a megabyte of
// base64 would spend the context window on something the model cannot look
// at anyway.
func describe(item sdk.Content) string {
	switch item := item.(type) {
	case *sdk.TextContent:
		return sanitise(item.Text)
	case *sdk.ImageContent:
		return fmt.Sprintf("[image, %s, %d bytes, not shown]", mediaType(item.MIMEType), len(item.Data))
	case *sdk.AudioContent:
		return fmt.Sprintf("[audio, %s, %d bytes, not shown]", mediaType(item.MIMEType), len(item.Data))
	case *sdk.ResourceLink:
		return fmt.Sprintf("[link to resource %s, %s, not fetched]", item.URI, mediaType(item.MIMEType))
	case *sdk.EmbeddedResource:
		return embedded(item)
	default:
		return fmt.Sprintf("[%T, not shown]", item)
	}
}

// sanitise strips known instruction-tag patterns from tool output text so
// they are not interpreted by the model as directives, preventing one of the
// most common tool-poisoning attack vectors: a server returning output that
// contains hidden instructions for the model.
//
// Closing tags (</system>, </instruction>, etc.) are stripped first — they
// are unambiguous. Opening tags are then removed one at a time by nextTag,
// keeping the content between them so legitimate text survives.
//
// Tags are stripped with their content kept: <system>do X</system> becomes
// "do X". Stripping the tag and keeping the content loses the wrapper but not
// the instruction, which is deliberate — the model still sees the instruction
// as plain text in its context, but without the markup that tells it to treat
// the instruction as authoritative. A future here that wants stronger isolation
// can strip the content as well, at the cost of losing legitimate text.
func sanitise(text string) string {
	if !strings.ContainsAny(text, "<>") {
		return text
	}

	cleaned := text
	for _, tag := range []string{"</system>", "</instruction>", "</IMPORTANT>", "</tool>"} {
		cleaned = strings.ReplaceAll(cleaned, tag, "")
	}

	for {
		idx, end := nextTag(cleaned)
		if idx < 0 {
			break
		}
		cleaned = cleaned[:idx] + cleaned[end:]
	}
	return cleaned
}

// nextTag finds the earliest known opening tag in text and returns its
// start index and the position just past the closing >.
func nextTag(text string) (int, int) {
	best, end := -1, 0
	for _, prefix := range []string{"<system", "<instruction", "<IMPORTANT", "<tool"} {
		idx := strings.Index(text, prefix)
		if idx >= 0 && (best < 0 || idx < best) {
			close := strings.IndexByte(text[idx:], '>')
			if close >= 0 {
				best, end = idx, idx+close+1
			}
		}
	}
	return best, end
}

// embedded renders a resource the server inlined.
//
// A text resource is the interesting half and it is passed straight through:
// a server returning a file it read has returned text, and wrapping it in a
// notice would be describing content it is already holding.
func embedded(item *sdk.EmbeddedResource) string {
	switch {
	case item.Resource == nil:
		return "[embedded resource, empty]"
	case item.Resource.Text != "":
		return item.Resource.Text
	default:
		return fmt.Sprintf("[embedded resource %s, %s, %d bytes, not shown]",
			item.Resource.URI, mediaType(item.Resource.MIMEType), len(item.Resource.Blob))
	}
}

// structured renders a result that carried no content blocks.
func structured(value any) string {
	if value == nil {
		return "(no output)"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("[structured result, %s, not shown]", err)
	}
	return string(encoded)
}

// mediaType names a MIME type a server left out, so the notice reads as a
// sentence rather than trailing off into an empty pair of commas.
func mediaType(mime string) string {
	if mime == "" {
		return "unknown type"
	}
	return mime
}
