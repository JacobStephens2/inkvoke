package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"net"
	"os"
	"strings"
	"syscall"
)

// Exit codes are part of the machine-callable interface (docs/specs/agent-interface.md).
// Callers should branch on these rather than scraping stderr.
const (
	exitOK            = 0 // success, output written (or help/version printed)
	exitUsage         = 1 // bad flag, missing prompt, wrong arity
	exitAuth          = 2 // missing or rejected API key
	exitAPIPermanent  = 3 // API error, non-retryable (policy, unsupported size, …)
	exitAPIRetryable  = 4 // API error, retryable (rate limit, 5xx, timeout)
	exitIO            = 5 // local I/O error reading inputs or writing output
)

// classed is the structured form of a failure for both exit codes and --json.
type classed struct {
	Exit      int
	Code      string // stable machine code for the error object
	Message   string
	Retryable bool
}

func classedOf(err error) classed {
	if err == nil {
		return classed{Exit: exitOK, Code: "ok", Message: ""}
	}
	if errors.Is(err, flag.ErrHelp) {
		return classed{Exit: exitOK, Code: "ok", Message: ""}
	}

	// Explicit usage / auth / io wrappers take priority.
	var ue *usageError
	if errors.As(err, &ue) {
		return classed{Exit: exitUsage, Code: "usage", Message: ue.Error(), Retryable: false}
	}
	var ae *authError
	if errors.As(err, &ae) {
		return classed{Exit: exitAuth, Code: "auth", Message: ae.Error(), Retryable: false}
	}
	var ie *ioError
	if errors.As(err, &ie) {
		return classed{Exit: exitIO, Code: "io", Message: ie.Error(), Retryable: false}
	}

	var api *APIError
	if errors.As(err, &api) {
		if api.StatusCode == 401 || api.StatusCode == 403 {
			return classed{
				Exit: exitAuth, Code: "auth", Message: api.Error(), Retryable: false,
			}
		}
		if api.Retryable() {
			return classed{
				Exit: exitAPIRetryable, Code: "api_retryable", Message: api.Error(), Retryable: true,
			}
		}
		return classed{
			Exit: exitAPIPermanent, Code: "api_permanent", Message: api.Error(), Retryable: false,
		}
	}

	// Context / network timeouts and cancellations: retryable for agents.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return classed{Exit: exitAPIRetryable, Code: "timeout", Message: err.Error(), Retryable: true}
	}
	if errors.Is(err, context.Canceled) {
		return classed{Exit: exitAPIRetryable, Code: "canceled", Message: err.Error(), Retryable: true}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return classed{Exit: exitAPIRetryable, Code: "timeout", Message: err.Error(), Retryable: true}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return classed{Exit: exitAPIRetryable, Code: "network", Message: err.Error(), Retryable: true}
	}

	// Filesystem errors that weren't wrapped.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) ||
		errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return classed{Exit: exitIO, Code: "io", Message: err.Error(), Retryable: false}
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return classed{Exit: exitIO, Code: "io", Message: err.Error(), Retryable: false}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return classed{Exit: exitIO, Code: "io", Message: err.Error(), Retryable: false}
	}

	// Fallback: permanent API-ish failure so agents do not infinite-retry unknown errors.
	msg := err.Error()
	if strings.Contains(msg, "missing API key") || strings.Contains(msg, "API key file") {
		return classed{Exit: exitAuth, Code: "auth", Message: msg, Retryable: false}
	}
	return classed{Exit: exitAPIPermanent, Code: "error", Message: msg, Retryable: false}
}

// usageError is a bad invocation (flags, arity, validation). Exit 1.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: sprintf(format, args...)}
}

// authError is a missing or rejected API key. Exit 2.
type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

func authf(format string, args ...any) error {
	return &authError{msg: sprintf(format, args...)}
}

// ioError is a local filesystem failure. Exit 5.
type ioError struct{ msg string }

func (e *ioError) Error() string { return e.msg }

func iof(format string, args ...any) error {
	return &ioError{msg: sprintf(format, args...)}
}

// sprintf avoids importing fmt in every call site through a tiny indirection in report.go;
// defined here for error constructors.
func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return errSprintf(format, args...)
}
