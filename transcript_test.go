package nacelle_test

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
	"time"
)

// readTranscript parses a session file into raw records, keeping key order
// out of the assertions.
func readTranscript(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var records []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(l), &rec); err != nil {
			t.Fatalf("bad line %q: %v", l, err)
		}
		records = append(records, rec)
	}
	return records
}

func openTestTranscript(t *testing.T) (*nacelle.Transcript, string) {
	t.Helper()
	dir := t.TempDir()
	tr, err := nacelle.OpenTranscript(nacelle.TranscriptOptions{Dir: dir})
	if err != nil {
		t.Fatalf("nacelle.OpenTranscript: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr, filepath.Join(dir, tr.SessionID()+".jsonl")
}

func TestTranscriptWritesVersionedJSONL(t *testing.T) {
	tr, path := openTestTranscript(t)
	tr.Record(nacelle.Event{Kind: nacelle.KindText, Text: "hello"})
	tr.Close()

	records := readTranscript(t, path)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 (session + text)", len(records))
	}
	for _, rec := range records {
		if v, ok := rec["v"].(float64); !ok || int(v) != nacelle.TranscriptSchemaVersion {
			t.Errorf("record missing v:%d: %v", nacelle.TranscriptSchemaVersion, rec)
		}
		if _, ok := rec["ts"].(string); !ok {
			t.Errorf("record missing ts: %v", rec)
		}
	}
	if records[1]["kind"] != string(nacelle.KindText) || records[1]["text"] != "hello" {
		t.Errorf("text record wrong: %v", records[1])
	}
}

func TestTranscriptDropsSecretShapedFieldsWhole(t *testing.T) {
	tr, path := openTestTranscript(t)
	cases := []string{
		"the key is sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaa",
		"token = hunter2hunter2hunter2",
		"AKIAIOSFODNN7EXAMPLE in a result",
	}
	for _, s := range cases {
		tr.Record(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{ID: "t1", Name: "env", Result: s}})
	}
	tr.Record(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{ID: "t2", Name: "safe", Result: "clean output"}})
	tr.Record(nacelle.Event{Kind: nacelle.KindText, Text: "password: supersecret99 words"})
	tr.Close()

	records := readTranscript(t, path)
	droppedResults, clean := 0, 0
	for _, rec := range records[1:] {
		tool, _ := rec["tool"].(map[string]any)
		if tool == nil {
			continue
		}
		result, _ := tool["result"].(string)
		switch result {
		case "":
			dropped, _ := tool["dropped"].([]any)
			if len(dropped) == 0 || dropped[0] != "result" {
				t.Errorf("dropped field not named: %v", tool)
			}
			droppedResults++
		case "clean output":
			if _, has := tool["dropped"]; has {
				t.Errorf("clean record claims drops: %v", tool)
			}
			clean++
		default:
			t.Errorf("secret survived to disk: %q", result)
		}
	}
	if droppedResults != 3 || clean != 1 {
		t.Errorf("got %d dropped / %d clean results, want 3 / 1", droppedResults, clean)
	}
	for _, rec := range records {
		if text, _ := rec["text"].(string); strings.Contains(text, "supersecret99") {
			t.Errorf("secret text survived: %q", text)
		}
	}
}

func TestTranscriptCapsToolBodiesAndSidesTheRest(t *testing.T) {
	dir2 := t.TempDir()
	tr, err := nacelle.OpenTranscript(nacelle.TranscriptOptions{Dir: dir2, BodyCap: 64})
	if err != nil {
		t.Fatalf("nacelle.OpenTranscript: %v", err)
	}
	body := strings.Repeat("x", 200)
	tr.Record(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{ID: "big", Result: body}})
	tr.Close()

	main := readTranscript(t, filepath.Join(dir2, tr.SessionID()+".jsonl"))
	tool, _ := main[len(main)-1]["tool"].(map[string]any)
	if got, _ := tool["result"].(string); len(got) > 64 {
		t.Errorf("main file kept %d bytes, cap is 64", len(got))
	}

	sidecarPath := filepath.Join(dir2, tr.SessionID()+".jsonl.gz")
	f, err := os.Open(sidecarPath)
	if err != nil {
		t.Fatalf("no sidecar written: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("sidecar is not gzip: %v", err)
	}
	raw, _ := io.ReadAll(gz)
	var sideRecs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var rec map[string]any
		json.Unmarshal([]byte(l), &rec)
		sideRecs = append(sideRecs, rec)
	}
	if len(sideRecs) != 1 {
		t.Fatalf("got %d sidecar records, want 1", len(sideRecs))
	}
	if got, _ := sideRecs[0]["body"].(string); got != body {
		t.Errorf("sidecar body truncated: %d bytes, want %d", len(got), len(body))
	}
	if sideRecs[0]["field"] != "result" {
		t.Errorf("sidecar field wrong: %v", sideRecs[0]["field"])
	}
}

