package client

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// render turns a tool result into what the model reads: the pictures it can
// look at, and text for everything else.
//
// Nothing is dropped in silence. A model handed only the text of a result
// that also carried something else will reason as though it was never there,
// so content with no slot in a Result is announced in a sentence instead.
// StructuredContent is the fallback when there is no content at all: a server
// that leaves content empty is returning data, and answering "(no output)" to
// a call that produced some is the same silent edit by another route.
func render(result *sdk.CallToolResult) nacelle.Result {
	var rendered nacelle.Result
	parts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		if image, ok := picture(item); ok {
			rendered.Images = append(rendered.Images, image)
			continue
		}
		if text := describe(item); text != "" {
			parts = append(parts, text)
		}
	}
	rendered.Text = strings.Join(parts, "\n")
	if len(parts) == 0 && len(rendered.Images) == 0 {
		rendered.Text = structured(result.StructuredContent)
	}
	return rendered
}

// picture is an image block a model API can take, as an Image.
func picture(item sdk.Content) (nacelle.Image, bool) {
	image, ok := item.(*sdk.ImageContent)
	if !ok || !slices.Contains(viewable, image.MIMEType) {
		return nacelle.Image{}, false
	}
	return nacelle.Image{MediaType: image.MIMEType, Data: image.Data}, true
}

// viewable are the image types every backend accepts.
var viewable = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// describe renders one content block that is not a picture the model can see.
//
// Only text passes through as itself. Everything else becomes a sentence
// saying what arrived, because inlining a megabyte of base64 would spend the
// context window on something the model cannot look at anyway.
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
