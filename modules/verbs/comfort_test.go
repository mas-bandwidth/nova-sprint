package verbs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// A hook prints the receipt, the identity and where the playbook is, never
// the playbook itself: it runs after every wrap-around (fix list 25).
func TestHookPrintsWhereThePlaybookIsNotIt(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r := w.do("dave", "hook", "--role", "worker")
	want(t, r, 0, "HOOKED dave role=worker", "playbook: nova-sprint help playbook worker", "next: nova-sprint next --as dave")
	if strings.Contains(r.Output, "# The worker's playbook") || len(r.Output) > 2048 {
		t.Fatalf("hook printed %d bytes:\n%s", len(r.Output), r.Output)
	}
	want(t, w.do("dave", "help", "playbook", "worker"), 0, "# The worker's playbook")
}

// A friend's take says how the card finishes, by verb, and never sends it
// to a JOB.md or a result.json it does not have (fix list 15).
func TestTakeForKinPrintsFinishBlock(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r := w.do("alice", "take", "p1.w1@1")
	want(t, r, 0, "How this card finishes", "git push origin sprint/s/p1",
		"nova-sprint finish p1.w1@1 --head <sha> --as alice --epoch 1",
		`nova-sprint finish p1.w1@1 --failed --reason "<what is not done, and why>" --as alice --epoch 1`,
		"nova-sprint report p1.w1@1 --result fault", "git config user.email")
	r = w.do("bob", "take", "p2.r1@1")
	want(t, r, 0, "How this card finishes", "detached", "nova-sprint read p2.r1@1 yes --as bob --epoch 1",
		`nova-sprint read p2.r1@1 fix --finding "<path>:<line>: <the defect and its repair>" --as bob --epoch 1`)
	for _, out := range []string{r.Output, w.do("alice", "take", "p1.w1@1").Output} {
		if strings.Contains(out, "JOB.md") || strings.Contains(out, "result.json") {
			t.Fatalf("a kin's take names a bench's file:\n%s", out)
		}
	}
}

// show prints a card's brief once: its prose is the brief's task part.
func TestShowPrintsBriefOnce(t *testing.T) {
	t.Parallel()
	brief := "tier: pro\nPATHS: a.go\n\nbuild the unique thing"
	w := worldFrom(t, func() store.Snapshot {
		s := sprint()
		c := s.Cards["p1"]
		c.Brief, c.Prose = brief, "build the unique thing"
		s.Cards["p1"] = c
		return s
	}())
	for _, args := range [][]string{{"show", "p1"}, {"show"}} {
		r := w.do("alice", args...)
		want(t, r, 0, "PATHS: a.go")
		if n := strings.Count(r.Output, "build the unique thing"); n != 1 {
			t.Fatalf("%v prints the task %d times:\n%s", args, n, r.Output)
		}
	}
}

// next lists a dealt card not yet acknowledged before one being worked,
// whatever their deal order: its take is what is owed now.
func TestNextListsDealtFirst(t *testing.T) {
	t.Parallel()
	s := sprint()
	s.Cards["p3"] = store.Card{ID: "p3", Kind: "producer", Stream: "s", State: "working", Gen: 1, Attempt: 1, Tier: "pro", Priority: "blocker", Branch: "sprint/s/p3"}
	s.Cards["p3.w1"] = store.Card{ID: "p3.w1", Kind: "work", Of: "p3", Stream: "s", State: "working", Gen: 1, Attempt: 1, Holder: "alice", Tier: "pro", Branch: "sprint/s/p3", BaseSHA: base, Brief: "do p3", Priority: "blocker"}
	acked("p3.w1")(&s)
	w := worldFrom(t, s)
	r := w.do("alice", "next", "--json")
	want(t, r, 0)
	var v nextView
	if err := json.Unmarshal([]byte(r.Output), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Working) != 2 || v.Working[0].ID != "p1.w1" || v.Working[1].ID != "p3.w1" {
		t.Fatalf("working %+v", v.Working)
	}
	want(t, w.do("alice", "next"), 0, "work p1.w1 is in hand", "next: nova-sprint take p1.w1@1 --as alice --epoch 1")
}
