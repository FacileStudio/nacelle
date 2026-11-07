package nacelle

import (
	"encoding/json"
	"fmt"
)

// MarshalMessages encodes a conversation as JSON, every part included:
// reasoning, tool images, documents, Finish and Native parts survive the
// round trip through UnmarshalMessages byte for byte.
func MarshalMessages(messages []Message) ([]byte, error) {
	return json.Marshal(messages)
}

// UnmarshalMessages decodes a conversation MarshalMessages encoded. A part
// of a type this package does not know is an error rather than a silent gap.
func UnmarshalMessages(data []byte) ([]Message, error) {
	var messages []Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// wireMessage is a Message as it is stored.
type wireMessage struct {
	Role  Role       `json:"role"`
	Parts []wirePart `json:"parts"`
}

// wirePart is every Part as it is stored, told apart by Type. The field names
// are the ones Bulle stored transcripts with before this codec existed, so
// those transcripts decode unchanged. A Native part keeps its raw JSON in
// input, the one field that already holds raw JSON.
type wirePart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Finished bool            `json:"finished,omitempty"`
	Result   string          `json:"result,omitempty"`
	Failed   bool            `json:"failed,omitempty"`
	Images   []Image         `json:"images,omitempty"`
	Stop     Stop            `json:"stop,omitempty"`
	Provider string          `json:"provider,omitempty"`
	wireMedia
}

// wireMedia is the bytes of an Image or a Document part.
type wireMedia struct {
	Media string `json:"media_type,omitempty"`
	Data  []byte `json:"data,omitempty"`
}

// MarshalJSON encodes the message with its parts tagged by type.
func (m Message) MarshalJSON() ([]byte, error) {
	wire := wireMessage{Role: m.Role, Parts: make([]wirePart, 0, len(m.Parts))}
	for _, part := range m.Parts {
		encoded, err := encodePart(part)
		if err != nil {
			return nil, err
		}
		wire.Parts = append(wire.Parts, encoded)
	}
	return json.Marshal(wire)
}

// UnmarshalJSON decodes a message MarshalJSON encoded.
func (m *Message) UnmarshalJSON(data []byte) error {
	var wire wireMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	decoded := Message{Role: wire.Role, Parts: make([]Part, 0, len(wire.Parts))}
	for _, part := range wire.Parts {
		next, err := part.decode()
		if err != nil {
			return err
		}
		decoded.Parts = append(decoded.Parts, next)
	}
	*m = decoded
	return nil
}

// encodePart tags one part with its type.
func encodePart(part Part) (wirePart, error) {
	switch p := part.(type) {
	case Text:
		return wirePart{Type: "text", Text: p.Text}, nil
	case Reasoning:
		return wirePart{Type: "reasoning", Text: p.Text}, nil
	case ToolCall:
		return wirePart{Type: "tool_call", ID: p.ID, Name: p.Name, Input: validJSON(p.Input), Finished: p.Finished}, nil
	case ToolResult:
		return wirePart{Type: "tool_result", ID: p.ID, Name: p.Name, Result: p.Result, Failed: p.Failed, Images: p.Images}, nil
	case Finish:
		return wirePart{Type: "finish", Stop: p.Stop}, nil
	case Image:
		return wirePart{Type: "image", wireMedia: wireMedia{Media: p.MediaType, Data: p.Data}}, nil
	case Document:
		return wirePart{Type: "document", Name: p.Name, wireMedia: wireMedia{Media: p.MediaType, Data: p.Data}}, nil
	case Native:
		return wirePart{Type: "native", Provider: p.Provider, Input: validJSON(p.Data)}, nil
	}
	return wirePart{}, fmt.Errorf("nacelle: cannot encode a %T part", part)
}

// validJSON keeps raw JSON that parses and drops what does not, because a
// call abandoned mid-stream holds half an object and a message carrying it
// would not encode at all.
func validJSON(raw json.RawMessage) json.RawMessage {
	if !json.Valid(raw) {
		return nil
	}
	return raw
}

// decode turns a stored part back into the Part it was.
func (w wirePart) decode() (Part, error) {
	switch w.Type {
	case "text":
		return Text{Text: w.Text}, nil
	case "reasoning":
		return Reasoning{Text: w.Text}, nil
	case "tool_call":
		return ToolCall{ID: w.ID, Name: w.Name, Input: w.Input, Finished: w.Finished}, nil
	case "tool_result":
		return ToolResult{ID: w.ID, Name: w.Name, Result: w.Result, Failed: w.Failed, Images: w.Images}, nil
	case "finish":
		return Finish{Stop: w.Stop}, nil
	case "image":
		return Image{MediaType: w.Media, Data: w.Data}, nil
	case "document":
		return Document{Name: w.Name, MediaType: w.Media, Data: w.Data}, nil
	case "native":
		return Native{Provider: w.Provider, Data: w.Input}, nil
	}
	return nil, fmt.Errorf("nacelle: unknown stored part type %q", w.Type)
}
