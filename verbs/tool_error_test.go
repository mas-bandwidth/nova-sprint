package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A failed tool's stderr reaches the refusal as its last line, with nothing
// that may be a secret and no home directory.
func TestStderrLineKeepsTheErrorAndNoSecret(t *testing.T) {
	stderr := []byte("starting\nnova-secrets: open /Users/someone/.config/nova/store.age: AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQ NOVA_PG_PASSWORD=hunter2 postgres://u:pw@db/x token ghp_abcdefABCDEF0123456789 c2VjcmV0LWtleS0xMjM0NTY3ODkwYWJj\n\n")
	line := StderrLine(stderr)
	if !strings.HasPrefix(line, "nova-secrets: open .../store.age:") {
		t.Fatalf("line %q", line)
	}
	for _, leak := range []string{"AGE-SECRET-KEY", "hunter2", "u:pw", "ghp_", "c2VjcmV0", "/Users", "someone", "starting", "\n"} {
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
	long := StderrLine([]byte(strings.Repeat("word ", 100)))
	if len(long) > stderrLineMax+3 {
		t.Fatalf("unbounded %d", len(long))
	}
}

// The setup walk's refusal is one line carrying the tool's own error.
func TestFailedBusProbeNamesTheToolError(t *testing.T) {
	w := newWorld(t)
	w.bus.err = fmt.Errorf("bus secret names unavailable%s", ToolDetail(Failure("nova-secrets", errors.New("exit status 1"), []byte("nova-secrets: the seat key does not open the store\n"))))
	r := w.e.Execute(t.Context(), []string{"check"})
	if r.Exit != 2 || strings.Count(r.Output, "\n") != 1 || !strings.Contains(r.Output, `nova-bus who failed (bus secret names unavailable (nova-secrets: exit -1 (exit status 1); stderr "nova-secrets: the seat key does not open the store"))`) {
		t.Fatalf("%+v", r)
	}
}

func TestParseFailureNamesTheByteAndWhatItSaw(t *testing.T) {
	out := []byte("SECRETS EXEC STARTING as=x keys=1 cmd=env\n{\"result\":{}}")
	var v map[string]any
	got := ParseFailure(json.Unmarshal(out, &v), out)
	if !strings.Contains(got, "byte 1") || !strings.Contains(got, "SECRETS EXEC STARTING") {
		t.Fatalf("%s", got)
	}
}
