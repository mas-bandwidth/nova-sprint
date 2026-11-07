package run

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWriterLoggerReportsMeasuredCountsWithoutErrorText(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	l := NewWriterLogger(&out)
	l.Log(TickMetrics{Number: 1, Seq: 9, Took: 2 * time.Second, Counts: TickCounts{Folded: 4, Applied: 2, Refused: 1, Edges: 3}, Err: ErrLockLost})
	l.PushFailed("alice")
	got := out.String()
	for _, want := range []string{"folded=4", "ops=2/1", "edges=3", "OVERRUN", "LOCK LOST", "PUSH FAILED to=alice"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	l.Log(TickMetrics{Number: 2, Err: errors.New("secret-error-body")})
	if strings.Contains(out.String(), "secret-error-body") || l.Err() != nil {
		t.Fatalf("log=%q err=%v", out.String(), l.Err())
	}
}
