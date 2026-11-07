package edge

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// A failed child's error names the command, its exit code and the first
// lines of stdout and stderr, and never a secret's value: names only.
func TestFailedCarriesCommandExitAndOutputWithNoSecret(t *testing.T) {
	t.Parallel()
	cmd := Cmd{
		Argv: []string{"/usr/local/bin/nova-secrets", "exec", "--key", "AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQ", "NOVA_PG_PASSWORD=hunter2", "postgres://sprint:pw@db/x"},
		Env:  []string{"GH_TOKEN=ghp_abcdefABCDEF0123456789", "NOVA_PG_PASSWORD=hunter2"},
	}
	res := Result{ExitCode: 3, Stdout: "SECRETS EXEC STARTING\nsecond\nthird\n", Stderr: "nova-secrets: open /Users/someone/.config/x.age: denied\nnext line\n"}
	err := Failed(cmd, res, nil)
	got := err.Error()
	for _, want := range []string{"nova-secrets exec --key", "NOVA_PG_PASSWORD=<redacted>", "exit 3", "nova-secrets: open .../x.age: denied | next line", "SECRETS EXEC STARTING | second ... third", "[env GH_TOKEN,NOVA_PG_PASSWORD]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q lacks %q", got, want)
		}
	}
	for _, leak := range []string{"AGE-SECRET-KEY", "hunter2", "sprint:pw", "ghp_", "/Users", "someone"} {
		if strings.Contains(got, leak) {
			t.Fatalf("%q leaks %q", got, leak)
		}
	}
	if len(got) > FailureMax+3 || strings.Contains(got, "\n") {
		t.Fatalf("not one bounded line: %d %q", len(got), got)
	}
	if Failed(cmd, Result{}, nil) != nil {
		t.Fatal("exit 0 is no failure")
	}
	timeout := &ExecError{Program: "git", Kind: ErrTimeout, Reason: "timed out after 1s", Fix: "raise it"}
	err = Failed(Cmd{Argv: []string{"git", "fetch"}}, Result{ExitCode: -1}, timeout)
	if !errors.Is(err, timeout) || !strings.Contains(err.Error(), "git fetch: exit -1 (exec git (timeout)") || !strings.Contains(err.Error(), "no output") {
		t.Fatalf("%v", err)
	}
}

func TestParseFailureNamesTheByteAndWhatItSaw(t *testing.T) {
	t.Parallel()
	out := []byte("SECRETS EXEC STARTING NOVA_KEY=AGE-SECRET-KEY-1ABCDEFGHIJKLMNOPQRST\n{\"a\":1}")
	var v map[string]any
	got := ParseFailure(json.Unmarshal(out, &v), out)
	if !strings.Contains(got, "byte 1") || !strings.Contains(got, "SECRETS EXEC STARTING") || strings.Contains(got, "AGE-SECRET") {
		t.Fatalf("%s", got)
	}
	_, err := strconv.ParseInt("12x4", 10, 64)
	if got := ParseFailure(err, []byte("12x4")); !strings.Contains(got, "byte 2") || !strings.Contains(got, `"12x4"`) {
		t.Fatalf("%s", got)
	}
	trunc := []byte(`{"a":`)
	if got := ParseFailure(json.Unmarshal(trunc, &v), trunc); !strings.Contains(got, "byte 5") {
		t.Fatalf("%s", got)
	}
}

func TestCauseKeepsTheCauseRedactedAndUnwraps(t *testing.T) {
	t.Parallel()
	base := errors.New("dial postgres://u:secretpw@db:5432/nova failed: password: hunter2 rejected; password authentication failed for user")
	err := Cause("store unavailable", base)
	if !errors.Is(err, base) || !strings.HasPrefix(err.Error(), "store unavailable: dial postgres://<redacted>@db") || strings.Contains(err.Error(), "secretpw") || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), "password authentication failed") {
		t.Fatalf("%v", err)
	}
}

// Redaction is idempotent: a redacted line redacted again is the same line,
// so a wrapped CommandError keeps its quotes.
func TestRedactIsIdempotent(t *testing.T) {
	t.Parallel()
	in := `x: exit 1; stderr "NOVA_KEY=abc password: hunter2 at /Users/someone/f.txt postgres://u:p@h/d"`
	once := RedactN(in, 0)
	if twice := RedactN(once, 0); twice != once || !strings.HasSuffix(once, `"`) {
		t.Fatalf("%q then %q", once, twice)
	}
}

// A failed child's stderr reaches its CommandError whole: every non-empty
// line but nova-secrets' banner, joined, redacted, the first lines kept
// when it is cut; nova-secrets' own refusals stay.
func TestFailureCarriesEveryStderrLineButTheBanner(t *testing.T) {
	t.Parallel()
	stderr := "SECRETS EXEC STARTING as=x keys=1 cmd=nova-sprint\n! SECRETS EXEC OK as=x keys=1\nnova-sprint: refused: one\n\nnova-sprint: fix: two\nthree NOVA_PG_PASSWORD=hunter2\nfour\nSECRETS EXEC FAIL invocation: missing '--'\n"
	err := Failure([]string{"nova-secrets", "exec"}, nil, 1, "", stderr, nil)
	if err.Stderr != "nova-sprint: refused: one | nova-sprint: fix: two | three NOVA_PG_PASSWORD=<redacted> | four | SECRETS EXEC FAIL invocation: missing '--'" {
		t.Fatalf("%q", err.Stderr)
	}
	if !strings.Contains(err.Error(), err.Stderr[:40]) || strings.Contains(err.Error(), "hunter2") {
		t.Fatal(err)
	}
	long := StderrLines("cause\n" + strings.Repeat("x y ", 2000) + "\nlast\n")
	if len(long) > StderrMax+3 || !strings.HasPrefix(long, "cause | x y") || strings.Contains(long, "last") {
		t.Fatalf("%d %q", len(long), long[:20])
	}
}

// A harness that paints its output ends with a bare reset; the cause is the
// error line above it (defect 25: the probe reported "\x1b[0m").
func TestCauseLineSkipsEscapesAndKeepsTheErrorLine(t *testing.T) {
	t.Parallel()
	out := "\x1b[91m\x1b[1mError: \x1b[0mInsufficient credits. Add more\n\x1b[0m\n\x1b[0m"
	if got := CauseLine(out, 200); got != "Error: Insufficient credits. Add more" {
		t.Fatalf("CauseLine = %q", got)
	}
	if got := CauseLine("starting\nwarming up\n\x1b[0m\n", 200); got != "warming up" {
		t.Fatalf("no error line: want the last non-empty line, got %q", got)
	}
	if got := CauseLine("request FAILED: 502\nretrying\n", 200); got != "request FAILED: 502" {
		t.Fatalf("case-insensitive match: got %q", got)
	}
	if got := StderrLines("\x1b[31mboom\x1b[0m\n"); got != "boom" {
		t.Fatalf("StderrLines keeps escapes: %q", got)
	}
}
