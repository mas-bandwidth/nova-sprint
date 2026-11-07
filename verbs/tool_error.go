package verbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// ToolError is a tool or child that failed: its redacted command line, its
// exit code and the first lines of its stdout and stderr (edge.CommandError,
// the one shape every layer reports a failed child in; audit row 28).
type ToolError = edge.CommandError

// Failure is err from running name as a ToolError, with the stderr it
// wrote; nil when err is nil.
func Failure(name string, err error, stderr []byte) error {
	if err == nil {
		return nil
	}
	return edge.Failure([]string{name}, nil, exitCode(err), "", string(stderr), runErr(err))
}

// exitCode is the child's exit code, or -1 when it never exited on its own.
func exitCode(err error) int {
	var x *exec.ExitError
	if errors.As(err, &x) {
		return x.ExitCode()
	}
	return -1
}

// runErr is err when it says more than the exit code (a missing binary, a
// cancelled context), or nil for a plain non-zero exit.
func runErr(err error) error {
	var x *exec.ExitError
	if errors.As(err, &x) {
		return nil
	}
	return err
}

// ToolDetail is " (<the cause>)" for any error, redacted and bounded, or ""
// for nil: a wrapped failure keeps the real error in its one refusal line.
func ToolDetail(err error) string {
	if err == nil {
		return ""
	}
	return " (" + edge.RedactN(err.Error(), edge.CommandMax) + ")"
}

const stderrLineMax = edge.StderrMax

// StderrLine is every non-empty line of a tool's stderr but nova-secrets'
// banner, joined by " | " on one line, bounded (the first lines kept), with
// anything that may be a secret replaced (edge.StderrLines).
func StderrLine(stderr []byte) string { return edge.StderrLines(string(stderr)) }

// headTail keeps the first and last bytes a child writes, bounded: enough
// for the first lines a failure carries.
type headTail struct{ buf []byte }

func (s *headTail) Write(p []byte) (int, error) {
	if room := 8192 - len(s.buf); room > 0 {
		s.buf = append(s.buf, p[:min(len(p), room)]...)
	}
	return len(p), nil
}

// runTool runs cmd with stdout into out (and a bounded copy for the failure)
// and stderr kept, returning an edge.CommandError when it fails.
func runTool(cmd *exec.Cmd, out io.Writer) error {
	var stdout, stderr headTail
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout = io.MultiWriter(out, &stdout)
	cmd.Stderr = &stderr
	if cmd.WaitDelay == 0 {
		// a descendant that keeps the child's stdout or stderr open (a
		// daemon it started) cannot hang the caller: Wait gives up after
		// this, and the failure says so (exec.ErrWaitDelay)
		cmd.WaitDelay = 10 * time.Second
	}
	err := cmd.Run()
	if err == nil {
		return nil
	}
	return edge.Failure(cmd.Args, nil, exitCode(err), string(stdout.buf), string(stderr.buf), runErr(err))
}

// RunQuiet runs name with stdout discarded; a failure carries the command,
// its exit code and its output's first lines.
func RunQuiet(ctx context.Context, name string, args ...string) error {
	return runTool(exec.CommandContext(ctx, name, args...), nil)
}

// OutputWith runs cmd for its stdout (into out); a failure carries the
// command, its exit code and its output's first lines.
func OutputWith(cmd *exec.Cmd, out io.Writer) error { return runTool(cmd, out) }

// Output runs name for its stdout; a failure carries the command, its exit
// code and its output's first lines.
func Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	var out bytes.Buffer
	err := runTool(exec.CommandContext(ctx, name, args...), &out)
	return out.Bytes(), err
}

// Redact is the edge's redaction on any text, kept on one line.
func Redact(text string) string { return edge.Redact(text) }

// firstBytes is the first 200 bytes a parser saw, redacted.
func firstBytes(out []byte) string { return edge.FirstBytes(out) }

// ParseFailure names where a parse failed and what it saw: the byte offset
// and the first 200 bytes (audit row 28: a parse failure never drops its
// cause).
func ParseFailure(err error, out []byte) string { return edge.ParseFailure(err, out) }

