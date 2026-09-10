package tools

import (
	"bytes"
	"sync"
)

// lineEmitter hands each completed output line to its callback, buffering a
// trailing partial line until the next newline. It is what lets a long-running
// command's output stream to a consumer instead of only landing when the
// command finishes. Its Write runs from os/exec's copying goroutine, so
// emission is serialised under a mutex.
type lineEmitter struct {
	mu     sync.Mutex
	buffer []byte
	emit   func(string)
}

func (w *lineEmitter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, p...)
	for {
		i := bytes.IndexByte(w.buffer, '\n')
		if i < 0 {
			break
		}
		w.emit(string(bytes.TrimSuffix(w.buffer[:i+1], []byte("\n"))))
		w.buffer = w.buffer[i+1:]
	}
	return len(p), nil
}
