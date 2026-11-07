package verbs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ToolError is a tool or child that failed, with the last line of its
// stderr: the tool's own words, which a refusal carries so the real error
// reaches the human. The line is redacted (StderrLine) before it is kept.
type ToolError struct {
	Name, Line string
	Err        error
}

func (t *ToolError) Error() string {
	if t.Line != "" {
		return t.said()
	}
	if t.Err != nil {
		return t.Name + ": " + t.Err.Error()
	}
	return t.Name + " failed"
}
func (t *ToolError) Unwrap() error { return t.Err }

// said is the line, prefixed with the tool's name unless it names itself.
func (t *ToolError) said() string {
	if strings.HasPrefix(t.Line, t.Name+":") || strings.HasPrefix(t.Line, t.Name+" ") {
		return t.Line
	}
	return t.Name + ": " + t.Line
}

// Failure is err as a ToolError naming name, its line taken from stderr.
func Failure(name string, err error, stderr []byte) error {
	if err == nil {
		return nil
	}
	return &ToolError{Name: name, Line: StderrLine(stderr), Err: err}
}

// ToolDetail is " (<the tool's last stderr line>)" when err carries one, or
// "": a wrapped failure keeps the real error in its one refusal line.
func ToolDetail(err error) string {
	var t *ToolError
	if errors.As(err, &t) && t.Line != "" {
		return " (" + t.said() + ")"
	}
	var x *exec.ExitError
	if errors.As(err, &x) {
		if line := StderrLine(x.Stderr); line != "" {
			return " (" + line + ")"
		}
	}
	return ""
}

const stderrLineMax = 200

var (
	// A token that looks like a key: an age secret key, a known token
	// prefix, or a long run of key characters holding a digit.
	keyLike  = regexp.MustCompile(`(?i)AGE-SECRET-KEY-[0-9A-Z]+|\b(?:sk|pk|rk|ghp|gho|ghs|ghu|github_pat|xox[abpr])[-_][A-Za-z0-9_\-]+|[A-Za-z0-9+/_\-=.]{20,}`)
	assigned = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)=\S+`)
	userinfo = regexp.MustCompile(`://[^/@\s]*@`)
	absPath  = regexp.MustCompile(`(?:~|\B)/[^\s:'"]+`)
)

// StderrLine is the last non-empty line of a tool's stderr, on one line,
// bounded, with anything that may be a secret replaced: NAME=value
// assignments, URL credentials, key-like tokens; and absolute paths cut to
// their base name (a home directory names a person).
func StderrLine(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	line := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if line = strings.TrimSpace(lines[i]); line != "" {
			break
		}
	}
	if line == "" {
		return ""
	}
	line = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, line)
	line = userinfo.ReplaceAllString(line, "://<redacted>@")
	line = assigned.ReplaceAllString(line, "$1=<redacted>")
	line = absPath.ReplaceAllStringFunc(line, func(p string) string { return ".../" + filepath.Base(p) })
	line = keyLike.ReplaceAllStringFunc(line, func(tok string) string {
		if strings.HasPrefix(strings.ToUpper(tok), "AGE-SECRET-KEY-") || strings.ContainsAny(tok, "0123456789") {
			return "<redacted>"
		}
		return tok
	})
	if len(line) > stderrLineMax {
		line = line[:stderrLineMax] + "..."
	}
	return line
}

// stderrTail keeps the last bytes a child writes to stderr, bounded.
type stderrTail struct{ buf []byte }

func (s *stderrTail) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	if len(s.buf) > 4096 {
		s.buf = append([]byte(nil), s.buf[len(s.buf)-4096:]...)
	}
	return len(p), nil
}

// RunQuiet runs name with stdout discarded and stderr kept for the
// failure's line (a ToolError).
func RunQuiet(ctx context.Context, name string, args ...string) error {
	var tail stderrTail
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = &tail
	return Failure(name, cmd.Run(), tail.buf)
}

// OutputWith runs cmd for its stdout (into out), keeping stderr's last line
// for a failure.
func OutputWith(cmd *exec.Cmd, out io.Writer) error {
	var tail stderrTail
	cmd.Stdout = out
	cmd.Stderr = &tail
	return Failure(filepath.Base(cmd.Path), cmd.Run(), bytes.TrimSpace(tail.buf))
}

// Redact is StderrLine's redaction on any text, kept on one line.
func Redact(text string) string { return StderrLine([]byte(strings.ReplaceAll(text, "\n", " "))) }

// firstBytes is the first 200 bytes a parser saw, redacted.
func firstBytes(out []byte) string {
	if len(out) > 200 {
		out = out[:200]
	}
	return Redact(string(out))
}

// ParseFailure names where a JSON parse failed and what it saw: the byte
// offset and the first 200 bytes (audit row 28: a parse failure never
// drops its cause).
func ParseFailure(err error, out []byte) string {
	var off int64 = -1
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off = se.Offset
	case errors.As(err, &te):
		off = te.Offset
	}
	return fmt.Sprintf("parse failed at byte %d: %v; first bytes %q", off, err, firstBytes(out))
}
