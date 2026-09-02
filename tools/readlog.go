package tools

import "sync"

// readLog remembers which files have been read.
//
// Edit consults it. An agent that rewrites a file it has not read is working
// from what it assumes the file says, and the assumption is usually a
// plausible reconstruction rather than the real thing.
type readLog struct {
	mu    sync.Mutex
	files map[string]bool
}

func newReadLog() *readLog { return &readLog{files: map[string]bool{}} }

func (l *readLog) record(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.files[name] = true
}

func (l *readLog) seen(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.files[name]
}
