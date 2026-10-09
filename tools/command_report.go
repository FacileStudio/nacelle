package tools

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

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
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return fmt.Sprintf("%s\n\n[exit status %d]", body, exit.ExitCode())
		}
		return fmt.Sprintf("%s\n\n[%s]", body, err)
	}
}
