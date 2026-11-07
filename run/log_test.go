package run

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
)

// The log says each failure's cause, redacted: a name, never a secret's
// value (errors are reported, never swallowed).
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

// TestRosterRefusedLogsOncePerChange: the same refused rows read each tick
// are logged once; a change logs the new set; an empty set says none.
func TestRosterRefusedLogsOncePerChange(t *testing.T) {
	var out bytes.Buffer
	l := NewWriterLogger(&out)
	bad := []roster.Refused{{Name: "bench-1", Kind: roster.KindWorker, Reason: "machine bench-1 has 0 cores; set its cores in nova-config"}}
	l.RosterRefused(nil)
	if out.Len() != 0 {
		t.Fatalf("nothing refused, nothing was logged before: %q", out.String())
	}
	l.RosterRefused(bad)
	l.RosterRefused(bad)
	l.RosterRefused(bad)
	if got := strings.Count(out.String(), "ROSTER REFUSED"); got != 1 || !strings.Contains(out.String(), `ROSTER REFUSED worker name="bench-1" reason="machine bench-1 has 0 cores`) {
		t.Fatalf("want one line, log %q", out.String())
	}
	l.RosterRefused(nil)
	l.RosterRefused(nil)
	if !strings.HasSuffix(out.String(), "ROSTER REFUSED none\n") || strings.Count(out.String(), "none") != 1 {
		t.Fatalf("log %q", out.String())
	}
}
