package verbs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestStreamAddAndSet(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("carol", "stream", "add", "t", "--repo", "o/r", "--forge", "github"), 0, "STREAM t repo=o/r base=main mode=auto forge=github applied at seq", "next: nova-sprint add --stream t")
	if s := w.snap(t).Streams["t"]; s.Forge != "github" || s.Mode != "auto" || s.Pos != 1 {
		t.Fatalf("stream %+v", s)
	}
	want(t, w.do("carol", "stream", "set", "t", "--mode", "hand", "--forge", "none"), 0, "STREAM t mode=hand forge=none applied")
	want(t, w.do("carol", "stream", "set", "t", "--mode", "hand"), 0, "unchanged")
	if s := w.snap(t).Streams["t"]; s.Forge != "" || s.Mode != "hand" {
		t.Fatalf("stream %+v", s)
	}
}

func TestStreamRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no repo", as: "carol", args: []string{"stream", "add", "t"}, exit: 2, want: []string{"--repo <org/repo>"}},
		{name: "a bad name", as: "carol", args: []string{"stream", "add", "a/b", "--repo", "o/r"}, exit: 2, want: []string{"a stream's name"}},
		{name: "a bad mode", as: "carol", args: []string{"stream", "add", "t", "--repo", "o/r", "--mode", "fast"}, exit: 2, want: []string{"--mode is one of"}},
		{name: "a bad forge", as: "carol", args: []string{"stream", "add", "t", "--repo", "o/r", "--forge", "gitlab"}, exit: 2, want: []string{"--forge is github, or none"}},
		{name: "exists", as: "carol", args: []string{"stream", "add", "s", "--repo", "o/r"}, exit: 1, want: []string{"stream s exists", "nova-sprint stream set s"}},
		{name: "no base at origin", as: "carol", args: []string{"stream", "add", "t", "--repo", "o/r", "--base", "dev"}, exit: 1, want: []string{"branch dev is not at o/r's origin"}},
		{name: "origin fails", as: "carol", args: []string{"stream", "add", "t", "--repo", "o/r"}, exit: 2,
			prep: func(_ *testing.T, w *world) { w.origin.err = errBroken }, want: []string{"git ls-remote of o/r failed"}},
		{name: "set of no stream", as: "carol", args: []string{"stream", "set", "t", "--mode", "hand"}, exit: 1, want: []string{"no stream t", "nova-sprint stream add t"}},
		{name: "set of nothing", as: "carol", args: []string{"stream", "set", "s"}, exit: 2, want: []string{"stream set changes --mode, --forge"}},
		{name: "a worker", as: "alice", args: []string{"stream", "add", "t", "--repo", "o/r"}, exit: 1, want: []string{"stream add is the coordinator's"}},
	})
}

// brief writes a brief file named id.md and returns its path.
func brief(t *testing.T, id, needs string) string {
	t.Helper()
	text := "tier: pro\nPATHS: a.go\nTEST: go test ./...\nDONE-WHEN: it works\n"
	if needs != "" {
		text += "NEEDS: " + needs + "\n"
	}
	path := filepath.Join(t.TempDir(), id+".md")
	if err := os.WriteFile(path, []byte(text+"\nDo the thing.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAddIntoAnEmptySprint is the first add of a sprint with no cards: the
// plan's replay reads the cards as they were before the add, so the new
// card is not refused as existing.
func TestAddIntoAnEmptySprint(t *testing.T) {
	s := sprint()
	s.Cards = map[string]store.Card{}
	w := worldFrom(t, s)
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a1", "")), 0, "ADD a1 kind=producer", "ADDED 1 cards")
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "b1", "a1"), "--brief-file", brief(t, "c1", "")), 0, "ADDED 2 cards")
	for _, id := range []string{"a1", "b1", "c1"} {
		if c := w.snap(t).Cards[id]; c.Kind != "producer" {
			t.Fatalf("card %s: %+v", id, c)
		}
	}
}

func TestAdd(t *testing.T) {
	w := newWorld(t)
	a, b := brief(t, "a1", ""), brief(t, "b1", "a1")
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", b, "--brief-file", a, "--dry-run"), 0, "ADD a1", "ADD b1", "(dry run)")
	if _, ok := w.snap(t).Cards["a1"]; ok {
		t.Fatal("a dry run added")
	}
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", b, "--brief-file", a, "--priority", "high"), 0, "ADD b1 kind=producer stream=s tier=pro priority=high needs=a1", "ADDED 2 cards")
	want(t, w.do("carol", "add", "--stream", "s", "--sentinel", "done", "--needs", "a1,b1"), 0, "ADD done kind=sentinel", "ADDED 1 cards")
	if c := w.snap(t).Cards["b1"]; c.Kind != "producer" || c.Priority != "high" {
		t.Fatalf("card %+v", c)
	}
}

func TestAddRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no stream", as: "carol", args: []string{"add", "--sentinel", "x", "--needs", "p1"}, exit: 2, want: []string{"add names its stream"}},
		{name: "neither form", as: "carol", args: []string{"add", "--stream", "s"}, exit: 2, want: []string{"one of them"}},
		{name: "a sentinel with no needs", as: "carol", args: []string{"add", "--stream", "s", "--sentinel", "x"}, exit: 1, want: []string{"a sentinel needs the cards it waits for"}},
		{name: "an unknown stream", as: "carol", args: []string{"add", "--stream", "t", "--sentinel", "x", "--needs", "p1"}, exit: 1, want: []string{"no stream t"}},
		{name: "a bad needs list", as: "carol", args: []string{"add", "--stream", "s", "--sentinel", "x", "--needs", "p1,"}, exit: 2, want: []string{"--needs takes a comma list"}},
		{name: "an unreadable brief", as: "carol", args: []string{"add", "--stream", "s", "--brief-file", "/nonexistent/x.md"}, exit: 2, want: []string{"cannot read brief"}},
		{name: "a card that exists", as: "carol", args: []string{"add", "--stream", "s", "--sentinel", "p1", "--needs", "p2"}, exit: 1, want: []string{"card p1 exists"}},
		{name: "an unknown need", as: "carol", args: []string{"add", "--stream", "s", "--sentinel", "x", "--needs", "zz"}, exit: 1, want: []string{"fix the brief or the flags"}},
	})
	w := newWorld(t)
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "x1", "y1"), "--brief-file", brief(t, "y1", "x1")), 1, "the cards need each other")
	bad := filepath.Join(t.TempDir(), "z1.md")
	if err := os.WriteFile(bad, []byte("tier: pro\nTEST: x\nDONE-WHEN: y\n\nprose\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", bad), 1, "z1: line", "PATHS")
	group := filepath.Join(t.TempDir(), "group.md")
	if err := os.WriteFile(group, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", group), 1, "card id group is the stream's group branch")
	a := brief(t, "a1", "")
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", a, "--brief-file", a), 2, "card a1 is named twice")
}

func TestJudge(t *testing.T) {
	blocked := withJudgement("blocked.p3.1", "", "blocked", "p3", "unblock | drop")
	s := sprint()
	blocked(&s)
	dependant(&s)
	setCard("p3", func(c *store.Card) { c.State = "blocked" })(&s)
	w := worldFrom(t, s)
	want(t, w.do("carol", "judge"), 0, "JUDGEMENT blocked.p3.1 kind=blocked about=p3", "choices=unblock|drop")
	want(t, w.do("carol", "judge", "blocked.p3.1"), 0, "question: what now?")
	want(t, w.do("carol", "judge", "blocked.p3.1", "unblock", "--reason", "fixed by hand"), 0, "judgement blocked.p3.1 answered unblock", "applied at seq")
	want(t, w.do("carol", "judge"), 0, "no open judgements")
	want(t, w.do("carol", "judge", "blocked.p3.1", "unblock", "--reason", "again"), 1, "judgement blocked.p3.1 was answered by carol")
}

func TestJudgeAGroup(t *testing.T) {
	s := sprint()
	withJudgement("stall.p1.1", "g", "stall", "p1", "retry")(&s)
	withJudgement("stall.p2.1", "g", "stall", "p2", "retry")(&s)
	w := worldFrom(t, s)
	want(t, w.do("carol", "judge", "g"), 0, "next: nova-sprint judge g <choice> --expect 2")
	want(t, w.do("carol", "judge", "g", "retry", "--reason", "r"), 1, "group g has 2 open judgements", "confirm with --expect 2")
	want(t, w.do("carol", "judge", "g", "retry", "--reason", "r", "--expect", "2"), 0, "stall.p1.1 answered retry", "stall.p2.1 answered retry")
}

func TestJudgeRefusals(t *testing.T) {
	blocked := withJudgement("blocked.p3.1", "", "blocked", "p3", "unblock | drop")
	runCases(t, []tcase{
		{name: "no judgement", as: "carol", args: []string{"judge", "zz"}, exit: 1, want: []string{"no open judgement or group zz", "nova-sprint judge"}},
		{name: "no reason", as: "carol", snap: blocked, args: []string{"judge", "blocked.p3.1", "unblock"}, exit: 1, want: []string{"needs its reason"}},
		{name: "a choice not offered", as: "carol", snap: blocked, args: []string{"judge", "blocked.p3.1", "retry", "--reason", "r"}, exit: 2, want: []string{"takes one of: unblock drop"}},
		{name: "a worker", as: "alice", snap: blocked, args: []string{"judge"}, exit: 1, want: []string{"judge is the coordinator's"}},
	})
}

