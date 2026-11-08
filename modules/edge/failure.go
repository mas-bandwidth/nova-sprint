package edge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The one way an error names a failed child or a failed parse (audit row 28:
// "We should not be swallowing errors... we should be reporting errors").
// A failed child's error carries its command line, its exit code and the
// first lines of its stdout and stderr; a parse failure carries the byte
// where it failed and the first bytes it saw. Everything is redacted on the
// way in: a secret is shown by its name, never its value.

const (
	// RedactMax bounds one redacted fragment (a stderr line, a parse's
	// first bytes).
	RedactMax = 200
	// FailureMax bounds a refusal's own words (a Problem built from a
	// cause) so a refusal stays one readable line.
	FailureMax = 300
	// StderrMax bounds the stderr a failed child's refusal carries: every
	// non-empty line, so the child's own cause (its refusal, its Fix) reaches
	// the terminal; the first lines are kept over the last when it is cut.
	StderrMax = 2000
	// CommandMax bounds a whole CommandError line: the command, the exit,
	// the child's stderr and the first lines of its stdout.
	CommandMax = StderrMax + FailureMax + 100
)

var (
	// A token that looks like a key: an age secret key, a known token
	// prefix, or a long run of key characters holding a digit.
	keyLike  = regexp.MustCompile(`(?i)AGE-SECRET-KEY-[0-9A-Z]+|\b(?:sk|pk|rk|ghp|gho|ghs|ghu|github_pat|xox[abpr])[-_][A-Za-z0-9_\-]+|[A-Za-z0-9+/_\-=.]{20,}`)
	assigned = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)=\S+`)
	userinfo = regexp.MustCompile(`://[^/@\s]*@`)
	pgPass   = regexp.MustCompile(`(?i)\b(password|passwd|pwd)(\s*[:=]\s*)(\S+)`)
	absPath  = regexp.MustCompile(`(^|[\s"'=(\[])(~?/[^\s:'"]+)`)
)

// RedactN is text on one line, bounded to max bytes, with anything that may
// be a secret replaced: NAME=value assignments keep the name, URL
// credentials, password words and key-like tokens go; absolute paths are cut
// to their base name (a home directory names a person).
func RedactN(text string, max int) string {
	line := strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(text))
	line = userinfo.ReplaceAllString(line, "://<redacted>@")
	line = assigned.ReplaceAllStringFunc(line, func(m string) string {
		name, value, _ := strings.Cut(m, "=")
		if strings.HasPrefix(value, "<redacted>") {
			return m // already redacted: redaction is idempotent
		}
		return name + "=<redacted>"
	})
	line = pgPass.ReplaceAllStringFunc(line, func(m string) string {
		sub := pgPass.FindStringSubmatch(m)
		if strings.HasPrefix(sub[3], "<redacted>") {
			return m
		}
		return sub[1] + sub[2] + "<redacted>"
	})
	line = absPath.ReplaceAllStringFunc(line, func(m string) string {
		sub := absPath.FindStringSubmatch(m)
		return sub[1] + ".../" + filepath.Base(sub[2])
	})
	line = keyLike.ReplaceAllStringFunc(line, func(tok string) string {
		if strings.HasPrefix(strings.ToUpper(tok), "AGE-SECRET-KEY-") || strings.ContainsAny(tok, "0123456789") {
			return "<redacted>"
		}
		return tok
	})
	if max > 0 && len(line) > max {
		line = line[:max] + "..."
	}
	return line
}

// Redact is RedactN bounded at RedactMax.
func Redact(text string) string { return RedactN(text, RedactMax) }

// LastLine is the last non-empty line of out, redacted: a tool's own last
// words.
func LastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return Redact(l)
		}
	}
	return ""
}

// secretsBanner is nova-secrets exec's own announcement on stderr (SECRETS
// EXEC STARTING, or OK in older releases): never a cause, so a child's
// refusal leaves it out. Its refusals (SECRETS EXEC FAIL, REFUSED) are kept.
var secretsBanner = regexp.MustCompile(`^!?\s*SECRETS EXEC (STARTING|OK)\b`)

// ansiEscape is a terminal escape sequence (colour, cursor): a harness that
// paints its output ends a line with a bare reset, which is not a cause.
var ansiEscape = regexp.MustCompile("\x1b(?:\\[[0-9;?]*[ -/]*[@-~]|\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|[@-Z\\\\-_])")

// StripANSI is out without its terminal escape sequences.
func StripANSI(out string) string { return ansiEscape.ReplaceAllString(out, "") }

// CauseLine is the line of out that says what went wrong, escapes stripped:
// the first line holding "error", "refused" or "failed" (any case), else
// the last non-empty line; redacted and bounded to max bytes.
func CauseLine(out string, max int) string {
	last := ""
	for _, l := range strings.Split(StripANSI(out), "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		if low := strings.ToLower(l); strings.Contains(low, "error") || strings.Contains(low, "refused") || strings.Contains(low, "failed") {
			return RedactN(l, max)
		}
		last = l
	}
	return RedactN(last, max)
}

// StderrLines is every non-empty line a child wrote on stderr except
// nova-secrets' banner, escapes stripped, each redacted, joined by " | " and
// bounded to StderrMax bytes, the first lines kept over the last: the
// child's whole cause on one line.
func StderrLines(out string) string {
	var keep []string
	for _, l := range strings.Split(StripANSI(out), "\n") {
		if l = strings.TrimSpace(l); l == "" || secretsBanner.MatchString(l) {
			continue
		}
		keep = append(keep, l)
	}
	return RedactN(strings.Join(keep, " | "), StderrMax)
}

