package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/FacileStudio/nacelle"
)

// execHook builds the hook that runs one spec's command and translates its
// exit back into a decision. A leading ~ in the command is expanded once at
// build time: sh -c does not expand a tilde mid-string, and every config
// file in this ecosystem writes paths that start with one.
func execHook(spec HookSpec) nacelle.Hook {
	command := expandTilde(spec.Run)
	return func(ctx context.Context, ev nacelle.HookEvent) nacelle.HookResult {
		if !spec.matches(ev.Tool) {
			return nacelle.HookResult{}
		}
		out, errOut, runErr := runCommand(ctx, command, hookPayload{
			Event: string(ev.Point), Tool: ev.Tool,
			Input: ev.Input, Result: ev.Result, Retry: ev.Retry,
		})
		return interpret(command, ev, runErr, out, errOut)
	}
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(command string) string {
	if command != "~" && !strings.HasPrefix(command, "~/") {
		return command
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return command
	}
	return filepath.Join(home, strings.TrimPrefix(command, "~"))
}

// runCommand executes one spec through sh with the event JSON on stdin.
func runCommand(ctx context.Context, command string, payload hookPayload) (out []byte, errOut []byte, runErr error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nacelle hooks: encoding event for %q: %v\n", command, err)
		return nil, nil, nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(encoded)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), runErr
}

// interpret turns one finished hook process into a decision:
//
//	exit 0        allow; stdout becomes injected context
//	exit 2        deny; stderr is the reason the model reads
//	other failure deny; stderr goes to this process's stderr instead,
//	              because a crash is a bug report for the human, not
//	              instruction-shaped text for the model
//
// Injection only lands on AfterToolCall — there is no result to amend before
// the call — and is skipped on a failed tool: prose after an error reads to
// the model as if the error were handled when nothing was.
func interpret(command string, ev nacelle.HookEvent, runErr error, out, errOut []byte) nacelle.HookResult {
	switch {
	case runErr == nil:
		if ev.Point == nacelle.AfterToolCall && len(out) > 0 && ev.Err == nil {
			return nacelle.HookResult{Inject: strings.TrimRight(string(out), "\n")}
		}
		return nacelle.HookResult{}
	case exitCode(runErr) == 2:
		reason := strings.TrimSpace(string(errOut))
		if reason == "" {
			reason = "denied by hook without a reason"
		}
		return nacelle.HookResult{Deny: reason}
	default:
		fmt.Fprintf(os.Stderr, "nacelle hooks: %q failed: %v: %s\n",
			command, runErr, strings.TrimSpace(string(errOut)))
		return nacelle.HookResult{Deny: fmt.Sprintf("hook watching %q failed", ev.Tool)}
	}
}

// exitCode recovers a command's status without importing syscall for it.
func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}
