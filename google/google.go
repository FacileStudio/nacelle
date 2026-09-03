// Package google runs a nacelle agent on the Google Gemini API via its OpenAI-compatible endpoint.
package google

import (
	"fmt"
	"os"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// DefaultBaseURL is Google's OpenAI-compatible API root.
const DefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

// DefaultModel is what this backend runs on when Config leaves Model empty.
const DefaultModel = "gemini-3.7-flash"

// Config describes the backend.
type Config struct {
	APIKey  string
	Model   string
	BaseURL string
	Options []option.RequestOption
}

// Backend runs agents on Google Gemini.
type Backend struct {
	client oai.Client
	model  string
}

var _ nacelle.Backend = (*Backend)(nil)

// New builds the backend.
func New(cfg Config) (*Backend, error) {
	key := cfg.APIKey
	if key == "" {
		key = os.Getenv("GEMINI_API_KEY")
	}
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("nacelle/google: no API key; set GEMINI_API_KEY or Config.APIKey")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}

	options := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithAPIKey(key),
	}
	options = append(options, cfg.Options...)

	return &Backend{
		client: oai.NewClient(options...),
		model:  model,
	}, nil
}

// Name identifies the backend.
func (b *Backend) Name() string { return "google" }

// Model is the model slug this backend was built for.
func (b *Backend) Model() string { return b.model }

// Capabilities reports what this backend can do.
func (b *Backend) Capabilities() nacelle.Capabilities {
	return nacelle.Capabilities{
		MCP:             false,
		Thinking:        true,
		Effort:          true,
		Cost:            false,
		TokenCounting:   false,
		ToolCallPlanner: true,
		ContextWindow:   1000000,
	}
}