// Head is the first n non-empty lines of out joined by " | ", redacted and
// bounded to max bytes; when out has more lines, its last line follows
// " ... " (a tool's error is often its last line).
func Head(out string, n, max int) string {
	var keep []string
	all := 0
	last := ""
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		all++
		last = l
		if len(keep) < n {
			keep = append(keep, l)
		}
	}
	s := strings.Join(keep, " | ")
	if all > n {
		s += " ... " + last
	}
	return RedactN(s, max)
}

// CommandLine is argv redacted, then the names (never the values) of the
// environment entries the caller added.
func CommandLine(argv, env []string) string {
	parts := make([]string, 0, len(argv))
	for i, a := range argv {
		if i == 0 {
			a = filepath.Base(a)
		}
		if strings.ContainsAny(a, " \t") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	line := RedactN(strings.Join(parts, " "), 160)
	if len(env) > 0 {
		names := make([]string, 0, len(env))
		for _, kv := range env {
			name, _, _ := strings.Cut(kv, "=")
			names = append(names, name)
		}
		line += " [env " + RedactN(strings.Join(names, ","), 80) + "]"
	}
	return line
}

// CommandError is a child that failed: its redacted command line, its exit
// code (-1 when it never exited on its own), every line of its stderr but
// nova-secrets' banner (StderrLines) and the first lines of its stdout,
// redacted. Err is the run's own error (a timeout, a
// missing binary), or nil when the child only exited non-zero.
type CommandError struct {
	Command        string
	Exit           int
	Stdout, Stderr string
	Err            error
}

func (e *CommandError) Error() string {
	var b strings.Builder
	b.WriteString(e.Command)
	fmt.Fprintf(&b, ": exit %d", e.Exit)
	if e.Err != nil {
		b.WriteString(" (" + RedactN(e.Err.Error(), 120) + ")")
	}
	switch {
	case e.Stderr == "" && e.Stdout == "":
		b.WriteString("; no output")
	default:
		if e.Stderr != "" {
			fmt.Fprintf(&b, "; stderr %q", e.Stderr)
		}
		if e.Stdout != "" {
			fmt.Fprintf(&b, "; stdout %q", e.Stdout)
		}
	}
	s := b.String()
	if len(s) > CommandMax {
		s = s[:CommandMax] + "..."
	}
	return s
}
func (e *CommandError) Unwrap() error { return e.Err }

// ExitCode is the child's exit code (os/exec's ExitError shape, so a caller
// that asks errors.As for an ExitCode finds it here).
func (e *CommandError) ExitCode() int { return e.Exit }

// Failed is cmd's failure as a CommandError, or nil when it ran and exited 0.
func Failed(cmd Cmd, res Result, err error) error {
	if err == nil && res.OK() {
		return nil
	}
	return Failure(cmd.Argv, cmd.Env, res.ExitCode, res.Stdout, res.Stderr, err)
}

// Failure is a CommandError from the parts a caller holds (os/exec callers
// that do not run through Exec).
func Failure(argv, env []string, exit int, stdout, stderr string, err error) *CommandError {
	return &CommandError{
		Command: CommandLine(argv, env),
		Exit:    exit,
		Stdout:  Head(stdout, 2, FailureMax),
		Stderr:  StderrLines(stderr),
		Err:     err,
	}
}

// FirstBytes is the first 200 bytes a parser saw, redacted.
func FirstBytes(out []byte) string {
	if len(out) > RedactMax {
		out = out[:RedactMax]
	}
	return RedactN(string(out), RedactMax)
}

// ParseFailure names where a parse failed and what it saw: the byte offset
// (-1 when the parser gave none) and the first 200 bytes, redacted. JSON's
// syntax and type errors carry their offset; an unexpected end is the end;
// a number's is its first byte that is not part of a number.
func ParseFailure(err error, out []byte) string {
	off := int64(-1)
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	var ne *strconv.NumError
	switch {
	case err == nil:
		return ParseRefused("no error given", -1, out)
	case errors.As(err, &se):
		off = se.Offset
	case errors.As(err, &te):
		off = te.Offset
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		off = int64(len(out))
	case errors.As(err, &ne):
		off = int64(strings.IndexFunc(ne.Num, func(r rune) bool { return !strings.ContainsRune("+-0123456789._eE", r) }))
		if off < 0 && errors.Is(ne.Err, strconv.ErrRange) {
			off = 0
		}
	}
	return fmt.Sprintf("parse failed at byte %d: %s; first bytes %q", off, RedactN(err.Error(), 120), FirstBytes(out))
}

// ParseRefused is a parse the caller refused itself (a header, a field, a
// shape): why, the byte where it stopped, and the first 200 bytes, redacted.
func ParseRefused(why string, at int, out []byte) string {
	return fmt.Sprintf("parse failed at byte %d: %s; first bytes %q", at, why, FirstBytes(out))
}

// Cause is what plus err's text, redacted and bounded, keeping err for
// errors.Is and errors.As: an error that says what failed and why, never
// a bare "failed".
func Cause(what string, err error) error {
	if err == nil {
		return errors.New(what)
	}
	return &causeError{what: what, err: err}
}

type causeError struct {
	what string
	err  error
}

func (c *causeError) Error() string {
	return c.what + ": " + RedactN(c.err.Error(), CommandMax+100)
}
func (c *causeError) Unwrap() error { return c.err }

// Refusal is an edge that will not act, with the one fix: what verbs show
// as Problem and Fix (verbs.AsRefusal passes it through as a refusal).
type Refusal struct{ Problem, Fix string }

func (r *Refusal) Error() string { return r.Problem + ": " + r.Fix }
