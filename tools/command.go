package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/FacileStudio/nacelle"
)

type commandInput struct {
	Command string `json:"command" jsonschema:"required,description=The shell command to run, from the working directory"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"description=Seconds to allow before the command is killed. Omit for the default"`
}

// commandTool builds the shell runner.
//
// The description says what the tool does and nothing about what it must not
// be used for. A model reads that as guidance and a determined prompt reads it
// as a list; neither is a control. The control is that this tool is not
// mounted unless the caller asked for it, and that whoever asked knows a
// command runs with the process's own privileges.
func (s *Set) commandTool() (nacelle.Tool, error) {
	return nacelle.NewTool("run_command",
		"Run a shell command in the working directory and return its output. Use it for builds, tests, version control and anything else the other tools do not cover. Output is truncated if it is very long, so prefer commands that answer a question over commands that print everything.",
		func(ctx context.Context, in commandInput) (string, error) {
			if strings.TrimSpace(in.Command) == "" {
				return "", fmt.Errorf("no command given")
			}
			timeout := s.commandTimeout
			if in.Timeout > 0 {
				timeout = time.Duration(in.Timeout) * time.Second
			}
			return s.run(ctx, in.Command, timeout)
		})
}

// run executes one command, bounded in time and output.
//
// The command is put in its own process group so the whole tree can be
// signalled. Killing the shell alone leaves its children running, and a test
// runner or a dev server orphaned by a timeout goes on holding the port the
// next command needs.
//
// Reaping is announced by closing a channel rather than by sending the wait
// error down it, because two goroutines need to hear it: this one, and the
// escalation in killGroup. A close is a broadcast, a send is consumed once, and
// the version that sent it invited exactly the kind of who-drains-what bug that
// deadlocks a timeout path nobody exercises. The error rides alongside in a
// plain variable, published by the same close and read only after it.
func (s *Set) run(ctx context.Context, command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = s.dir
	cmd.Env = s.commandEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var waitErr error
	done := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return report(out.String(), waitErr, s.maxOutput), nil
	case <-ctx.Done():
		killGroup(cmd, done)
		<-done
		return report(out.String(), errTimedOut{after: timeout}, s.maxOutput), nil
	}
}

// killGroup signals the command and everything it started.
//
// The negative pid is the whole point: kill(-pgid) reaches the process group,
// where kill(pid) reaches only the shell that spawned the work. A signal that
// fails to send means the group is already gone, so there is nothing left to
// escalate to.
//
// Whether the group went quietly on SIGTERM is read from the caller's done
// channel, closed when cmd.Wait returns. The obvious-looking alternative is to
// poll cmd.ProcessState, and it is a data race: Wait writes that field from
// another goroutine and nothing publishes it to this one. Two seconds is the
// grace a well-behaved process gets to flush and exit before SIGKILL, which it
// does not get to refuse.
func killGroup(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	pgid := -cmd.Process.Pid
	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil {
		return
	}

	select {
	case <-time.After(2 * time.Second):
		if err := syscall.Kill(pgid, syscall.SIGKILL); err != nil {
			return
		}
	case <-done:
	}
}

// errTimedOut is a command killed for running too long.
type errTimedOut struct{ after time.Duration }

func (e errTimedOut) Error() string { return fmt.Sprintf("timed out after %s", e.after) }

// report renders the outcome for the model.
//
// A failed command returns its output and its status rather than an error,
// because a non-zero exit is usually the answer: a failing test suite is
// information, not a broken tool. The distinction the model needs is what
// happened, and that is in the text.
func report(output string, err error, limit int) string {
	body := truncate(strings.TrimRight(output, "\n"), limit)
	if body == "" {
		body = "(no output)"
	}

	switch {
	case err == nil:
		return body
	case errors.As(err, &errTimedOut{}):
		return body + "\n\n[" + err.Error() + "; the command and its children were killed]"
	default:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Sprintf("%s\n\n[exit status %d]", body, exit.ExitCode())
		}
		return fmt.Sprintf("%s\n\n[%s]", body, err)
	}
}
