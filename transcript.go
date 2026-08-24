package nacelle

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Transcript records what happened during runs as JSONL, one file per
// session, under a directory (by default ~/.nacelle/sessions).
//
// It exists because an agent run is cheap to start and expensive to remember:
// a consumer that wants to answer "what did the model actually do last
// Tuesday" needs the events as they happened, not reconstructed from a UI.
//
// Three properties are fixed and none is negotiable. Redaction happens at
// write time, not later: a value that looks like a credential is dropped from
// the record entirely — field-drop-on-doubt, because a redacted-but-present
// secret is still a secret on disk. Tool bodies are capped in the main file
// at DefaultToolBodyCap with the full text preserved in a gzipped sidecar, so
// the readable file stays readable without losing evidence. And trim events
// carry counts only, never the content that was dropped, so a context trim
// cannot become a second copy of everything just deleted.
//
// A Transcript is safe to share between goroutines. Close it when the session
// ends; an unclosed one loses whatever its buffer held.
type Transcript struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer

	sidecarFile *os.File
	sidecar     *gzip.Writer

	dir       string
	sessionID string
	bodyCap   int
	now       func() time.Time
}

// TranscriptOptions configures OpenTranscript. Every field has a working
// zero value.
type TranscriptOptions struct {
	// Dir is where session files are written. Empty means
	// ~/.nacelle/sessions. The directory is created with 0700 and each
	// file with 0600: a transcript is closer to a private notebook than a
	// log, and sessions must stay out of any synced path.
	Dir string

	// BodyCap overrides DefaultToolBodyCap. Useful in tests; rarely
	// anywhere else.
	BodyCap int
}

// OpenTranscript starts a new session file in opts.Dir (default
// ~/.nacelle/sessions), compressing any session left uncompressed for longer
// than fourteen days on the way in.
func OpenTranscript(opts TranscriptOptions) (*Transcript, error) {
	file, dir, id, err := openSessionFile(opts.Dir)
	if err != nil {
		return nil, err
	}
	cap := opts.BodyCap
	if cap <= 0 {
		cap = DefaultToolBodyCap
	}
	t := &Transcript{
		file:      file,
		writer:    bufio.NewWriter(file),
		dir:       dir,
		sessionID: id,
		bodyCap:   cap,
		now:       time.Now,
	}
	t.writeLine(line{V: TranscriptSchemaVersion, TS: t.stamp(), Kind: "session"})
	return t, nil
}

// SessionID names this transcript's session, and with it the file
// <SessionID>.jsonl in the transcript directory.
func (t *Transcript) SessionID() string { return t.sessionID }

// Record writes one event. Write errors are swallowed on purpose: a
// transcript that can fail a run is a transcript the caller must handle, and
// recording is bookkeeping, not the job. Record never blocks on anything but
// the file.
func (t *Transcript) Record(event Event) {
	rec := line{V: TranscriptSchemaVersion, TS: t.stamp(), Kind: event.Kind}
	switch event.Kind {
	case KindText, KindThinking:
		if looksSecret(event.Text) {
			rec.Dropped = append(rec.Dropped, "text")
		} else {
			rec.Text = event.Text
		}
	case KindToolCall, KindToolResult:
		rec.Tool = t.toolLine(event)
	}
	if event.Kind == KindTurn || event.Kind == KindDone {
		u := newUsageLine(event.Usage)
		rec.Usage = &u
		rec.Stop = event.Stop
	}
	t.writeLine(rec)
}

// RecordTrim appends a metadata-only trim event: how many messages a context
// trim dropped and nothing else. The content itself was already recorded
// turn by turn, if it was recorded at all; repeating it here would make the
// trim a no-op and the file twice the size.
func (t *Transcript) RecordTrim(dropped int) {
	t.writeLine(line{V: TranscriptSchemaVersion, TS: t.stamp(), Kind: "trim", Trimmed: &dropped})
}

// Close flushes and closes the session file and any open sidecar.
func (t *Transcript) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var err error
	if t.writer != nil {
		err = t.writer.Flush()
		t.writer = nil
	}
	if t.file != nil {
		if cerr := t.file.Close(); err == nil {
			err = cerr
		}
		t.file = nil
	}
	if t.sidecar != nil {
		if cerr := t.sidecar.Close(); err == nil {
			err = cerr
		}
		t.sidecar = nil
	}
	if t.sidecarFile != nil {
		if cerr := t.sidecarFile.Close(); err == nil {
			err = cerr
		}
		t.sidecarFile = nil
	}
	return err
}

// stamp is this transcript's clock, in UTC wire format.
func (t *Transcript) stamp() string { return t.now().UTC().Format(time.RFC3339Nano) }

// writeLine serialises one record under the lock and forgets write errors,
// per Record's contract.
func (t *Transcript) writeLine(rec line) {
	data, err := json.Marshal(rec)
	if err != nil || t.writer == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emit(data)
}

// emit appends one marshalled line, dropping whatever error the writer
// reports — the contract lives at writeLine.
func (t *Transcript) emit(data []byte) {
	w := t.writer
	defer func() {
		_, _ = w.Write(data)
		_ = w.WriteByte('\n')
	}()
}

// sidecarWrite appends one uncapped tool body to the gzipped sidecar,
// opening it on first use. Errors are swallowed like every other write here,
// and the sidecar dies with the session: once the session file is closed
// there is nothing left to attach a body to.
func (t *Transcript) sidecarWrite(rec sidecarLine) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.writer == nil || t.file == nil {
		return
	}
	if t.sidecar == nil {
		f, err := os.OpenFile(filepath.Join(t.dir, t.sessionID+".jsonl.gz"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		t.sidecarFile = f
		t.sidecar = gzip.NewWriter(f)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	w := t.sidecar
	defer func() {
		_, _ = w.Write(data)
		_, _ = w.Write([]byte{'\n'})
	}()
}

// openSessionFile creates the transcript directory, rotates anything old out
// of the way, and claims a fresh session file in it.
func openSessionFile(dir string) (*os.File, string, string, error) {
	resolved, err := transcriptDir(dir)
	if err != nil {
		return nil, "", "", err
	}
	if err := os.MkdirAll(resolved, 0o700); err != nil {
		return nil, "", "", fmt.Errorf("nacelle: transcript dir: %w", err)
	}
	if err := gzipOldSessions(resolved, time.Now(), gzipAfter); err != nil {
		return nil, "", "", err
	}
	id, err := newSessionID(time.Now())
	if err != nil {
		return nil, "", "", err
	}
	path := filepath.Join(resolved, id+".jsonl")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", "", fmt.Errorf("nacelle: transcript: %w", err)
	}
	return file, resolved, id, nil
}

// transcriptDir resolves the transcript directory: explicit, or
// ~/.nacelle/sessions when empty.
func transcriptDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("nacelle: transcript dir: %w", err)
	}
	return filepath.Join(home, ".nacelle", "sessions"), nil
}
