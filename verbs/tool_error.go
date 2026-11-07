package verbs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"

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
	return " (" + edge.RedactN(err.Error(), edge.FailureMax) + ")"
}

const stderrLineMax = edge.RedactMax

// StderrLine is the last non-empty line of a tool's stderr, on one line,
// bounded, with anything that may be a secret replaced (edge.RedactN).
func StderrLine(stderr []byte) string { return edge.LastLine(string(stderr)) }

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
