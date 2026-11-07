package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// An unknown outcome never leaks: a take (the acknowledgement) whose commit
// was lost is applied again; one that landed is its stored receipt.
func TestUnknownOutcomeReplaysOrApplies(t *testing.T) {
	t.Parallel()
	for _, landed := range []bool{false, true} {
		w := newWorld(t)
		w.f.Drop(consumerOpID("take", 1, "p1.w1", 1), landed)
		want(t, w.do("alice", "take", "p1.w1@1"), 0, "TAKEN p1.w1")
		s := w.snap(t)
		if c := s.Cards["p1.w1"]; c.State != "working" || c.Gen != 1 {
			t.Fatalf("landed=%v: card %+v", landed, c)
		}
		if _, ok := s.Cursors[steps.AckCursor(1, "p1.w1")]; !ok {
			t.Fatalf("landed=%v: no acknowledgement in %v", landed, s.Cursors)
		}
	}
}

func TestNextOrdersAndCounts(t *testing.T) {
	t.Parallel()
	s := sprint()
	busyAlice(&s)
	w := worldFrom(t, s)
	want(t, w.do("alice", "next"), 0, "work p1.w1 is in hand with alice (1 more: nova-sprint next --as alice --json lists them)")
}

func TestQueueEmpty(t *testing.T) {
	t.Parallel()
	s := sprint()
	for _, id := range []string{"p1", "p2"} {
		setCard(id, func(c *store.Card) { c.State = "fail" })(&s)
	}
	quiet(&s)
	w := worldFrom(t, s)
	want(t, w.do("alice", "queue"), 0, "the queue is empty")
	want(t, w.do("carol", "next"), 0, "every card is final")
}

func TestFinishEdgeFailures(t *testing.T) {
	t.Parallel()
	withForge := func(s *store.Snapshot) { st := s.Streams["s"]; st.Forge = "github"; s.Streams["s"] = st }
	finish := []string{"finish", "p1.w1@1", "--head", head}
	runCases(t, []tcase{
		// no PR is asked of a card; the landing opens the group's
		{name: "a forge stream, no PR", as: "alice", snap: withForge, args: finish, exit: 0, prep: takeW1,
			want: []string{"FINISHED p1.w1"}},
		{name: "no git edge", as: "alice", args: finish, exit: 2, prep: func(t *testing.T, w *world) { takeW1(t, w); w.e.Origin = nil }, want: []string{"the git edge is not configured"}},
	})
}

// Each brief rule is refused, by line, without quoting the brief.
func TestBriefLint(t *testing.T) {
	t.Parallel()
	good := "tier: pro\nPATHS: a.go\nTEST: go test ./...\nDONE-WHEN: it works\n\nDo it.\n"
	cases := map[string]string{
		"must be tier: <flash|pro|heavy|frontier>":   "PATHS: a.go\n",
		"line 1 must be tier":                        "tier: huge\nPATHS: a.go\n",
		"must be a recognized metadata field":        strings.Replace(good, "PATHS: a.go", "OWNER: me", 1),
		"must contain a valid NEEDS section":         strings.Replace(good, "PATHS: a.go", "PATHS: a.go\nDEPENDS-ON: x", 1),
		"must contain a valid PRIORITY section":      strings.Replace(good, "PATHS: a.go", "PATHS: a.go\nPRIORITY: urgent", 1),
		"must contain a valid PATHS section":         strings.Replace(good, "PATHS: a.go", "PATHS: /etc/x", 1),
		"must contain a valid TEST section":          strings.Replace(good, "TEST: go test ./...\n", "", 1),
		"must include task prose after a blank line": strings.TrimSuffix(good, "\nDo it.\n") + "\n",
	}
	for rule, text := range cases {
		_, err := parseBrief("b1", text)
		var lint *BriefLintError
		if !errors.As(err, &lint) || !strings.Contains(err.Error(), rule) || strings.Contains(err.Error(), "a.go") {
			t.Errorf("%q: %v", rule, err)
		}
	}
	a, err := parseBrief("b1", strings.Replace(good, "PATHS: a.go", "PATHS: a.go, b/c.go\nNEEDS: x, y\nAFTER: none\nPRIORITY: high", 1))
	if err != nil || len(a.Paths) != 2 || len(a.Needs) != 2 || a.After != nil || a.Priority != "high" || a.Prose != "Do it." {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestPolicyCheckRefusesAFileThatFails(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	withPolicy(t, w)
	file := filepath.Join(t.TempDir(), "bad.star")
	if err := os.WriteFile(file, []byte("def broken(:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := w.do("alice", "policy", "check", file)
	if r.Exit != 1 || !strings.HasSuffix(r.Output, "again\n") || strings.Count(r.Output, "SPRINT REFUSED") != 1 {
		t.Fatalf("exit %d:\n%s", r.Exit, r.Output)
	}
}
