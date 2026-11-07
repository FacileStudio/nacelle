package nacelle

import "iter"

// Transcript drains a run and returns every message it committed, in order,
// with the KindDone that ended it.
//
// The messages are what the model saw: each assistant turn with its reasoning,
// calls and Finish, and each round of tool results with their images. Appended
// to the conversation the run started from, they are the conversation to send
// next. A run that failed returns what it committed before the error, so a
// caller can keep the turns that did happen.
func Transcript(seq iter.Seq2[Event, error]) ([]Message, Event, error) {
	var messages []Message
	for event, err := range seq {
		if err != nil {
			return messages, Event{}, err
		}
		switch event.Kind {
		case KindMessage:
			messages = append(messages, *event.Message)
		case KindDone:
			return messages, event, nil
		}
	}
	return messages, Event{}, nil
}
