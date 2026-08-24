package nacelle

import "regexp"

// line is one JSONL record in a session file. Every line carries the schema
// version and a timestamp; the rest is present per kind.
type line struct {
	V       int        `json:"v"`
	TS      string     `json:"ts"`
	Kind    Kind       `json:"kind"`
	Text    string     `json:"text,omitempty"`
	Tool    *toolLine  `json:"tool,omitempty"`
	Trimmed *int       `json:"trimmed,omitempty"`
	Stop    Stop       `json:"stop,omitempty"`
	Usage   *usageLine `json:"usage,omitempty"`
	Dropped []string   `json:"dropped,omitempty"`
}

// toolLine is the tool section of a call or result record.
type toolLine struct {
	ID         string   `json:"id,omitempty"`
	Name       string   `json:"name,omitempty"`
	Input      string   `json:"input,omitempty"`
	Result     string   `json:"result,omitempty"`
	Err        string   `json:"error,omitempty"`
	DurationMS int64    `json:"duration_ms,omitempty"`
	Discarded  bool     `json:"discarded,omitempty"`
	Refused    bool     `json:"refused,omitempty"`
	Dropped    []string `json:"dropped,omitempty"`
}

// usageLine is the cost section of a turn or done record.
type usageLine struct {
	Input         int64   `json:"input_tokens,omitempty"`
	Output        int64   `json:"output_tokens,omitempty"`
	CacheRead     int64   `json:"cache_read_tokens,omitempty"`
	CacheCreation int64   `json:"cache_creation_tokens,omitempty"`
	Cost          float64 `json:"cost,omitempty"`
}

// sidecarLine is one uncapped tool body in the gzipped sidecar.
type sidecarLine struct {
	V     int    `json:"v"`
	TS    string `json:"ts"`
	Field string `json:"field"`
	Body  string `json:"body"`
}

// toolLine builds the record's tool section, applying redaction and the body
// cap. A doubtful field is dropped whole and named in Dropped; a large but
// clean field is capped here and sent whole to the sidecar.
func (t *Transcript) toolLine(event Event) *toolLine {
	out := &toolLine{
		ID:        event.Tool.ID,
		Name:      event.Tool.Name,
		Discarded: event.Tool.Discarded,
		Refused:   event.Tool.Refused,
	}
	if event.Tool.Duration > 0 {
		out.DurationMS = event.Tool.Duration.Milliseconds()
	}
	out.Input, out.Result, out.Dropped = t.toolBodies(event.Tool.Input, event.Tool.Result, nil)
	if event.Tool.Err != nil {
		if looksSecret(event.Tool.Err.Error()) {
			out.Dropped = append(out.Dropped, "error")
		} else {
			out.Err = event.Tool.Err.Error()
		}
	}
	return out
}

// toolBodies runs both tool body fields through redaction and the cap,
// returning what the record keeps and the names of any fields dropped as
// doubtful.
func (t *Transcript) toolBodies(input, result string, dropped []string) (in, res string, _ []string) {
	for _, one := range []struct {
		name, text string
		dst        *string
	}{{"input", input, &in}, {"result", result, &res}} {
		switch s, kept, drop := t.body(one.name, one.text); {
		case drop:
			dropped = append(dropped, one.name)
		case kept:
			*one.dst = s
		}
	}
	return in, res, dropped
}

// body handles one tool body field. A secret-shaped body returns drop —
// the doubt goes to the whole field, never partially to the bytes. Otherwise
// kept reports whether the whole text fit (an oversized clean body was
// capped here and sent whole to the sidecar).
func (t *Transcript) body(field, s string) (text string, kept, drop bool) {
	if looksSecret(s) {
		return "", false, true
	}
	if len(s) <= t.bodyCap {
		return s, true, false
	}
	t.sidecarWrite(sidecarLine{V: TranscriptSchemaVersion, TS: t.stamp(), Field: field, Body: s})
	return truncateRunes(s, t.bodyCap), false, false
}

// secretShapes are the credential forms worth refusing to write. The list
// covers the providers this suite actually uses plus the generic
// key=value assignment, and errs on the side of dropping: a false positive
// costs one omitted field, a false negative costs a secret on disk.
var secretShapes = []*regexp.Regexp{
	regexp.MustCompile(`sk-(ant-)?[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|password|passphrase|token)\b\s*[=:]\s*["']?[^\s"']{8,}`),
}

// looksSecret reports whether s carries anything shaped like a credential.
func looksSecret(s string) bool {
	for _, re := range secretShapes {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// truncateRunes cuts s to at most max bytes without splitting one.
func truncateRunes(s string, max int) string {
	for len(s) > max {
		s = s[:max]
		r := []rune(s)
		s = string(r[:len(r)-1])
	}
	return s
}

// newUsageLine converts a Usage into its record shape.
func newUsageLine(u Usage) usageLine {
	return usageLine{
		Input:         u.InputTokens,
		Output:        u.OutputTokens,
		CacheRead:     u.CacheReadTokens,
		CacheCreation: u.CacheCreationTokens,
		Cost:          u.Cost,
	}
}
