package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"THE TASK. same\nDEADLINE: 120", "THE TASK. same\nDEADLINE: 240", true},
		{"THE TASK. same\nDeadline: finish within 120 minutes", "THE TASK. same\nDeadline: finish within 240 minutes", true},
		{"THE TASK. same", "THE TASK. same\nNOTE: the benches are shared", true},
		{"THE TASK. same\nSTEP 1. make -j16", "THE TASK. same\nSTEP 1. make -j4", true},
		{"THE TASK. same\nSTEP 1. make -j$(nproc) && ctest -j 8", "THE TASK. same\nSTEP 1. make -j4 && ctest -j4", true},
		{"THE TASK. same\nSTEP 1. make -j16", "THE TASK. same\nSTEP 1. make -j4 install", false},
		{"THE TASK. same\nDEADLINE: 120", "THE TASK. same", false},
		{"THE TASK. same\nDONE-WHEN: a", "THE TASK. same\nDONE-WHEN: b\nDEADLINE: 240", false},
	} {
		require.Equal(t, tc.operational, operationalBriefOnly(tc.old, tc.next), "%q -> %q", tc.old, tc.next)
	}
}

// 2026-10-24: a script re-briefed 36 working cards to change only -j16 to -j4 and DEADLINE
// 120 to 240, with --replace-in-flight, and every finish came back "brief replaced". An edit
// whose only changed lines are the deadline, a NOTE, or a build's -jN is operational: it
// applies in place with no flag and the in-flight head stays eligible for review.
func TestAnEditOfOnlyTheDeadlineNotesAndJobsKeepsTheInFlightHead(t *testing.T) {
	t.Parallel()
	const head = "0123456789012345678901234567890123456789"
	w := setup(t, 1)
	brief := w.s.Work.Card("s1-1").F("brief") + "\nDEADLINE: 120\nDeadline: finish within 120 minutes; report what you have\nSTEP 4. Run make -j16 && ctest -j16 --output-on-failure"
	w.s.Work.Card("s1-1").Fields["brief"] = brief
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	c := w.s.Fleet.Card("s1-1.w1")
	w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
	before := w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt)
	edited := strings.NewReplacer("DEADLINE: 120", "DEADLINE: 240", "within 120 minutes", "within 240 minutes", "-j16", "-j4").Replace(brief) + "\nNOTE: the benches are shared"
	p := w.must(Brief(w.s, briefOne("s1-1", edited)))
	assert.Empty(t, p.Refused)
	assert.Equal(t, edited, w.s.Work.Card("s1-1").F("brief"), "applied in place")
	assert.Equal(t, before, w.s.Work.Card("s1-1").F(FieldHeadBriefAttempt), "the binding stands")
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: head}))
	require.Equal(t, Review, w.state("s1-1"), "the finish is reviewed, never \"brief replaced\"")
	assert.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
}

// A substantive edit still needs --replace-in-flight, and with it the edit says plainly how
// many in-flight heads it returned, and which.
func TestReplaceInFlightSaysHowManyHeadsItReturned(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 2}}))
	var cards []BriefCard
	for _, id := range []string{"s1-1", "s1-2"} {
		wc := w.s.Fleet.Card(id + ".w1")
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
		cards = append(cards, BriefCard{ID: id, Brief: w.s.Work.Card(id).F("brief") + "\nTHE TASK. changed acceptance"})
	}
	p := w.must(Brief(w.s, BriefReq{Cards: cards, ReplaceInFlight: true}))
	said := strings.Join(p.Said, "\n")
	assert.Contains(t, said, "--replace-in-flight returned 2 in-flight head(s)")
	assert.Contains(t, said, "s1-1,s1-2")

	// kept: none returned, none said
	w2 := setup(t, 1)
	w2.must(Deal(w2.s, DealReq{Sel: Sel{Limit: 1}}))
	wc := w2.s.Fleet.Card("s1-1.w1")
	w2.must(Take(w2.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w2.s, wc.ID)}))
	req := briefOne("s1-1", w2.s.Work.Card("s1-1").F("brief")+"\nTHE TASK. changed acceptance")
	req.ReplaceInFlight, req.KeepHead = true, true
	p = w2.must(Brief(w2.s, req))
	assert.NotContains(t, strings.Join(p.Said, "\n"), "returned")
}
