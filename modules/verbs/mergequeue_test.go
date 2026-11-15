package verbs

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// mergeQueueWorld is the sprint with p2 merging (on a forge stream) and the
// clock at t0 plus later.
func mergeQueueWorld(t *testing.T, later time.Duration, judged bool) *world {
	t.Helper()
	s := sprint()
	c := s.Cards["p2"]
	c.State = "merging"
	s.Cards["p2"] = c
	delete(s.Cards, "p2.r1")
	st := s.Streams["s"]
	st.Forge = "github"
	s.Streams["s"] = st
	if judged {
		j := judgement("land.p2.1", "land", "p2", t0)
		j.Command = "nova-sprint judge land.p2.1 <choice> --reason <why>   choices: land | fix | retry"
		s.Judgements[j.ID] = j
	}
	w := worldFrom(t, s)
	healthy(t, w)
	w.e.Now = func() time.Time { return t0.Add(later) }
	return w
}

// TestCheckMergeQueueRed: a card merging longer than timeouts merging_nag
// (300 s by default) is CHECK RED, with the count, the oldest and its age
// (at least, when the store's history does not show it go merging), and
// the command that clears it: its land judgement's, else its hand landing.
// Inside the threshold it is no red line.
func TestCheckMergeQueueRed(t *testing.T) {
	t.Parallel()
	if r := mergeQueueWorld(t, time.Minute, false).do("carol", "check"); strings.Contains(r.Output, "merge queue") {
		t.Fatalf("a minute merging is no red line:\n%s", r.Output)
	}
	r := mergeQueueWorld(t, 10*time.Minute, false).do("carol", "check")
	want(t, r, 1)
	head := sprint().Cards["p2"].Head
	line := "CHECK RED merge queue: 1 cards merging longer than 5m0s (timeouts merging_nag); oldest p2 merging at least 10m0s: nova-sprint land p2 --head " + head + " --reason \"<why>\""
	if !strings.Contains(r.Output, line+"\n") {
		t.Fatalf("no merge queue red line %q in:\n%s", line, r.Output)
	}
	r = mergeQueueWorld(t, 10*time.Minute, true).do("carol", "check")
	if !strings.Contains(r.Output, "oldest p2 merging at least 10m0s: nova-sprint judge land.p2.1 <choice> --reason <why>") {
		t.Fatalf("the judged card's red line names its judgement's command:\n%s", r.Output)
	}
}

// TestMergingSinceReadsTheHistory: the age is from the event that moved
// the card into merging, not the oldest event read.
func TestMergingSinceReadsTheHistory(t *testing.T) {
	t.Parallel()
	s := sprint()
	before := store.State{Cards: map[string]store.Card{"p2": s.Cards["p2"]}}
	m := s.Cards["p2"]
	m.State = "merging"
	after := store.State{Cards: map[string]store.Card{"p2": m}}
	snap := s
	snap.Seq, snap.Epoch = 2, s.Epoch
	snap.Cards["p2"] = m
	evs := []store.Event{{Seq: 1, At: t0, Accepted: true, Epoch: s.Epoch}, {Seq: 2, At: t0.Add(4 * time.Minute), Accepted: true, Epoch: s.Epoch, Before: &before, After: &after}}
	got, floor := mergingSinceOf(evs, snap, t0.Add(time.Hour))
	if !got["p2"].Equal(t0.Add(4*time.Minute)) || !floor.Equal(t0) {
		t.Fatalf("since %v floor %v", got, floor)
	}
}
