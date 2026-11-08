package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// A failed tool's stderr reaches the refusal as every non-empty line but
// nova-secrets' banner, with nothing that may be a secret and no home
// directory.
func TestStderrLineKeepsTheErrorAndNoSecret(t *testing.T) {
	t.Parallel()
	stderr := []byte("SECRETS EXEC STARTING as=x keys=1 cmd=nova-sprint\nstarting\nnova-secrets: open /Users/someone/.config/nova/store.age: AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQ NOVA_PG_PASSWORD=hunter2 postgres://u:pw@db/x token ghp_abcdefABCDEF0123456789 c2VjcmV0LWtleS0xMjM0NTY3ODkwYWJj\n\n")
	line := StderrLine(stderr)
	if !strings.HasPrefix(line, "starting | nova-secrets: open .../store.age:") {
		t.Fatalf("line %q", line)
	}
	for _, leak := range []string{"SECRETS EXEC", "AGE-SECRET-KEY", "hunter2", "u:pw", "ghp_", "c2VjcmV0", "/Users", "someone", "\n"} {
		if strings.Contains(line, leak) {
			t.Fatalf("%q leaks %q", line, leak)
		}
	}
	err := fmt.Errorf("bus secret names unavailable%s", ToolDetail(Failure("nova-secrets", errors.New("exit status 1"), stderr)))
	if !strings.HasPrefix(err.Error(), "bus secret names unavailable (nova-secrets: exit -1 (exit status 1); stderr \"starting | nova-secrets: open .../store.age:") {
		t.Fatal(err)
	}
	if ToolDetail(errors.New("plain")) != " (plain)" || ToolDetail(nil) != "" || StderrLine(nil) != "" || Failure("x", nil, stderr) != nil {
		t.Fatal("no tool error is no detail")
	}
	long := StderrLine([]byte("first cause\n" + strings.Repeat("word ", 1000) + "\nlast"))
	if len(long) > stderrLineMax+3 || !strings.HasPrefix(long, "first cause | word") || strings.Contains(long, "last") {
		t.Fatalf("not bounded with the first lines kept: %d %q...", len(long), long[:40])
	}
}

// A refusal deep inside an internal child (an *OperationError: a pg_dump
// too old, a role the admin does not administer) is written as two stderr
// lines and read back by the parent as the same Problem and Fix, through
// nova-secrets' banner, never re-wrapped as the parent's own failure.
func TestChildRefusalPassesProblemAndFixThrough(t *testing.T) {
	t.Parallel()
	inner := &OperationError{Problem: `pg_dump is older than the server (pg_dump 14 vs server 18; saw "pg_dump (PostgreSQL) 14.9")`, Fix: "put PostgreSQL 18's bin directory first on PATH, then run nova-sprint up again", Exit: 1}
	var stderr bytes.Buffer
	stderr.WriteString("SECRETS EXEC STARTING as=x keys=1 cmd=nova-sprint\n")
	writeChildFailure(&stderr, cause("first backup failed", inner))
	child := edge.Failure([]string{"nova-secrets", "exec", "--", "nova-sprint", "--internal-store-setup"}, nil, 1, "", stderr.String(), nil)
	got := AsRefusal(fmt.Errorf("store child failed: %w", child))
	op, ok := got.(*OperationError)
	if !ok || op.Problem != inner.Problem || op.Fix != inner.Fix || op.Exit != 1 {
		t.Fatalf("%#v from %q", got, child.Stderr)
	}
	if f := classify(fmt.Errorf("store child failed: %w", child)); f.What != inner.Problem || f.Exit != exitRefused {
		t.Fatalf("classify %+v", f)
	}
	if AsRefusal(cause("x", inner)) != inner || AsRefusal(errors.New("plain")) != nil || AsRefusal(nil) != nil {
		t.Fatal("in-process refusal passes as itself; a plain error is none")
	}
	plain := edge.Failure([]string{"pg_dump"}, nil, 1, "", "pg_dump: error: connection refused\n", nil)
	if AsRefusal(plain) != nil {
		t.Fatal("a tool's own stderr is no refusal")
	}
	var w bytes.Buffer
	writeChildFailure(&w, errors.New("schema creation failed: ERROR: permission denied to create role (SQLSTATE 42501)"))
	if w.String() != "nova-sprint: schema creation failed: ERROR: permission denied to create role (SQLSTATE 42501)\n" {
		t.Fatalf("%q", w.String())
	}
}

// The setup walk's refusal is one line carrying the tool's own error.
func TestFailedBusProbeNamesTheToolError(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.bus.err = fmt.Errorf("bus secret names unavailable%s", ToolDetail(Failure("nova-secrets", errors.New("exit status 1"), []byte("nova-secrets: the seat key does not open the store\n"))))
	r := w.e.Execute(t.Context(), []string{"check"})
	if r.Exit != 2 || strings.Count(r.Output, "SPRINT REFUSED") != 1 || !strings.Contains(r.Output, `nova-bus who failed (bus secret names unavailable (nova-secrets: exit -1 (exit status 1); stderr "nova-secrets: the seat key does not open the store"))`) {
		t.Fatalf("%+v", r)
	}
}

func TestParseFailureNamesTheByteAndWhatItSaw(t *testing.T) {
	t.Parallel()
	out := []byte("SECRETS EXEC STARTING as=x keys=1 cmd=env\n{\"result\":{}}")
	var v map[string]any
	got := ParseFailure(json.Unmarshal(out, &v), out)
	if !strings.Contains(got, "byte 1") || !strings.Contains(got, "SECRETS EXEC STARTING") {
		t.Fatalf("%s", got)
	}
}

// A refusal built from a failing command whose argv and environment carry
// secret-looking values names them and never shows a value.
func TestRefusalFromFailingCommandNamesSecretsNotValues(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	argv := []string{"/usr/local/bin/nova-secrets", "exec", "--", "env", "NOVA_PG_PASSWORD=hunter2", "postgres://u:pw@db/x", "--token", "ghp_abcdefABCDEF0123456789"}
	env := []string{"NOVA_SPRINT_PG_PASSWORD=s3cr3t-value", "GH_TOKEN=ghp_ZYXWVUTSRQ0123456789ab"}
	failed := edge.Failure(argv, env, 3, "", "nova-secrets: key AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQ rejected\n", errors.New("exit status 3"))
	w.bus.err = fmt.Errorf("bus secret names unavailable%s", ToolDetail(failed))
	r := w.e.Execute(t.Context(), []string{"check"})
	if r.Exit != 2 || strings.Count(r.Output, "SPRINT REFUSED") != 1 {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{"nova-secrets exec", "NOVA_PG_PASSWORD=<redacted>", "NOVA_SPRINT_PG_PASSWORD", "GH_TOKEN", "exit 3", "rejected"} {
		if !strings.Contains(r.Output, want) {
			t.Fatalf("%q lacks %q", r.Output, want)
		}
	}
	for _, leak := range []string{"hunter2", "u:pw", "ghp_", "s3cr3t", "AGE-SECRET-KEY", "/usr/local"} {
		if strings.Contains(r.Output, leak) {
			t.Fatalf("%q leaks %q", r.Output, leak)
		}
	}
}
