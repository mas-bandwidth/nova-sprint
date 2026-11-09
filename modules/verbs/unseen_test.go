package verbs

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// unseenWorld is the sprint with two open stall judgements, opened at 1000
// and 1100 on a fake clock, the clock at now.
func unseenWorld(t *testing.T, now int64) *world {
	t.Helper()
	s := sprint()
	s.Judgements["j1"] = judgement("j1", "stall", "s", time.Unix(1000, 0))
	s.Judgements["j2"] = judgement("j2", "stall", "s", time.Unix(1100, 0))
	w := worldFrom(t, s)
	healthy(t, w)
	w.e.Now = func() time.Time { return time.Unix(now, 0) }
	return w
}

const unseenTail = ": " + unseenFix

// TestUnseenBelowThreshold: judgements open no longer than timeouts
// judgement_unseen (300 s by default) say nothing.
func TestUnseenBelowThreshold(t *testing.T) {
	t.Parallel()
	w := unseenWorld(t, 1300) // oldest 300 s: not over
	for _, args := range [][]string{{"next"}, {"check"}, {"judge"}} {
		if r := w.do("carol", args...); strings.Contains(r.Output, "UNSEEN") {
			t.Fatalf("%v:\n%s", args, r.Output)
		}
	}
}

// TestUnseenAboveThreshold: past the threshold, next, check and every
// hooked verb the coordinator runs print the UNSEEN line first, with the
// count and the oldest's age; a worker's output does not carry it, nor does
// JSON.
func TestUnseenAboveThreshold(t *testing.T) {
	t.Parallel()
	w := unseenWorld(t, 1000+3725) // oldest 1h2m5s
	line := "UNSEEN 2 judgements open, oldest 1h2m5s"
	for _, args := range [][]string{{"next"}, {"judge"}, {"show"}} {
		r := w.do("carol", args...)
		want(t, r, 0)
		if !strings.HasPrefix(r.Output, line+unseenTail+"\n") {
			t.Fatalf("%v does not start with the UNSEEN line:\n%s", args, r.Output)
		}
	}
	r := w.do("carol", "check")
	want(t, r, 1, "SPRINT REFUSED check: 2 lines are red; the first: "+line)
	if !strings.HasPrefix(r.Output, "CHECK RED "+line+unseenTail+"\n") {
		t.Fatalf("check does not start with the UNSEEN red line:\n%s", r.Output)
	}
	for _, r := range []Result{w.do("alice", "next"), w.do("carol", "next", "--json")} {
		if strings.Contains(r.Output, "UNSEEN") {
			t.Fatal(r.Output)
		}
	}
}

// TestUnseenClearsWhenAnswered: answering leaves the line on the younger
// one's age; answering the last clears it, the answering verb's own output
// included.
func TestUnseenClearsWhenAnswered(t *testing.T) {
	t.Parallel()
	w := unseenWorld(t, 1500)
	r := w.do("carol", "judge", "j1", "retry", "--reason", "seen")
	want(t, r, 0, "UNSEEN 1 judgements open, oldest 6m40s"+unseenTail)
	r = w.do("carol", "judge", "j2", "retry", "--reason", "seen")
	want(t, r, 0)
	if strings.Contains(r.Output, "UNSEEN") {
		t.Fatal(r.Output)
	}
	if r := w.do("carol", "next"); strings.Contains(r.Output, "UNSEEN") {
		t.Fatal(r.Output)
	}
	if got := w.snap(t).Judgements["j2"].State; got != "answered" {
		t.Fatalf("j2 is %s", got)
	}
}

// TestUnseenOf is the report alone: no open judgement, an answered one, the
// threshold exact, and the policy's own threshold.
func TestUnseenOf(t *testing.T) {
	t.Parallel()
	at := time.Unix(1000, 0)
	open := store.Snapshot{Judgements: map[string]store.Judgement{"j": judgement("j", "stall", "s", at)}}
	answered := store.Snapshot{Judgements: map[string]store.Judgement{"j": {ID: "j", State: "answered", OpenedAt: at}}}
	for _, c := range []struct {
		snap  store.Snapshot
		now   int64
		limit int
		want  string
	}{
		{store.Snapshot{}, 99999, 300, ""},
		{answered, 99999, 300, ""},
		{open, 1300, 300, ""},
		{open, 1301, 300, "UNSEEN 1 judgements open, oldest 5m1s"},
		{open, 1301, 0, "UNSEEN 1 judgements open, oldest 5m1s"}, // 0: the default
		{open, 1301, 600, ""},
		{open, 1601, 600, "UNSEEN 1 judgements open, oldest 10m1s"},
	} {
		if got := unseenOf(c.snap, time.Unix(c.now, 0), c.limit); got != c.want {
			t.Errorf("now %d limit %d: %q, want %q", c.now, c.limit, got, c.want)
		}
	}
}