func TestHold(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("carol", "hold", "alice", "s", "--reason", "r", "--dry-run"), 0, "row alice held=true (dry run)", "next: nova-sprint hold alice s --reason")
	want(t, w.do("carol", "hold", "alice", "s", "--reason", "r"), 0, "row alice held=true", "stream s held=true", "next: "+waitNext)
	if s := w.snap(t); !s.Rows["alice"].Held {
		t.Fatalf("rows %+v", s.Rows)
	}
	want(t, w.do("carol", "unhold", "alice", "s", "--reason", "r"), 0, "row alice held=false", "stream s held=false")
}

func TestHoldRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no reason", as: "carol", args: []string{"hold", "alice"}, exit: 1, want: []string{"a hold needs its reason"}},
		{name: "no name", as: "carol", args: []string{"hold", "--reason", "r"}, exit: 2, want: []string{"hold takes"}},
		{name: "no such row", as: "carol", args: []string{"hold", "zz", "--reason", "r"}, exit: 1, want: []string{"no row, stream or card zz"}},
		{name: "an unhold of nothing held", as: "carol", args: []string{"unhold", "alice", "--reason", "r"}, exit: 1, want: []string{"nova-sprint show alice"}},
	})
}

func TestPriority(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("carol", "priority", "p1", "high", "--reason", "glenn asked"), 0, "p1 priority=high")
	if c := w.snap(t).Cards["p1"]; c.Priority != "high" {
		t.Fatalf("card %+v", c)
	}
}

func TestPriorityRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "a bad level", as: "carol", args: []string{"priority", "p1", "urgent", "--reason", "r"}, exit: 2, want: []string{"a level is one of"}},
		{name: "no reason", as: "carol", args: []string{"priority", "p1", "high"}, exit: 1, want: []string{"a priority needs its reason"}},
		{name: "no card", as: "carol", args: []string{"priority", "zz", "high", "--reason", "r"}, exit: 1, want: []string{"no card zz", "nova-sprint queue"}},
		{name: "a consumer", as: "carol", args: []string{"priority", "p1.w1", "high", "--reason", "r"}, exit: 1, want: []string{"p1.w1 is a consumer", "nova-sprint priority p1 high"}},
	})
}

// dependant adds p4, a ready producer, and p3, waiting on it.
func dependant(s *store.Snapshot) {
	s.Cards["p3"] = store.Card{ID: "p3", Kind: "producer", Stream: "s", State: "waiting", Gen: 1, Attempt: 1, Tier: "pro", Priority: "normal", Needs: []string{"p4"}}
	s.Cards["p4"] = store.Card{ID: "p4", Kind: "producer", Stream: "s", State: "ready", Gen: 1, Attempt: 1, Tier: "pro", Priority: "normal"}
}

func TestDropAndReplace(t *testing.T) {
	s := sprint()
	dependant(&s)
	w := worldFrom(t, s)
	want(t, w.do("carol", "drop", "p4", "--reason", "not needed", "--dry-run"), 0, "(dry run)")
	r := w.do("carol", "drop", "p4", "--reason", "not needed", "--waive", "p3")
	want(t, r, 0, "p4->fail")
	if c := w.snap(t).Cards["p3"]; len(c.Needs) != 0 {
		t.Fatalf("card %+v", c)
	}
}

func TestReplaceAndRelink(t *testing.T) {
	s := sprint()
	dependant(&s)
	w := worldFrom(t, s)
	want(t, w.do("carol", "replace", "p4", "--by", "p2", "--reason", "p2 does it"), 0, "p4->fail")
	if c := w.snap(t).Cards["p3"]; len(c.Needs) != 1 || c.Needs[0] != "p2" {
		t.Fatalf("card %+v", c)
	}
	s = sprint()
	dependant(&s)
	setCard("p4", func(c *store.Card) { c.State = "fail" })(&s)
	w = worldFrom(t, s)
	want(t, w.do("carol", "relink", "p3", "--need", "p4", "--to", "p2", "--reason", "p2 does it"), 0, "p3 needs=p2")
}

func TestDropRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no reason", as: "carol", args: []string{"drop", "p2"}, exit: 1, want: []string{"a drop needs its reason"}},
		{name: "a consumer", as: "carol", args: []string{"drop", "p1.w1", "--reason", "r"}, exit: 1, want: []string{"p1.w1 is a consumer", "nova-sprint drop p1"}},
		{name: "a bad relink", as: "carol", args: []string{"drop", "p2", "--reason", "r", "--relink", "p3"}, exit: 2, want: []string{"--relink is <dependant>=<card>"}},
		{name: "waived and relinked", as: "carol", args: []string{"drop", "p2", "--reason", "r", "--waive", "p3", "--relink", "p3=p1"}, exit: 2, want: []string{"p3 is both waived and relinked"}},
		{name: "a replace with no by", as: "carol", args: []string{"replace", "p2", "--reason", "r"}, exit: 2, want: []string{"--by <card>"}},
		{name: "a live consumer", as: "carol", args: []string{"drop", "p1", "--reason", "r"}, exit: 1, want: []string{"nova-sprint show p1"}},
		{name: "a relink with no need", as: "carol", args: []string{"relink", "p3", "--to", "p1", "--reason", "r"}, exit: 2, want: []string{"relink takes --need"}},
		{name: "a relink of no failed need", as: "carol", snap: dependant, args: []string{"relink", "p3", "--need", "p4", "--to", "p1", "--reason", "r"}, exit: 1, want: []string{"nova-sprint show p3"}},
	})
}

// stillClock is a policy clock that never moves and never expires.
type stillClock struct{}

func (stillClock) Now() time.Time                       { return t0 }
func (stillClock) After(time.Duration) <-chan time.Time { return nil }

func withPolicy(t *testing.T, w *world) {
	t.Helper()
	w.e.Policy = policy.NewEngine(stillClock{})
	if _, err := policy.Up(t.Context(), w.f, policy.Caller{Actor: "carol", Coordinator: true, OpID: "up-policy", At: t0}); err != nil {
		t.Fatal(err)
	}
}

func TestPolicy(t *testing.T) {
	w := newWorld(t)
	withPolicy(t, w)
	file := filepath.Join(t.TempDir(), "mine.star")
	if err := os.WriteFile(file, []byte("# mine\n"+policy.DefaultBody()), 0o600); err != nil {
		t.Fatal(err)
	}
	want(t, w.do("carol", "policy", "set", file, "--reason", "try"), 0, "POLICY STORED v2", "next: nova-sprint policy check v2")
	want(t, w.do("alice", "policy", "show", "--list"), 0, "v2")
	want(t, w.do("alice", "policy", "show", "v2"), 0, "# mine")
	want(t, w.do("alice", "policy", "diff", "v1", "v2"), 0)
	want(t, w.do("alice", "policy", "check", "v2"), 0)
	want(t, w.do("carol", "policy", "activate", "2", "--reason", "go"), 0, "POLICY ACTIVE v2")
}

func TestPolicyRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no engine", as: "carol", args: []string{"policy", "show"}, exit: 2, want: []string{"the policy engine is not configured"}},
		{name: "a worker sets", as: "alice", args: []string{"policy", "set", "x", "--reason", "r"}, exit: 1, want: []string{"policy set is the coordinator's"}},
		{name: "a bad version", as: "carol", prep: withPolicy, args: []string{"policy", "activate", "vx", "--reason", "r"}, exit: 2, want: []string{"a version is 7 or v7"}},
		{name: "no reason", as: "carol", prep: withPolicy, args: []string{"policy", "activate", "1"}, exit: 1, want: []string{"reason"}},
		{name: "an unreadable file", as: "carol", prep: withPolicy, args: []string{"policy", "set", "/nonexistent.star", "--reason", "r"}, exit: 1, want: []string{"set"}},
		{name: "before up", as: "carol", prep: func(_ *testing.T, w *world) { w.e.Policy = policy.NewEngine(stillClock{}) },
			args: []string{"policy", "show", "v1"}, exit: 1},
	})
}

// TestHoldCard: hold parks one producer blocked with the reason, its live
// consumer withdrawn in the same op; unhold sends it to waiting (defect 47).
func TestHoldCard(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("carol", "hold", "p1", "--reason", "wrong brief"), 0, "p1", "next: "+waitNext)
	s := w.snap(t)
	if c := s.Cards["p1"]; c.State != "blocked" || c.Reason != card.HeldPrefix+"wrong brief" {
		t.Fatalf("held card %+v", c)
	}
	if c := s.Cards["p1.w1"]; c.State != "fail" {
		t.Fatalf("its consumer %+v", c)
	}
	want(t, w.do("carol", "unhold", "p1", "--reason", "brief fixed"), 0, "p1")
	if c := w.snap(t).Cards["p1"]; c.State != "waiting" {
		t.Fatalf("unheld card %+v", c)
	}
}

func TestHoldCardRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "a consumer", as: "carol", args: []string{"hold", "p1.w1", "--reason", "r"}, exit: 1, want: []string{"p1.w1 is a work card; hold its producer", "nova-sprint hold p1 --reason"}},
		{name: "an unhold of a card not held", as: "carol", args: []string{"unhold", "p2", "--reason", "r"}, exit: 1, want: []string{"nova-sprint show p2"}},
		{name: "a worker", as: "alice", args: []string{"hold", "p1", "--reason", "r"}, exit: 1, want: []string{"hold is the coordinator's"}},
	})
}
