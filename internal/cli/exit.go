// Package cli implements `pt`, the partrack command-line client. It is built
// for non-interactive use: plain, column-aligned text on stdout by default,
// diagnostics on stderr, `--format json` for anything that needs parsing, and
// distinct exit codes so a script can branch without reading the message.
package cli

import "fmt"

// Exit codes. These mirror the dks taxonomy so the two tools can be scripted
// together without remembering which is which.
const (
	ExitOK        = 0
	ExitFailure   = 1
	ExitUsage     = 2
	ExitAuth      = 3
	ExitNotFound  = 4
	ExitAmbiguous = 5
	ExitNetwork   = 6
)

// ExitError carries a process exit code alongside the underlying error.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// exitErr wraps err so Execute reports it with the given code.
func exitErr(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

// exitErrf wraps a formatted message with the given code.
func exitErrf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}
