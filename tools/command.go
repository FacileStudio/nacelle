package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/FacileStudio/nacelle"
)

// grace is what a command gets between being asked to stop and being made to.
// It is also how long os/exec waits for a pipe an orphaned grandchild is still
// holding before it closes the pipe itself.
const grace = 2 * time.Second

type commandInput struct {
	Command string `json:"command" jsonschema:"required,description=The shell command to run, from the working directory"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"description=Seconds to allow before the command is killed. Omit for the default. A value above the configured ceiling is clamped to it"`
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
			return s.run(ctx, in.Command, bounded(in.Timeout, s.commandTimeout))
		})
}

// bounded is the timeout one call actually gets.
//
// A model may ask for less than the operator configured and never for more.
// Without the ceiling, a model writing `timeout: 86400` has raised a bound it
// does not own and the operator's setting is advisory. Anything absent, zero,
// negative or above the ceiling falls back to the ceiling rather than being
// refused, because a bad guess at a timeout should cost the model a shorter
// command, not a failed tool call.
//
// The comparison is made in seconds on purpose. Converting first overflows:
// time.Duration(1<<40) * time.Second wraps int64 into a negative duration,
// which expires the moment it is set and turns a silly number into an
// unrunnable tool.
func bounded(seconds int, ceiling time.Duration) time.Duration {
	if seconds <= 0 || time.Duration(seconds) > ceiling/time.Second {
		return ceiling
	}
	return time.Duration(seconds) * time.Second
}

// run executes one command, bounded in time, in output, and in how long it can
// hold the goroutine that called it.
//
// The command is put in its own process group so the whole tree can be
// signalled. Killing the shell alone leaves its children running, and a test
// runner or a dev server orphaned by a timeout goes on holding the port the
// next command needs.
//
// WaitDelay is the difference between a tool call that returns and one that
// does not, and it is not obvious. Collecting output into a bytes.Buffer makes
// os/exec insert a pipe and a copying goroutine, and Wait blocks until every
// holder of the write end has closed it — including a grandchild that called
// setsid and so was never in the group the kill reached. Without a delay,
// `./server &` hangs this call for as long as the server lives. With one,
// os/exec closes the pipes itself and Wait returns.
//
// Cancel is wired to the same signal so that os/exec owns the deadline too.
// Its timer, not ours, is what closes the pipes when the signal does not land
// at all — EPERM on a setuid child, or a process that simply ignores SIGTERM —
// which is the path that used to wait forever with nothing left to wait for.
//
// A cancelled context is not a timeout and is not reported as one. The caller
// asking for the command to stop is the caller's own business, so it comes
// back as an error rather than as output the model would read as a run that
// finished.
func (s *Set) run(ctx context.Context, command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = s.dir
	cmd.Env = s.commandEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = grace
	cmd.Cancel = func() error { return signalGroup(cmd, syscall.SIGTERM) }

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Start(); err != nil {
		return "", err
	}

	reaped := make(chan struct{})
	go escalate(cmd, ctx.Done(), reaped)
	waitErr := cmd.Wait()
	close(reaped)

	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return "", ctx.Err()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return report(out.String(), errTimedOut{after: timeout}, s.maxOutput), nil
	default:
		return report(out.String(), waitErr, s.maxOutput), nil
	}
}

// killGroup sends one signal to the command and everything it started,
// reporting whether there was still a group there to receive it.
//
// The negative pid is the whole point: kill(-pgid) reaches the process group,
// where kill(pid) reaches only the shell that spawned the work.
//
// It answers with a bool rather than an error because the answer is advisory
// at both call sites and only one of them can act on it. A group that is
// already gone is the ordinary ending, not a failure worth propagating.
func killGroup(cmd *exec.Cmd, signal syscall.Signal) bool {
	if cmd.Process == nil {
		return false
	}
	return syscall.Kill(-cmd.Process.Pid, signal) == nil
}

// signalGroup is killGroup in the shape os/exec's Cancel field asks for.
//
// A signal that cannot be sent comes back as os.ErrProcessDone, which is what
// os/exec reads as "there was nothing left to interrupt" and so declines to
// dress up as a failure of the command. It changes nothing about the bound:
// os/exec starts its WaitDelay timer either way, so a group that refuses the
// signal is still released on a clock rather than on optimism.
func signalGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if killGroup(cmd, signal) {
		return nil
	}
	return os.ErrProcessDone
}

// escalate is the SIGKILL that a command ignoring SIGTERM does not get to
// refuse.
//
// os/exec escalates on its own once WaitDelay is up, but only to the command
// itself, and the children are the entire reason this tool bothers with a
// process group: a killed shell whose test runner survives is the bug the
// group was introduced to fix.
//
// reaped closes when Wait returns, which is also the moment the pid becomes
// free to reuse, so the signal is only ever sent while the group is still
// known to be there.
func escalate(cmd *exec.Cmd, expired <-chan struct{}, reaped <-chan struct{}) {
	select {
	case <-reaped:
		return
	case <-expired:
	}

	select {
	case <-reaped:
	case <-time.After(grace):
		killGroup(cmd, syscall.SIGKILL)
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
