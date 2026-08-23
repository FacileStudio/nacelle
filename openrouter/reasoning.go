package openrouter

import (
	"encoding/json"
	"slices"
)

// payloadKeys are the fields a reasoning block delivers in pieces.
//
// Everything else a block carries identifies it rather than fills it: an id,
// a format, a signature, the index. Those arrive whole, repeat unchanged on
// every fragment, and appending them would produce a signature no provider
// will accept. The three here are the documented content of the three block
// types OpenRouter defines, and they are the only fields that grow.
var payloadKeys = []string{"text", "summary", "data"}

// details reassembles the reasoning blocks a stream delivers in fragments.
//
// This exists because the obvious reading of the stream is wrong in a way
// nothing complains about. Each chunk carries a reasoning_details array, and
// it looks complete: a type, a format, an index, and some text. It is not. A
// single block arrives as a run of those, one word at a time, and OpenRouter's
// rule for sending them back is that the sequence must match what the model
// produced exactly, with nothing rearranged or modified. Keeping the last
// chunk seen therefore hands the model a one-word summary of its own last
// thought and calls it the whole thing. Measured against stealth/ox-alpha on
// 2026-08-23: fourteen chunks, all index 0, the last of them the single token
// "27.".
//
// The blocks are held as decoded JSON objects rather than a struct with the
// documented fields on it. A struct would drop any field OpenRouter adds after
// this was written, silently, on a round trip whose entire purpose is to
// return what arrived unmodified. Whatever a provider sends is carried back
// whether or not this package has a name for it.
type details struct {
	blocks []map[string]any
}

// add folds one chunk's fragments into the blocks assembled so far.
//
// A fragment that cannot be decoded is dropped rather than raised. Reasoning
// is not the answer, and a run that would otherwise have succeeded should not
// fail over a field this package only echoes; the model receives a shorter
// chain of thought, which is the same failure mode as a provider that sends no
// reasoning at all.
func (d *details) add(raw json.RawMessage) {
	var incoming []map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return
	}
	for _, fragment := range incoming {
		if open := d.open(); open != nil && continues(open, fragment) {
			merge(open, fragment)
			continue
		}
		d.blocks = append(d.blocks, fragment)
	}
}

// open is the block a fragment could still be continuing, which is only ever
// the last one.
//
// Only the last, deliberately. The blocks in a turn are consecutive: the model
// finishes one before it starts the next, so a fragment matching an earlier
// block's index is a new block that reuses a number, not a late addition to
// something already closed. Searching the whole slice would splice it into the
// wrong place and produce exactly the rearrangement the API forbids.
func (d *details) open() map[string]any {
	if len(d.blocks) == 0 {
		return nil
	}
	return d.blocks[len(d.blocks)-1]
}

// continues reports whether a fragment belongs to the block already open.
//
// Same position, and a type that does not contradict. A missing type counts as
// agreement in both directions because a provider that stamps the type on the
// first fragment only is continuing that block, and treating the silence as a
// mismatch would split one chain of thought in half. Two blocks that both name
// a type and disagree are two blocks, whatever their index says.
func continues(open, fragment map[string]any) bool {
	if position(open) != position(fragment) {
		return false
	}
	first, second := field(open, "type"), field(fragment, "type")
	return first == "" || second == "" || first == second
}

// merge appends a fragment's content to the block it continues.
func merge(open, fragment map[string]any) {
	for key, value := range fragment {
		if merged, changed := mergedValue(open[key], value, slices.Contains(payloadKeys, key)); changed {
			open[key] = merged
		}
	}
}

// mergedValue is what one key becomes when a later fragment carries it again,
// and whether it changes at all.
//
// A payload key grows and everything else is settled by whoever got there
// first, which is what keeps an id or a signature from being concatenated with
// itself once per chunk. A value that is not a string on both sides is left
// alone: the index is the case that matters, and a second copy of it carries
// nothing the first did not.
func mergedValue(existing, incoming any, grows bool) (any, bool) {
	if existing == nil {
		return incoming, true
	}
	was, wasText := existing.(string)
	addition, isText := incoming.(string)
	if !wasText || !isText {
		return nil, false
	}
	if grows {
		return was + addition, true
	}
	return addition, was == ""
}

// position is the block's place in the turn. A block that names no index is
// the first one, which is also what a provider that never sends an index gets:
// one block, assembled in arrival order.
func position(block map[string]any) float64 {
	value, _ := block["index"].(float64)
	return value
}

// field reads one string off a block, or the empty string when it is absent or
// is not a string.
func field(block map[string]any, key string) string {
	value, _ := block[key].(string)
	return value
}
