package nacelle

import "encoding/json"

// Role is whose turn a Message is.
//
// There are two, because two is what the model APIs agree on. A tool's answer
// is not a third voice: Anthropic carries it as a block inside the user turn
// and the OpenAI schema as a message of its own, and reconciling that is a
// backend's job at its own edge rather than a split this package repeats for
// both of them.
type Role string

const (
	// RoleUser is the caller's side of the conversation, and also where the
	// results of tools the model asked for are carried.
	RoleUser Role = "user"

	// RoleAssistant is the model's side.
	RoleAssistant Role = "assistant"
)

// Message is one turn of the conversation so far.
//
// Its content is a list of parts rather than a string, because a turn is often
// not prose. An assistant turn that used tools is text and tool calls together,
// and the turn answering it is tool results; a message that could hold only a
// string dropped every one of them. What that cost was not cosmetic. A resumed
// conversation asked the model to carry on from a transcript it had not
// produced, and cross-call prompt caching could never hit at all, because a
// replayed prefix cannot byte-match a request whose tool blocks were thrown
// away on the way in.
type Message struct {
	Role  Role
	Parts []Part
}

// Part is one piece of a message's content.
//
// The set is closed: part is unexported, so no type outside this package can
// join it, and a type switch over the five below is exhaustive today and stays
// exhaustive. That is why this is an interface and not a struct with a kind
// and eleven optional fields — which is the shape Event uses, and the shape
// that would let a backend read a tool call's arguments off a piece of prose.
//
// Every part is implemented on a value receiver, so a literal is a Part and
// nothing has to be addressable to go into a conversation.
type Part interface {
	part()
}

// Text is prose, from either side of the conversation.
type Text struct{ Text string }

// Reasoning is the model thinking out loud: shown, recorded, and never sent
// back.
//
// It is representable because the stream emits it, and a conversation that
// cannot hold what a consumer displayed is the same gap this type exists to
// close, one level down. Both backends drop it when they build a request, and
// that is not an oversight. Anthropic accepts a thinking block only with the
// signature it was issued with, which the stream does not carry, and OpenRouter
// is asked to exclude reasoning unless the caller opted in. Replaying it would
// mean paying again for a chain of thought, in a field the providers do not
// want it replayed in.
type Reasoning struct{ Text string }

// ToolCall is the model asking for a tool to be run.
type ToolCall struct {
	// ID is the model's identifier for this call, and the one the ToolResult
	// answering it carries.
	ID string

	// Name is the tool that was asked for.
	Name string

	// Input is the raw JSON the model wrote. It is not decoded here, because
	// the core knows no tool's schema, and it is kept byte for byte because
	// re-encoding it is what stops a replayed prefix matching the request
	// that was cached.
	Input json.RawMessage

	// Finished reports whether the arguments are whole.
	//
	// They arrive as JSON fragments, so a run abandoned mid-call leaves a
	// call whose Input is a truncated object. Recording it is how a
	// transcript stays honest about what happened; the false is how a
	// backend knows not to send it, because half an argument list is a
	// rejected request rather than a partial one.
	Finished bool
}

// ToolResult is what a tool returned, and the other half of the pairing.
type ToolResult struct {
	// ID is the ToolCall this answers.
	ID string

	// Name is the tool that ran, repeated so a result reads on its own.
	Name string

	// Result is what the model is told: the tool's own text, or what went
	// wrong.
	Result string

	// Failed reports that the tool errored.
	//
	// A bool rather than an error, because a conversation is a value that is
	// stored, compared and replayed and an error is none of those things: it
	// does not survive a round trip through JSON, and two conversations
	// carrying the same failure would not compare equal. What the model needs
	// is in Result either way.
	Failed bool
}

// Finish is why a turn ended, recorded where it ended.
//
// It is the Event's Stop, kept so a conversation read back later can still tell
// an answer that was finished from one the token ceiling cut off. Neither wire
// format has a field for it, so no backend sends it.
type Finish struct{ Stop Stop }

func (Text) part()       {}
func (Reasoning) part()  {}
func (ToolCall) part()   {}
func (ToolResult) part() {}
func (Finish) part()     {}

// UserText is the ordinary case: somebody asked something.
func UserText(text string) Message {
	return Message{Role: RoleUser, Parts: []Part{Text{Text: text}}}
}

// AssistantText is a model turn that was prose and nothing else.
func AssistantText(text string) Message {
	return Message{Role: RoleAssistant, Parts: []Part{Text{Text: text}}}
}
