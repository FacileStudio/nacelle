package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// tagged finds every jsonschema struct tag in this package's own source.
var tagged = regexp.MustCompile(`jsonschema:"([^"]*)"`)

// A tool's description is the only instruction the model gets about how to
// call it, and it reaches the model through a struct tag.
//
// invopop/jsonschema splits that tag on unescaped commas, so a description
// containing one is silently cut short at it — no error, nothing in the
// build, just half a sentence arriving at the model. Five shipped
// descriptions were losing their second half this way; the one that cost
// something was read_file's offset, which stopped telling the model the count
// starts at 1.
//
// This reads the source rather than the built schema, and it has to. The
// truncation takes the comma with it, so a description that lost half of
// itself comes back through the schema looking perfectly well formed — there
// is nothing left in the output to detect. The evidence only exists in the
// tag.
//
// Escaping is not the fix either. A `\,` is not a valid Go escape, so
// reflect.StructTag.Get cannot unquote the value and hands back an empty
// string, losing the whole description instead of half. The rule is that
// these carry no commas, and this is what says so out loud.
func TestNoDescriptionTagCarriesACommaThatWouldTruncateIt(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing this package's sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no sources found, so this would pass without checking anything")
	}

	for _, name := range sources {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		for _, match := range tagged.FindAllStringSubmatch(string(body), -1) {
			_, description, found := strings.Cut(match[1], "description=")
			if !found {
				continue
			}
			if strings.Contains(description, ",") {
				t.Errorf("%s: %q — everything from the comma on is dropped before the model sees it", name, description)
			}
		}
	}
}
