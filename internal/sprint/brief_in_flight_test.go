package sprint

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// Replay the attempt-one bounce of finish-refuses-a-head-not-on-origin:
// operational guidance arrives while the unchanged task is being implemented.
func TestAnOperationalBriefEditKeepsTheInFlightHead(t *testing.T) {
	t.Run("semantic replacement", workingBriefRequiresExplicitReplacement)
	const head = "0123456789012345678901234567890123456789"
	for _, line := range []string{"ROUTE NOTE: use the assigned Linux bench", "CARRY: recover the previous work", "RESUME: continue the current attempt"} {
		t.Run(line, func(t *testing.T) {
			w := setup(t, 1)
			w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
			c := w.s.Fleet.Card("s1-1.w1")
			w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
			before := w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt)
			brief := w.s.Work.Card("s1-1").F("brief") + "\n" + line
			w.must(Brief(w.s, briefOne("s1-1", brief)))
			require.Equal(t, before, w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt), "operational edit must preserve the binding")
			w.must(Brief(w.s, briefOne("s1-1", brief+" (updated)")))
			require.Equal(t, before, w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt), "a second operational edit preserves the same binding")
			w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: head}))
			require.Equal(t, Review, w.state("s1-1"))
			require.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
		})
	}
}

func workingBriefRequiresExplicitReplacement(t *testing.T) {
	const head = "0123456789012345678901234567890123456789"
	for _, extra := range []string{"THE TASK. change the implementation", "PATHS: other.go", "BASE: other", "BENCH: other", "DEPENDS-ON: other", " ROUTE NOTE: indented prose", "ROUTE NOTEBOOK: not an operational header", "ROUTE NOTE: use Linux\nchange the acceptance condition"} {
		t.Run(extra, func(t *testing.T) {
			w := setup(t, 1)
			w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
			c := w.s.Fleet.Card("s1-1.w1")
			w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
			before := w.s.Work.Card("s1-1").F("brief")
			p := Brief(w.s, briefOne("s1-1", before+"\n"+extra))
			require.NotEmpty(t, p.Refused)
			require.Contains(t, p.Refused[0].Why, "working attempt 1")
			require.Contains(t, p.Refused[0].Why, "--replace-in-flight")
			require.Empty(t, p.Units)
			require.Equal(t, before, w.s.Work.Card("s1-1").F("brief"))
			w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: head}))
			require.Equal(t, Review, w.state("s1-1"))
		})
	}
	t.Run("explicit replacement bounces and a later operational note cannot revive it", func(t *testing.T) {
		w := setup(t, 1)
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
		c := w.s.Fleet.Card("s1-1.w1")
		w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
		req := briefOne("s1-1", w.s.Work.Card("s1-1").F("brief")+"\nTHE TASK. changed acceptance")
		req.ReplaceInFlight = true
		w.must(Brief(w.s, req))
		w.must(Brief(w.s, briefOne("s1-1", req.Cards[0].Brief+"\nRESUME: continue on the assigned bench")))
		require.True(t, AttemptPredatesBrief(w.s.Work.Card("s1-1"), 1))
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: head}))
		require.Equal(t, Ready, w.state("s1-1"))
		require.Empty(t, w.s.Work.Card("s1-1").F("head"))
		require.Contains(t, w.s.Fleet.Card(c.ID).F("report"), "brief replaced:")
	})
}

func TestOperationalBriefDiffIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		old, next   string
		operational bool
	}{
		{"THE TASK. same\nCARRY: old", "THE TASK. same\nCARRY: new", true},
		{"THE TASK. same\nRESUME: old", "THE TASK. same\nRESUME: new", true},
		{"THE TASK. same\nROUTE NOTE: old", "THE TASK. same\nROUTE NOTE: new", true},
		{"THE TASK. same\nCARRY: old", "THE TASK. same", false},
		{"THE TASK. same", "THE TASK. changed\nCARRY: old", false},
		{"THE TASK. same", "THE TASK. same", false},
		{"THE TASK. same", "THE TASK. same\n", false},
		{"THE TASK. same\nPATHS: x", "PATHS: x\nTHE TASK. same\nCARRY: old", false},
	} {
		require.Equal(t, tc.operational, operationalBriefOnly(tc.old, tc.next), "%q -> %q", tc.old, tc.next)
	}
}
