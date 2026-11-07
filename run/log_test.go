package run

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// The log says each failure's cause, redacted: a name, never a secret's
// value (audit row 28: errors are reported, never swallowed).
func TestWriterLoggerReportsCountsAndRedactedCauses(t *testing.T) {
	var out bytes.Buffer
	l := NewWriterLogger(&out)
	l.Log(TickMetrics{Number: 1, Seq: 9, Took: 2 * time.Second, Counts: TickCounts{Folded: 4, Applied: 2, Refused: 1, Edges: 3}, Err: ErrLockLost})
	l.PushFailed("alice", errors.New("bus send to alice (SPRINT): nova-bus send: exit 1; stderr \"not hooked\""))
	l.EdgeFault("land req-1 stream s", errors.New("git fetch: exit 128"))
	got := out.String()
	for _, want := range []string{"folded=4", "ops=2/1", "edges=3", "OVERRUN", "LOCK LOST", "PUSH FAILED to=alice err=", "not hooked", "EDGE FAULT land req-1 stream s err=\"git fetch: exit 128\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	l.Log(TickMetrics{Number: 2, Err: errors.New("connect postgres://sprint:hunter2@db/nova NOVA_PG_PASSWORD=hunter2 refused")})
	if got := out.String(); strings.Contains(got, "hunter2") || !strings.Contains(got, "status=error err=") || !strings.Contains(got, "NOVA_PG_PASSWORD=<redacted> refused") || l.Err() != nil {
		t.Fatalf("log=%q err=%v", got, l.Err())
	}
}