// cause is what with err's text, redacted and bounded, err kept for
// errors.Is (edge.Cause).
func cause(what string, err error) error { return edge.Cause(what, err) }

// ChildFailed is an internal child's failure: its error, redacted and
// bounded, on stderr (the parent's edge.CommandError carries it), and code.
// A child that exits non-zero always says why. A refusal (an
// *OperationError or a *Fail anywhere in err) is written as two lines,
// "nova-sprint: refused: <problem>" then "nova-sprint: fix: <fix>", which
// AsRefusal in the parent reads back, so its Problem and Fix reach the
// terminal as they were.
func ChildFailed(err error, code int) int {
	writeChildFailure(os.Stderr, err)
	return code
}

const (
	childRefusedPrefix = "nova-sprint: refused: "
	childFixPrefix     = "nova-sprint: fix: "
)

func writeChildFailure(w io.Writer, err error) {
	if err == nil {
		err = errors.New("failed with no error to report (a bug: take it to the project's own tracker)")
	}
	var op *OperationError
	var f *Fail
	switch {
	case errors.As(err, &op):
		fmt.Fprintln(w, childRefusedPrefix+edge.RedactN(op.Problem, edge.StderrMax/2))
		fmt.Fprintln(w, childFixPrefix+edge.RedactN(op.Fix, edge.FailureMax))
	case errors.As(err, &f):
		fmt.Fprintln(w, childRefusedPrefix+edge.RedactN(f.What, edge.StderrMax/2))
		fmt.Fprintln(w, childFixPrefix+edge.RedactN(f.Fix, edge.FailureMax))
	default:
		fmt.Fprintln(w, "nova-sprint: "+edge.RedactN(err.Error(), edge.StderrMax))
	}
}

// RefusalOr is the refusal err carries (AsRefusal), passed through as it
// is, or otherwise: the one form for an err branch that would build its own
// refusal from err (silentlint S2).
func RefusalOr(err, otherwise error) error {
	if r := AsRefusal(err); r != nil {
		return r
	}
	return otherwise
}

// errText is err's whole text for a refusal, redacted on one line and bounded
// at edge.CommandMax (a failed child's whole stderr fits).
func errText(err error) string { return edge.RedactN(err.Error(), edge.CommandMax) }

// AsRefusal is the refusal err carries, to be passed through as it is and
// never re-wrapped: a *Fail or *OperationError anywhere in err's chain, an
// edge's *edge.Refusal as an *OperationError (Exit 1), or
// the refusal an internal child wrote on stderr (ChildFailed) inside an
// edge.CommandError, rebuilt as an *OperationError (Exit 2 when the child
// exited 2, else 1). Nil when err carries no refusal.
func AsRefusal(err error) error {
	var f *Fail
	var op *OperationError
	var er *edge.Refusal
	var cmd *edge.CommandError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &f):
		return f
	case errors.As(err, &op):
		return op
	case errors.As(err, &er):
		return &OperationError{Problem: er.Problem, Fix: er.Fix, Exit: exitRefused}
	case errors.As(err, &cmd):
		at := strings.LastIndex(cmd.Stderr, childRefusedPrefix)
		if at < 0 {
			return nil
		}
		problem, fix, ok := strings.Cut(cmd.Stderr[at+len(childRefusedPrefix):], " | "+childFixPrefix)
		if !ok {
			return nil
		}
		fix, _, _ = strings.Cut(fix, " | ")
		exit := exitRefused
		if cmd.Exit == exitFailed {
			exit = exitFailed
		}
		return &OperationError{Problem: problem, Fix: fix, Exit: exit}
	}
	return nil
}

// missingNames is the names (never the values) of the empty ones, sorted:
// "NAME, NAME not set".
func missingNames(values map[string]string) string {
	var names []string
	for name, v := range values {
		if v == "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ") + " not set"
}

// badNumber says why s is not the number wanted: the parse's byte and the
// first bytes when it did not parse, else what was given.
func badNumber(s string, err error) string {
	if err != nil {
		return edge.ParseFailure(err, []byte(s))
	}
	return fmt.Sprintf("got %q", Redact(s))
}
