// Package openai runs a nacelle agent on the OpenAI API.
package openai

import (
	"fmt"
	"os"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// DefaultBaseURL is OpenAI's API root.
const DefaultBaseURL = "https://api.openai.com/v1"

// DefaultModel is what this backend runs on when Config leaves Model empty.
const DefaultModel = "gpt-5.4"

// Config describes the backend.
type Config struct {
	APIKey       string
	Model        string
	BaseURL      string
	Organization string
	Project      string
	Options      []option.RequestOption
}

// Backend runs agents on OpenAI.
type Backend struct {
	client oai.Client
	model  string
}

var _ nacelle.Backend = (*Backend)(nil)

// New builds the backend.
func New(cfg Config) (*Backend, error) {
	key := cfg.APIKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("nacelle/openai: no API key; set OPENAI_API_KEY or Config.APIKey")
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
	if cfg.Organization != "" {
		options = append(options, option.WithHeader("OpenAI-Organization", cfg.Organization))
	}
	if cfg.Project != "" {
		options = append(options, option.WithHeader("OpenAI-Project", cfg.Project))
	}
	options = append(options, cfg.Options...)

	return &Backend{
		client: oai.NewClient(options...),
		model:  model,
	}, nil
}

// Name identifies the backend.
func (b *Backend) Name() string { return "openai" }

// Model is the model slug this backend was built for.
func (b *Backend) Model() string { return b.model }

// Capabilities reports what this backend can do.
func (b *Backend) Capabilities() nacelle.Capabilities {
	return nacelle.Capabilities{MCP: false, Thinking: true, Effort: true, Cost: false, TokenCounting: false}
}
