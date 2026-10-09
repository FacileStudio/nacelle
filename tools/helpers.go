package tools

import (
	"fmt"
	"os"
	"strings"

	"github.com/FacileStudio/nacelle"
)

func minimalEnv() []string {
	env := []string{"PATH=" + commandPath()}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

// buildAll collects tool constructors, failing on the first that cannot build
// its schema.
func buildAll(builders ...func() (nacelle.Tool, error)) ([]nacelle.Tool, error) {
	built := make([]nacelle.Tool, 0, len(builders))
	for _, build := range builders {
		tool, err := build()
		if err != nil {
			return nil, err
		}
		built = append(built, tool)
	}
	return built, nil
}

// truncate caps text at limit, saying so rather than trimming in silence.
//
// The notice matters more than the trimming: a model handed a quietly cut file
// will reason about it as if it were whole, and confidently describe the end
// of something it never saw.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	kept := text[:limit]
	if cut := strings.LastIndexByte(kept, '\n'); cut > limit/2 {
		kept = kept[:cut]
	}
	return kept + fmt.Sprintf("\n\n[truncated: %d of %d bytes shown]", len(kept), len(text))
}
