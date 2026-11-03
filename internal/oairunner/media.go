package oairunner

import (
	"encoding/base64"

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
