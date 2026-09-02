// Package openrouter runs a nacelle agent on OpenRouter.
//
// OpenRouter fronts hundreds of models behind one OpenAI-compatible endpoint,
// which is what makes it worth having: comparing two models is a change to one
// string rather than a change of client. It costs capability to get that.
// There is no /v1/messages and no server-side MCP connector, so this backend
// drives the tool loop itself and cannot reach MCP servers at all — see
// Capabilities, and expect nacelle.New to refuse a config that asks for one.
//
// What it gains in return is money. OpenRouter prices every generation and
// returns the figure, so Usage.Cost is real here and zero on a backend that
// only counts tokens — with one caveat worth reading before dividing by it,
// under Capabilities.
package openrouter

import (
	"fmt"
	"os"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// DefaultBaseURL is OpenRouter's API root.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

// Config describes the backend.
type Config struct {
	// APIKey defaults to the OPENROUTER_API_KEY environment variable.
	APIKey string

	// Model is the OpenRouter model slug, such as
	// "anthropic/claude-opus-5" or "deepseek/deepseek-v4". Required:
	// OpenRouter fronts hundreds of models and a default would be this
	// package choosing the one thing the caller came here to choose.
	Model string

	// BaseURL defaults to DefaultBaseURL. Set it to point at a compatible
	// gateway or a recording proxy in tests.
	BaseURL string

	// Referer and Title are attribution, sent as HTTP-Referer and
	// X-OpenRouter-Title. Both are optional and neither affects the answer;
	// without a Referer the usage simply does not appear against an app.
	Referer string
	Title   string

	// Provider is OpenRouter's provider-routing object, passed through
	// untouched — `{"sort": "throughput"}`, `{"only": [...]}`,
	// `{"require_parameters": true}`. It is a map rather than a struct
	// because the routing options change faster than this package will.
	//
	// Worth setting `require_parameters` when tool calling matters: it
	// keeps the request away from providers that would drop the tool schema.
	Provider map[string]any

	// Options are extra request options for the underlying client.
	Options []option.RequestOption
}

// Backend runs agents on OpenRouter.
type Backend struct {
	client   openai.Client
	model    string
	provider map[string]any
}

var _ nacelle.Backend = (*Backend)(nil)

// New builds the backend. It fails rather than degrading: a missing key or
// model produces a 401 or a routing error on the first turn, which is a worse
// place to learn about it.
func New(cfg Config) (*Backend, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("nacelle/openrouter: a model is required")
	}
	key := cfg.APIKey
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("nacelle/openrouter: no API key; set OPENROUTER_API_KEY or Config.APIKey")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("OPENROUTER_BASE_URL")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	options := []option.RequestOption{option.WithBaseURL(baseURL), option.WithAPIKey(key)}
	if cfg.Referer != "" {
		options = append(options, option.WithHeader("HTTP-Referer", cfg.Referer))
	}
	if cfg.Title != "" {
		options = append(options, option.WithHeader("X-OpenRouter-Title", cfg.Title))
	}
	options = append(options, cfg.Options...)
	return &Backend{
		client:   openai.NewClient(options...),
		model:    cfg.Model,
		provider: cfg.Provider,
	}, nil
}

// Name identifies the backend.
func (b *Backend) Name() string { return "openrouter" }

// Model is the model slug this backend was built for.
func (b *Backend) Model() string { return b.model }

// Capabilities is where this backend's honesty lives.
//
// MCP is false and cannot be talked up: the Anthropic API connects to remote
// MCP servers on its own side of the request, and there is no equivalent in
// the OpenAI schema. An agent that needs MCP tools is refused here rather than
// run without them, which is the difference between a config error and a model
// that mysteriously never uses its tools.
//
// Cost is true because OpenRouter prices the generation and returns the
// number, which is the reason to reach for this backend when runs are being
// compared. It is a promise that the field is filled when the gateway reports
// a price, not that a price always arrives: cost travels as an extra field
// outside the OpenAI schema, and a provider that omits it, or sends it in a
// shape this package cannot decode, leaves Usage.Cost at zero.
//
// A zero cost therefore means "not reported", never "free". Usage has no
// third state to say which, so a consumer comparing runs on money should
// treat a zero total as missing data — the tokens are still counted, and
// pricing those is the fallback. It is only ever zero for a whole turn: the
// figure covers the generation, so there is no partial cost to mistake for a
// cheap one.
func (b *Backend) Capabilities() nacelle.Capabilities {
	return nacelle.Capabilities{MCP: false, Thinking: true, Effort: true, Cost: true, TokenCounting: false}
}