func TestRecordTrimIsMetadataOnly(t *testing.T) {
	tr, path := openTestTranscript(t)
	tr.RecordTrim(7)
	tr.Record(nacelle.Event{Kind: nacelle.KindText, Text: "still here"})
	tr.Close()

	for _, rec := range readTranscript(t, path) {
		if rec["kind"] != "trim" {
			continue
		}
		n, _ := rec["trimmed"].(float64)
		if int(n) != 7 {
			t.Errorf("trim count %v, want 7", rec["trimmed"])
		}
		for key := range rec {
			if key == "text" || key == "tool" || key == "usage" {
				t.Errorf("trim record carries content under %q: %v", key, rec)
			}
		}
	}
}

func TestUsageAndStopAreRecordedOnTurns(t *testing.T) {
	tr, path := openTestTranscript(t)
	tr.Record(nacelle.Event{
		Kind:  nacelle.KindTurn,
		Stop:  nacelle.StopTools,
		Usage: nacelle.Usage{InputTokens: 10, OutputTokens: 5, Cost: 0.25},
	})
	tr.Close()

	last := readTranscript(t, path)[len(readTranscript(t, path))-1]
	if last["stop"] != string(nacelle.StopTools) {
		t.Errorf("stop not recorded: %v", last)
	}
	usage, _ := last["usage"].(map[string]any)
	if usage == nil || usage["input_tokens"].(float64) != 10 || usage["cost"].(float64) != 0.25 {
		t.Errorf("usage not recorded: %v", usage)
	}
}

func TestOpenGzipsSessionsOlderThanFourteenDays(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "20260101-000000-dead.jsonl")
	os.WriteFile(old, []byte(`{"v":1,"ts":"old","kind":"session"}`+"\n"), 0o600)
	stale := time.Now().Add(-20 * 24 * time.Hour)
	os.Chtimes(old, stale, stale)

	fresh := filepath.Join(dir, "fresh.jsonl")
	os.WriteFile(fresh, []byte("recent\n"), 0o600)

	tr, err := nacelle.OpenTranscript(nacelle.TranscriptOptions{Dir: dir})
	if err != nil {
		t.Fatalf("nacelle.OpenTranscript: %v", err)
	}
	tr.Close()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old session file still present")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("recent session file was rotated: %v", err)
	}
	gz, err := os.Open(old + ".gz")
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	defer gz.Close()
	zr, err := gzip.NewReader(gz)
	if err != nil {
		t.Fatalf("rotated file is not gzip: %v", err)
	}
	content, _ := io.ReadAll(zr)
	if !strings.Contains(string(content), `"kind":"session"`) {
		t.Errorf("rotated content lost: %q", content)
	}
}

func TestAgentStreamFeedsTranscript(t *testing.T) {
	dir := t.TempDir()
	tr, err := nacelle.OpenTranscript(nacelle.TranscriptOptions{Dir: dir})
	if err != nil {
		t.Fatalf("nacelle.OpenTranscript: %v", err)
	}
	agent, err := nacelle.New(nacelle.Config{Backend: full(), System: "sys", Transcript: tr})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := t.Context()
	for event, err := range agent.Stream(ctx, []nacelle.Message{{Role: nacelle.RoleUser, Parts: []nacelle.Part{nacelle.Text{Text: "hi"}}}}) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		_ = event
	}
	tr.Close()

	records := readTranscript(t, filepath.Join(dir, tr.SessionID()+".jsonl"))
	kinds := map[string]bool{}
	for _, rec := range records[1:] {
		kinds[rec["kind"].(string)] = true
	}
	if !kinds[string(nacelle.KindDone)] {
		t.Errorf("stream events did not reach the transcript: %v", kinds)
	}
}
