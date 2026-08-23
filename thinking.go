package nacelle

// Effort tunes how hard the model works, trading cost against quality.
//
// It replaces the fixed thinking budget older models took: a token budget is
// rejected outright by current Anthropic models, and this is what took its
// place. A backend that does not support it at all says so in its
// Capabilities.
//
// Nothing here checks a level against the model that will receive it, and that
// is deliberate. Measured against OpenRouter on 2026-08-23: a level a model
// does not advertise is clamped to one it does rather than refused, so a table
// of which model takes which would be a maintenance cost carrying a wrong
// answer from the week a provider adds a level. The refusal worth making is
// the one Capabilities already makes.
type Effort string

const (
	// EffortNone asks for no reasoning at all, and a model that cannot
	// oblige refuses the run rather than quietly ignoring it. Measured
	// against stealth/ox-alpha on 2026-08-23: OpenRouter answers a request
	// carrying it with 400, "Reasoning is mandatory for this endpoint and
	// cannot be disabled". That is the right outcome and it is why this is
	// its own level rather than a synonym for the cheapest one: a caller
	// who needs a model not to think has been told plainly that this model
	// always will, instead of being billed for reasoning they asked to
	// skip. The error is not marked retryable, so it fails once.
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

// Thinking is how hard the model thinks, and who gets to see it.
//
// Those are two questions, which is why this is a struct and not the bool it
// replaced. There used to be a third, and removing it is the point of this
// type: whether the reasoning travels back over the wire was wired to whether
// a human wanted to watch it, so the default configuration asked every
// provider to throw the reasoning away, and every tool call after the first
// handed the model a blank where its own last thought should have been.
//
// It always travels now. Nothing here asks a provider to withhold it, because
// the reasoning tokens are billed whether or not they come back and a loop
// that drops them is the one case where the saving is real and the cost is
// correctness. Show decides what a consumer is shown, and nothing else.
type Thinking struct {
	// Effort defaults to the backend's own default when empty.
	Effort Effort

	// Budget caps the tokens one turn may spend on reasoning. Zero means
	// no ceiling from here, which is not the same as EffortNone: the
	// backend still applies whatever it defaults to.
	//
	// Effort and Budget are two spellings of one idea, and the providers
	// disagree about how to take them. Anthropic takes both at once, on
	// separate fields. OpenRouter refuses the pair with a 400 and each
	// backend therefore resolves it its own way, the OpenRouter one by
	// letting a budget win: the levels are documented there as percentages
	// of the budget, so the precise number is the coarse one said properly.
	// Set one or the other unless a backend swap is the point.
	Budget int64

	// Show streams the model's reasoning as KindThinking events.
	//
	// Off by default, which matches the APIs: the raw chain of thought is
	// never returned and a readable summary is opt-in. Turning it on
	// changes what is displayed, never what is billed. The model thinks
	// either way and the tokens are on the invoice either way.
	Show bool
}
