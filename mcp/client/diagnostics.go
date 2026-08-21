package client

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// stderrLimit caps what one server's diagnostics cost in memory.
//
// The first bytes are kept rather than the last, because this exists to
// explain a server that failed to start and a program that is about to die
// says why on its way out, not after. Eight kilobytes holds a stack trace
// from any of the runtimes MCP servers are written in, and a server that
// chatters past it is bounded at that for the life of the process.
const stderrLimit = 8 * 1024

// diagnostics keeps the beginning of what a server wrote to its standard
// error, so that a failure to start can say why.
//
// Without it a misconfigured server is unreadable. The subprocess prints
// "FATAL: GITHUB_TOKEN is not set" and exits, the pipe closes, and the only
// thing reaching the operator is the shape of the silence that followed:
// `connection closed: calling "initialize": EOF`. That names the protocol
// step and nothing an operator can act on, while the sentence that would
// have ended the search went to /dev/null. Measured against a server that
// starts but never speaks MCP, that is exactly the error this package used
// to return.
//
// Writes are locked because os/exec copies stderr on a goroutine of its own
// and the error is built on the caller's, and the two are only ordered by
// Wait, which happens somewhere inside the SDK's cleanup.
type diagnostics struct {
	mu      sync.Mutex
	kept    []byte
	dropped bool
}

// Write keeps what fits and counts the rest as dropped. It never fails and
// never blocks: os/exec's copier is waited on by Cmd.Wait, so a writer that
// stalled here would stall the shutdown of the process it is reporting on.
func (d *diagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch room := stderrLimit - len(d.kept); {
	case room >= len(p):
		d.kept = append(d.kept, p...)
	case room > 0:
		d.kept = append(d.kept, p[:room]...)
		d.dropped = true
	default:
		d.dropped = len(p) > 0 || d.dropped
	}
	return len(p), nil
}

// note is the clause to hang off a failure, and the empty string for a server
// that said nothing — a trailing "the server said:" with nothing after it
// would read as output that went missing rather than output that never came.
func (d *diagnostics) note() string {
	d.mu.Lock()
	defer d.mu.Unlock()

	said := strings.TrimSpace(string(d.kept))
	if said == "" {
		return ""
	}
	if d.dropped {
		return fmt.Sprintf("; the server wrote (first %d bytes): %s", stderrLimit, said)
	}
	return fmt.Sprintf("; the server wrote: %s", said)
}

// tee sends a copy to the caller's writer when there is one.
//
// The bounded copy is kept either way. A caller who wired Command.Stderr to
// a log has somewhere to read the whole of it, but the error still has to
// carry the reason on its own: nobody correlating a returned error with a log
// file is having a good afternoon.
func (d *diagnostics) tee(extra io.Writer) io.Writer {
	if extra == nil {
		return d
	}
	return io.MultiWriter(d, extra)
}
