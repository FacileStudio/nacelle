// Package openai runs a nacelle agent on the OpenAI API.
package openai

import (
	"context"
	"fmt"
	"iter"
	"os"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/internal/oairunner"

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
	return nacelle.Capabilities{
		MCP:             false,
		Thinking:        true,
		Effort:          true,
		Cost:            false,
		TokenCounting:   false,
		ToolCallPlanner: true,
		ContextWindow:   128000,
	}
}

// Stream delegates to the shared OpenAI runner.
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return (&oairunner.Backend{
		Client:       b.client,
		Model:        b.model,
		RequestOptions: b.requestOptions,
	}).Stream(ctx, request)
}

// CountTokens delegates to the shared OpenAI runner.
func (b *Backend) CountTokens(ctx context.Context, request nacelle.Request) (int64, error) {
	return (&oairunner.Backend{
		Client:       b.client,
		Model:        b.model,
		RequestOptions: b.requestOptions,
	}).CountTokens(ctx, request)
}

func (b *Backend) requestOptions(request nacelle.Request) []option.RequestOption {
	var options []option.RequestOption
	if effort := reasoningEffort(request.Thinking); effort != "" {
		options = append(options, option.WithJSONSet("reasoning_effort", effort))
	}
	return options
}

func reasoningEffort(thinking nacelle.Thinking) string {
	switch thinking.Effort {
	case nacelle.EffortNone, "":
		return ""
	case nacelle.EffortMinimal, nacelle.EffortLow:
		return "low"
	case nacelle.EffortMedium:
		return "medium"
	case nacelle.EffortHigh, nacelle.EffortXHigh, nacelle.EffortMax:
		return "high"
	default:
		return string(thinking.Effort)
	}
}