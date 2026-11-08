package verbs

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// merging2 is p2 merging at its read-ok head, its read consumer gone.
func merging2(s *store.Snapshot) {
	setCard("p2", func(c *store.Card) { c.State = "merging" })(s)
	delete(s.Cards, "p2.r1")
}

func landJudged2(s *store.Snapshot) {
	merging2(s)
	withJudgement("land.p2.1", "land", "land", "p2", "land | fix | retry")(s)
}

// land merges the card's own head by hand and records it landed with the
// merge sha and the reason, in one op (intent rule 32).
func TestLandByHand(t *testing.T) {
	t.Parallel()
	s := sprint()
	merging2(&s)
	w := worldFrom(t, s)
	merge := strings.Repeat("e", 40)
	want(t, w.do("carol", "land", "p2", "--reason", "red is a flake"), 0, "LANDED p2 head="+head+" merge="+merge+" by hand", "next: "+waitNext)
	if got := w.lander.landed; len(got) != 1 || got[0] != "sprint/s/p2@"+head {
		t.Fatalf("landed %v", got)
	}
	if c := w.snap(t).Cards["p2"]; c.State != "landed" || !strings.Contains(c.Reason, merge) || !strings.Contains(c.Reason, "by hand: red is a flake") {
		t.Fatalf("p2 %+v", c)
	}
}

func TestLandRefusals(t *testing.T) {
	t.Parallel()
	land := []string{"land", "p2", "--reason", "r"}
	runCases(t, []tcase{
		{name: "no reason", as: "carol", snap: merging2, args: []string{"land", "p2"}, exit: 1, want: []string{"needs its reason", "--reason"}},
		{name: "a worker", as: "alice", snap: merging2, args: land, exit: 1, want: []string{"land is the coordinator's"}},
		{name: "not merging", as: "carol", args: land, exit: 1, want: []string{"p2 is review", "nova-sprint show p2"}},
		{name: "a consumer", as: "carol", args: []string{"land", "p1.w1", "--reason", "r"}, exit: 1, want: []string{"no producer p1.w1"}},
		{name: "another head", as: "carol", snap: merging2, args: []string{"land", "p2", "--reason", "r", "--head", base}, exit: 1,
			want: []string{"p2's finished head is " + head}},
		{name: "the merge fails", as: "carol", snap: merging2, args: land, exit: 2,
			prep: func(_ *testing.T, w *world) { w.lander.landErr = errBroken }, want: []string{"the hand landing of p2 failed"}},
		{name: "no landing edge", as: "carol", snap: merging2, args: land, exit: 2,
			prep: func(_ *testing.T, w *world) { w.e.Lander = nil }, want: []string{"the landing edge is not configured"}},
	})
}

// A land judgement's three answers: land is the hand merge, fix sends the
// producer back to work (ready, red, review one up) with the reason as its
// finding, retry is a new landing
// request (the card stays merging; the next tick asks).
func TestJudgeLandAnswers(t *testing.T) {
	t.Parallel()
	cases := []struct{ choice, state string }{{"land", "landed"}, {"fix", "ready"}, {"retry", "merging"}}
	for _, c := range cases {
		t.Run(c.choice, func(t *testing.T) {
			s := sprint()
			landJudged2(&s)
			w := worldFrom(t, s)
			want(t, w.do("carol", "judge", "land.p2.1", c.choice, "--reason", "looked at the log"), 0, "land.p2.1")
			snap := w.snap(t)
			p := snap.Cards["p2"]
			if p.State != c.state || (c.choice == "fix") != (p.Red && p.Finding == "looked at the log" && p.Review == 2) {
				t.Fatalf("p2 is %s, want %s: %+v", p.State, c.state, p)
			}
			if j := snap.Judgements["land.p2.1"]; j.State == "open" || j.Answer != c.choice {
				t.Fatalf("judgement %+v", j)
			}
			if (c.choice == "land") != (len(w.lander.landed) == 1) {
				t.Fatalf("landed %v for %s", w.lander.landed, c.choice)
			}
		})
	}
}

// stream add creates sprint-group/<stream> at the base tip for a stream
// that lands through a forge, and refuses, naming the fix, when it cannot.
func TestStreamAddCreatesTheGroupBranch(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("carol", "stream", "add", "t", "--repo", "o/r", "--forge", "github"), 0, "STREAM t")
	if got := w.lander.created; len(got) != 1 || got[0] != "t@"+base {
		t.Fatalf("created %v", got)
	}
	w = newWorld(t)
	w.lander.heads = map[string]string{"refs/heads/sprint-group/t": base}
	want(t, w.do("carol", "stream", "add", "t", "--repo", "o/r", "--forge", "github"), 0, "STREAM t")
	if len(w.lander.created) != 0 {
		t.Fatalf("created %v over an existing group branch", w.lander.created)
	}
	w = newWorld(t)
	want(t, w.do("carol", "stream", "add", "t", "--repo", "o/r", "--mode", "hand", "--forge", "none"), 0, "STREAM t")
	if len(w.lander.created) != 0 {
		t.Fatalf("created %v for a hand stream", w.lander.created)
	}
	add := []string{"stream", "add", "t", "--repo", "o/r", "--forge", "github"}
	runCases(t, []tcase{
		{name: "a blocking branch", as: "carol", args: add, exit: 1, prep: func(_ *testing.T, w *world) {
			w.lander.heads = map[string]string{"refs/heads/sprint-group": base}
		}, want: []string{"branch sprint-group at o/r keeps the group branch sprint-group/t from being created", "rename or delete sprint-group"}},
		{name: "a name git refuses", as: "carol", args: []string{"stream", "add", "t.lock", "--repo", "o/r", "--forge", "github"}, exit: 1,
			want: []string{"cannot be created"}},
		{name: "heads unreadable", as: "carol", args: add, exit: 2, prep: func(_ *testing.T, w *world) { w.lander.headErr = errBroken },
			want: []string{"listing o/r's branches failed"}},
		{name: "creation fails", as: "carol", args: add, exit: 2, prep: func(_ *testing.T, w *world) { w.lander.createErr = errBroken },
			want: []string{"creating sprint-group/t at o/r failed", "push access"}},
	})
}

// stream set admits the stream it makes as stream add does: a forge given
// to a stream creates its group branch, and a branch that blocks it
// refuses with nothing written.
func TestStreamSetAdmitsAsAddDoes(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("carol", "stream", "set", "s", "--forge", "github"), 0, "STREAM s mode=auto forge=github applied")
	if got := w.lander.created; len(got) != 1 || got[0] != "s@"+base {
		t.Fatalf("created %v", got)
	}
	w = newWorld(t)
	w.lander.heads = map[string]string{"refs/heads/sprint-group": base}
	want(t, w.do("carol", "stream", "set", "s", "--forge", "github"), 1, "keeps the group branch sprint-group/s from being created")
	if s := w.snap(t).Streams["s"]; s.Forge != "" {
		t.Fatalf("a refused set wrote the forge: %+v", s)
	}
}
