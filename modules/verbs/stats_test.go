package verbs

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// stats reset sets the board's stats mark (store.StatsCursor) to the seq the
// coordinator read, as one logged op with its reason; it needs a reason and
// the coordinator.
func TestStatsReset(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	before := w.snap(t).Seq
	want(t, w.do("carol", "stats", "reset", "--reason", "the verdict fix landed"), 0, "STATS RESET applied at seq", "the board's fleet and friends done, ok, failed and ok% count from here")
	s := w.snap(t)
	if got := s.Cursors[store.StatsCursor]; got != before {
		t.Fatalf("the mark is at %d, want the read's seq %d", got, before)
	}
	ev, err := w.f.Events(context.Background(), s.Seq-1, 1)
	if err != nil || len(ev) != 1 || ev[0].Kind != "stats-reset" || ev[0].Actor != "carol" || !ev[0].Accepted {
		t.Fatalf("one logged op: %+v %v", ev, err)
	}
	want(t, w.do("carol", "stats", "reset", "--reason", "again"), 0, "STATS RESET applied at seq")
	if got := w.snap(t).Cursors[store.StatsCursor]; got != s.Seq {
		t.Fatalf("a second reset moves the mark to %d, got %d", s.Seq, got)
	}
	want(t, w.do("carol", "stats", "reset"), 1, "a stats reset needs its reason")
	if r := w.do("alice", "stats", "reset", "--reason", "x"); r.Exit == 0 || !strings.Contains(r.Output, "coordinator") {
		t.Fatalf("a worker cannot reset the stats: %+v", r)
	}
	want(t, w.do("", "help", "stats", "reset"), 0, "stats reset", "--reason")
}
