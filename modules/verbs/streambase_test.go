package verbs

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// stream set --base points an existing stream at another branch at origin:
// one write, its receipt naming the old base and what happens to the
// stream's cards. A card working finishes its attempt on the old base and
// its head lands onto the new one; a card merging has its gate let go and
// its group rebuilt on the new base at the landing's next pass.
func TestStreamSetBase(t *testing.T) {
	t.Parallel()
	s := sprint()
	p2 := s.Cards["p2"]
	p2.State, p2.Review, p2.Head = "merging", 0, head
	s.Cards["p2"] = p2
	w := worldFrom(t, s)
	w.origin.tips["o/r sprint-s"] = tip
	want(t, w.do("carol", "stream", "set", "s", "--base", "sprint-s"), 0,
		"STREAM s base=sprint-s (was main) mode=auto forge=none applied at seq",
		"cards merging (p2): the landing lets go of any gate built on main and builds the group on sprint-s at its next pass",
		"cards working (p1): each finishes its attempt on main and its head lands onto sprint-s; cards taken from now on, and every retry, start from sprint-s")
	if got := w.snap(t).Streams["s"]; got.Base != "sprint-s" || got.Repo != "o/r" || got.Mode != "auto" || got.Pos != 0 {
		t.Fatalf("stream %+v, want only its base changed", got)
	}
	want(t, w.do("carol", "stream", "set", "s", "--base", "sprint-s"), 0, "unchanged")
}

func TestStreamSetBaseRefusals(t *testing.T) {
	t.Parallel()
	onBase := func(s *store.Snapshot) { // p2 merged onto the stream's base dev0, waiting for its promotion to main
		st := s.Streams["s"]
		st.Base = "dev0"
		s.Streams["s"] = st
		p2 := s.Cards["p2"]
		p2.State, p2.Review, p2.Head, p2.Reason = "merging", 0, head, store.OnBaseReason("dev0", tip)
		s.Cards["p2"] = p2
	}
	dev := func(_ *testing.T, w *world) { w.origin.tips["o/r dev"] = tip }
	runCases(t, []tcase{
		{name: "a base not at origin", as: "carol", args: []string{"stream", "set", "s", "--base", "dev"}, exit: 1,
			want: []string{"branch dev is not at o/r's origin", "set the stream again"}},
		{name: "a base that is the card branches' parent", as: "carol", args: []string{"stream", "set", "s", "--base", "sprint/s"}, exit: 1,
			want: []string{"the base sprint/s is a branch"}},
		{name: "cards merged onto the old base, not yet on main", as: "carol", args: []string{"stream", "set", "s", "--base", "dev"}, exit: 1, snap: onBase, prep: dev,
			want: []string{"stream s has cards merged onto dev0 that wait for their promotion to main (p2)", "let the promotion land them on main (nova-sprint show s), then set the base again"}},
		{name: "a worker", as: "alice", args: []string{"stream", "set", "s", "--base", "dev"}, exit: 1, want: []string{"stream set is the coordinator's"}},
	})
}
