package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const scopeWidenBaseBrief = "s1-1: widen scope (s1) tier: pro\n" +
	"REPO: mas-bandwidth/nova-sprint\n" +
	"BASE: v1.3/comms\n" +
	"DONE-WHEN: tests pass\n" +
	"START: internal/a.go (read)\n" +
	"PATHS: internal/a.go, internal/b.go\n" +
	"NEW: internal/a_test.go\n" +
	"SHARED: docs/CLI.md\n" +
	"TEST: ./internal/sprint TestX\n" +
	"DEPENDS-ON: -\n\n" +
	"THE TASK. Implement the fix."

const scopeWidenedBrief = "s1-1: widen scope (s1) tier: pro\n" +
	"REPO: mas-bandwidth/nova-sprint\n" +
	"BASE: v1.3/comms\n" +
	"DONE-WHEN: tests pass\n" +
	"START: internal/a.go (read), internal/b.go (read)\n" +
	"PATHS: internal/a.go, internal/b.go, internal/c.go\n" +
	"NEW: internal/a_test.go, internal/b_test.go\n" +
	"SHARED: docs/CLI.md, docs/SPEC-SPRINT.md\n" +
	"TEST: ./internal/sprint TestX\n" +
	"DEPENDS-ON: -\n\n" +
	"THE TASK. Implement the fix."

func scopeWidenWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b", "reader-c")
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	w.must(Add(w.s, AddReq{Brief: scopeWidenBaseBrief, Stream: "s1", Count: 1}))
	w.clean("setup")
	return w
}

func TestAScopeWideningBriefEditKeepsTheFinish(t *testing.T) {
	const head = "0123456789012345678901234567890123456789"

	w := scopeWidenWorld(t)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	c := w.s.Fleet.Card("s1-1.w1")
	w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
	beforeHeadGen := w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt)

	bp := w.must(Brief(w.s, briefOne("s1-1", scopeWidenedBrief)))
	require.Equal(t, beforeHeadGen, w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt), "scope-widening edit must preserve head binding")
	require.Empty(t, w.s.Work.Card("s1-1").F(FieldBriefAttempt), "scope-widening edit must not reset brief_attempt")
	require.Contains(t, bp.Units[0].Moved, "scope widened in place")
	require.Contains(t, w.s.Work.Card("s1-1").F("why"), "scope widened in place")
	require.Contains(t, strings.Join(bp.Said, "\n"), "bound: same task (1 attempts stand)")

	// Operational note combined with scope widening also preserves the head binding.
	w.must(Brief(w.s, briefOne("s1-1", strings.Replace(scopeWidenedBrief, "PATHS: internal/a.go, internal/b.go, internal/c.go", "PATHS: internal/a.go, internal/b.go, internal/c.go, internal/d.go\nROUTE NOTE: use the assigned Linux bench", 1))))
	require.Equal(t, beforeHeadGen, w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt))

	fp := w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: head}))
	require.Equal(t, Review, w.state("s1-1"))
	require.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
	require.NotContains(t, fp.Units[0].Moved, "brief replaced")

	t.Run("review card stays in review with head and read cards", func(t *testing.T) {
		w := scopeWidenWorld(t)
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
		c := w.s.Fleet.Card("s1-1.w1")
		w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: head}))
		w.must(Ask(w.s, AskReq{}))
		require.Len(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1), 2)

		bp := w.must(Brief(w.s, briefOne("s1-1", scopeWidenedBrief)))
		require.Equal(t, Review, w.state("s1-1"))
		require.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
		require.Len(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1), 2)
		require.Equal(t, scopeWidenedBrief, w.s.Work.Card("s1-1").F("brief"))
		require.Empty(t, w.s.Work.Card("s1-1").F(FieldBriefAttempt))
		require.Contains(t, bp.Units[0].Moved, "scope widened in place")
		require.Contains(t, w.s.Work.Card("s1-1").F("why"), "scope widened in place")
		require.Contains(t, strings.Join(bp.Said, "\n"), "bound: same task (1 attempts stand)")
	})

	for _, tc := range []struct {
		name string
		next string
	}{
		{
			name: "DONE-WHEN edit bounces",
			next: strings.Replace(scopeWidenBaseBrief, "DONE-WHEN: tests pass", "DONE-WHEN: every test passes", 1),
		},
		{
			name: "task-text edit bounces",
			next: strings.Replace(scopeWidenBaseBrief, "THE TASK. Implement the fix.", "THE TASK. Change the contract.", 1),
		},
		{
			name: "PATHS entry removed bounces",
			next: strings.Replace(scopeWidenBaseBrief, "PATHS: internal/a.go, internal/b.go", "PATHS: internal/a.go, internal/c.go", 1),
		},
		{
			name: "START entry without read bounces",
			next: strings.Replace(scopeWidenBaseBrief, "START: internal/a.go (read)", "START: internal/a.go (read), internal/b.go", 1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := scopeWidenWorld(t)
			w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
			c := w.s.Fleet.Card("s1-1.w1")
			w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))

			refused := Brief(w.s, briefOne("s1-1", tc.next))
			require.NotEmpty(t, refused.Refused)
			require.Contains(t, refused.Refused[0].Why, "--replace-in-flight")

			req := briefOne("s1-1", tc.next)
			req.ReplaceInFlight = true
			w.must(Brief(w.s, req))
			require.True(t, AttemptPredatesBrief(w.s.Work.Card("s1-1"), 1))

			w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: head}))
			require.Equal(t, Ready, w.state("s1-1"))
			require.Empty(t, w.s.Work.Card("s1-1").F("head"))
			require.Contains(t, w.s.Fleet.Card(c.ID).F("report"), "brief replaced:")
		})
	}
}

func TestScopeWidenedOnlyIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		name string
		old  string
		next string
		want bool
	}{
		{"paths added", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go, b.go\n\nTHE TASK. x", true},
		{"new added", "s1-1: x (s1) tier: pro\nPATHS: a.go\nNEW: a_test.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\nNEW: a_test.go, b_test.go\n\nTHE TASK. x", true},
		{"shared added", "s1-1: x (s1) tier: pro\nPATHS: a.go\nSHARED: docs/A.md\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\nSHARED: docs/A.md, docs/B.md\n\nTHE TASK. x", true},
		{"start read added", "s1-1: x (s1) tier: pro\nPATHS: a.go\nSTART: a.go (read)\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\nSTART: a.go (read), b.go (read)\n\nTHE TASK. x", true},
		{"inserted new line", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\nNEW: a_test.go\n\nTHE TASK. x", true},
		{"paths plus operational note", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go, b.go\n\nTHE TASK. x\nROUTE NOTE: use vision", true},
		{"paths reordered", "s1-1: x (s1) tier: pro\nPATHS: a.go, b.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: b.go, a.go, c.go\n\nTHE TASK. x", false},
		{"paths removed", "s1-1: x (s1) tier: pro\nPATHS: a.go, b.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x", false},
		{"start without read", "s1-1: x (s1) tier: pro\nPATHS: a.go\nSTART: a.go (read)\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\nSTART: a.go (read), b.go\n\nTHE TASK. x", false},
		{"no paths in old", "tier: pro", "tier: pro\nPATHS: other.go", false},
		{"identical", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x", false},
		{"operational note only", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x", "s1-1: x (s1) tier: pro\nPATHS: a.go\n\nTHE TASK. x\nCARRY: h", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, scopeWidenedOnly(tc.old, tc.next))
		})
	}
}
