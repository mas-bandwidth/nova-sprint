package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A broken read waits for its in-flight sibling read of the same attempt before it
// reworks: the sibling is begun and its lease is live, so its verdict is still owed
// and a rework now would retire the read and lose its finding
// (rules_read.go ruleReadBroken; tla/SprintRules.tla Part "reads":
// NoInflightReadRetired, ReadAnswered). Time is the rig's clock, advanced by
// assignment; no real time, no sockets.
func TestABrokenReadWaitsForItsInflightSiblingRead(t *testing.T) {
	t.Parallel()

	// reviewAttempt drives id's attempt through its work to review, pushed at readHead.
	reviewAttempt := func(t *testing.T, r *conflictRig, id string) {
		t.Helper()
		if r.snap().Work.Card(id).Col == sprint.Ready {
			r.must(dealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{id}}}))
		}
		s := r.snap()
		wc := s.Fleet.Card(s.Work.Card(id).F("work"))
		require.NotNil(t, wc, id)
		r.must(store.TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}}))
		wc = r.snap().Fleet.Card(wc.ID)
		r.must(store.FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Head: readHead}))
	}
	// twoReads asks two reads of id's attempt and begins the first: it returns the
	// begun read and the other, still asked.
	twoReads := func(t *testing.T, r *conflictRig, id string) (begun, other *sprint.Card) {
		t.Helper()
		reviewAttempt(t, r, id)
		r.must(store.AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{id}}}))
		r.must(store.AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{id}}, Another: true}))
		var reads []*sprint.Card
		for _, rc := range r.snap().Readers.Of(id) {
			if rc.Col == sprint.Asked {
				reads = append(reads, rc)
			}
		}
		require.Len(t, reads, 2, "two reads of %s are asked", id)
		begun, other = reads[0], reads[1]
		r.must(store.ReadStep(sprint.ReadReq{As: begun.Row, Begin: true, Sel: sprint.Sel{IDs: []string{begun.ID}}}))
		return begun, other
	}
	reportBroken := func(t *testing.T, r *conflictRig, rc *sprint.Card, finding string) {
		t.Helper()
		cur := r.snap().Readers.Card(rc.ID)
		require.NotNil(t, cur, rc.ID)
		r.must(store.ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: cur.Row, Verdict: "broken", Finding: finding, Sel: sprint.Sel{IDs: []string{cur.ID}}}))
	}
	reportOK := func(t *testing.T, r *conflictRig, rc *sprint.Card) {
		t.Helper()
		cur := r.snap().Readers.Card(rc.ID)
		require.NotNil(t, cur, rc.ID)
		r.must(store.ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: cur.Row, Verdict: "ok", Finding: "f:1 ok", Sel: sprint.Sel{IDs: []string{cur.ID}}}))
	}
	readBrokenAnswer := func(t *testing.T, r *conflictRig) sprint.RuleAnswer {
		t.Helper()
		answers, _, err := r.st.RuleAnswers(r.ctx)
		require.NoError(t, err)
		for _, a := range answers {
			if a.Rule == sprint.RuleReadBroken {
				return a
			}
		}
		t.Fatalf("no %s answer", sprint.RuleReadBroken)
		return sprint.RuleAnswer{}
	}
	const findingA = "internal/x/a.go:12 drops the error from Close; return it"
	const findingB = "internal/x/a.go:20 ignores the second error; return it too"

	t.Run("a live sibling begun at the attempt holds the rework and is not retired", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		r.readCard()
		begun, other := twoReads(t, r, "r-1")
		reportBroken(t, r, other, findingA)

		a := readBrokenAnswer(t, r)
		require.Equal(t, sprint.ActWaitReads, a.Act, "the broken read waits for its begun sibling")
		assert.True(t, a.Answers(), "the wait answers the judgment: it is not a mind's")
		assert.Contains(t, a.Why, begun.ID, "the why names the read waited on")
		assert.Contains(t, a.Why, "until", "the why names when the wait ends")

		r.tick()
		s := r.snap()
		pr := s.Work.Placed("r-1")
		require.NotNil(t, pr)
		assert.Equal(t, sprint.Review, pr.Col, "the primary stays in review")
		assert.Equal(t, 1, pr.Int("attempt"), "no rework while the sibling is in flight")
		assert.NotNil(t, s.Readers.Placed(begun.ID), "the begun read is not retired")
		assert.Equal(t, sprint.Reading, s.Readers.Placed(begun.ID).Col)
		assert.NotNil(t, s.Readers.Placed(other.ID), "the broken read stands")
	})

	t.Run("the sibling then reports broken: one rework carries both findings", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		r.readCard()
		begun, other := twoReads(t, r, "r-1")
		reportBroken(t, r, other, findingA)
		require.Equal(t, sprint.ActWaitReads, readBrokenAnswer(t, r).Act)
		r.tick()
		reportBroken(t, r, begun, findingB)

		r.tick()
		pr := r.snap().Work.Card("r-1")
		require.NotNil(t, pr)
		assert.Equal(t, 2, pr.Int("attempt"), "reworked once the sibling has reported")
		assert.Contains(t, pr.F("fix"), findingA, "the first finding rides")
		assert.Contains(t, pr.F("fix"), findingB, "and the sibling's")
	})

	t.Run("the sibling reports ok: it never cancels the broken finding", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		r.readCard()
		begun, other := twoReads(t, r, "r-1")
		reportBroken(t, r, other, findingA)
		require.Equal(t, sprint.ActWaitReads, readBrokenAnswer(t, r).Act)
		r.tick()
		reportOK(t, r, begun)

		r.tick()
		pr := r.snap().Work.Card("r-1")
		require.NotNil(t, pr)
		assert.Equal(t, 2, pr.Int("attempt"), "an ok sibling does not cancel a broken one")
		assert.Contains(t, pr.F("fix"), findingA)
	})

	t.Run("a sibling whose lease lapsed does not hold the rework", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		r.readCard()
		begun, other := twoReads(t, r, "r-1")
		reportBroken(t, r, other, findingA)
		require.Equal(t, sprint.ActWaitReads, readBrokenAnswer(t, r).Act)

		cur := r.snap().Readers.Card(begun.ID)
		require.NotNil(t, cur)
		begunAt, err := time.Parse(time.RFC3339, cur.F("begun"))
		require.NoError(t, err)
		r.mu.Lock()
		r.now = begunAt.Add(sprint.DefaultReadLease + time.Second)
		r.mu.Unlock()

		r.tick()
		pr := r.snap().Work.Card("r-1")
		require.NotNil(t, pr)
		assert.Equal(t, 2, pr.Int("attempt"), "the lapsed lease ends the wait")
		assert.Contains(t, pr.F("fix"), findingA, "the broken finding is the fix")
	})

	t.Run("a single broken read still reworks at once", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		r.readCard()
		r.brokenRead("r-1", findingA)

		r.tick()
		pr := r.snap().Work.Card("r-1")
		require.NotNil(t, pr)
		assert.Equal(t, 2, pr.Int("attempt"))
		assert.Contains(t, pr.F("fix"), findingA)
	})
}
