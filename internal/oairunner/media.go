package oairunner

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

func (s *sorted) addMedia(part oai.ChatCompletionContentPartUnionParam) {
	s.media = true
	s.content = append(s.content, part)
}

func imagePart(image nacelle.Image) oai.ChatCompletionContentPartUnionParam {
	return oai.ImageContentPart(oai.ChatCompletionContentPartImageImageURLParam{
		URL: dataURI(image.MediaType, image.Data),
	})
}

func filePart(document nacelle.Document) oai.ChatCompletionContentPartUnionParam {
	file := oai.ChatCompletionContentPartFileFileParam{FileData: oai.String(dataURI(document.MediaType, document.Data))}
	if document.Name != "" {
		file.Filename = oai.String(document.Name)
	}
	return oai.FileContentPart(file)
}

func dataURI(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (s *sorted) addResult(result nacelle.ToolResult) {
	text := result.Result
	if result.Failed {
		text = "the tool failed: " + text
	}
	s.results = append(s.results, oai.ToolMessage(text, result.ID))
	for _, part := range toolImageParts(result.ID, result.Name, result.Images) {
		s.addMedia(part)
	}
}

func toolImageParts(id, name string, images []nacelle.Image) []oai.ChatCompletionContentPartUnionParam {
	if len(images) == 0 {
		return nil
	}
	parts := []oai.ChatCompletionContentPartUnionParam{
		oai.TextContentPart(fmt.Sprintf("Images returned by the %s tool call %s:", name, id)),
	}
	for _, image := range images {
		parts = append(parts, imagePart(image))
	}
	return parts
}

// addNative merges the extra fields this provider wrote on an assistant turn.
// A part another provider wrote is not this one's to send.
func (s *sorted) addNative(name string, part nacelle.Native) {
	var extra map[string]any
	if part.Provider != name || json.Unmarshal(part.Data, &extra) != nil {
		return
	}
	if s.extra == nil {
		s.extra = map[string]any{}
	}
	maps.Copy(s.extra, extra)
}
