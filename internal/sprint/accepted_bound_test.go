package sprint_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// An accepted card at the attempt cap is never judged a brief defect (tla/Land.tla
// AcceptedNeverCapJudged): the coordinator's accept admits it to merge, and only
// a repeated refusal (the same finding twice) or exhausted widenings bound it.
// Red before the fix: AtBriefBound judged the accepted card at attempt 4 a brief defect.
func TestAnAcceptedCardAtTheAttemptCapIsNeverJudgedABriefDefect(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	// Fail attempts 1, 2, 3 so the card reaches attempt 4
	for i := 1; i <= 3; i++ {
		r.failedAttempt("s1-1", fmt.Sprintf("tests red: attempt %d failed", i))
		r.tick()
	}
	require.Equal(t, 4, r.snap().Work.Card("s1-1").Int("attempt"), "attempt 4")

	toMergingCoordinatorAt := func(id, head string) {
		if r.snap().Work.Card(id).Col == sprint.Ready {
			r.must(dealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{id}}}))
		}
		s := r.snap()
		wc := s.Fleet.Card(s.Work.Card(id).F("work"))
		require.NotNil(t, wc, id)
		r.must(store.TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}}))
		wc = r.snap().Fleet.Card(wc.ID)
		r.must(store.FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Head: head}))
		for range 4 {
			s = r.snap()
			if pr := s.Work.Card(id); pr.Col != sprint.Review || sprint.ReadsWanted(s, pr) == 0 {
				break
			}
			r.must(store.AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{id}}}))
			for _, rc := range r.snap().Readers.Of(id) {
				if rc.Col == sprint.Asked || rc.Col == sprint.Reading {
					r.must(store.ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: rc.Row, Verdict: "ok", Finding: "f:1", Sel: sprint.Sel{IDs: []string{rc.ID}}}))
				}
			}
		}
		r.must(store.AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{id}}, Who: "coordinator"}))
		r.must(verdictsStep())
		require.Equal(t, sprint.Merging, r.snap().Work.Card(id).Col, id)
	}

	// Accept attempt 4 into merging by coordinator
	toMergingCoordinatorAt("s1-1", refusedHeads[0])

	// Lander refuses its head on a file conflict
	why := "the head " + refusedHeads[0] + " of s1-1 does not merge: CONFLICT (content): Merge conflict in internal/x.go"
	r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", Note: why, ConflictKind: "file", ConflictPaths: []string{"internal/x.go"}}))

	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Ready, pr.Col, "the accepted card at attempt cap is reworked, never judged a brief defect")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "s1-1"), "no brief defect judgment")
	r.tick()
	assert.Equal(t, 5, r.snap().Work.Card("s1-1").Int("attempt"), "dealt to attempt 5")
}

// An accept is a decision on the card whoever accepts (brief_bound.go, FieldDecidedAttempt;
// tla/Land.tla AcceptedNeverCapJudged): both coordinator and pump accept set FieldDecidedAttempt
// to the card's attempt, opening a fresh attempt cap window.
// Red before the fix: Accept in steps_review.go did not write FieldDecidedAttempt.
func TestAnAcceptIsADecisionWhoeverAccepts(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	// Fail attempts 1, 2, 3 so the card reaches attempt 4
	for i := 1; i <= 3; i++ {
		r.failedAttempt("s1-1", fmt.Sprintf("tests red: attempt %d failed", i))
		r.tick()
	}
	require.Equal(t, 4, r.snap().Work.Card("s1-1").Int("attempt"), "attempt 4")

	// Drive attempt 4 through work and reads to review
	if r.snap().Work.Card("s1-1").Col == sprint.Ready {
		r.must(dealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	}
	s := r.snap()
	wc := s.Fleet.Card(s.Work.Card("s1-1").F("work"))
	require.NotNil(t, wc)
	r.must(store.TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}}))
	wc = r.snap().Fleet.Card(wc.ID)
	r.must(store.FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Head: refusedHeads[0]}))
	for range 4 {
		s = r.snap()
		if pr := s.Work.Card("s1-1"); pr.Col != sprint.Review || sprint.ReadsWanted(s, pr) == 0 {
			break
		}
		r.must(store.AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
		for _, rc := range r.snap().Readers.Of("s1-1") {
			if rc.Col == sprint.Asked || rc.Col == sprint.Reading {
				r.must(store.ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: rc.Row, Verdict: "ok", Finding: "f:1", Sel: sprint.Sel{IDs: []string{rc.ID}}}))
			}
		}
	}
	require.Equal(t, sprint.Review, r.snap().Work.Card("s1-1").Col, "card in review at attempt 4")

	// Coordinator accept
	r.must(store.AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Who: "coordinator"}))
	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Merging, pr.Col, "accepted into merging")
	assert.Equal(t, "4", pr.F(sprint.FieldDecidedAttempt), "coordinator accept sets FieldDecidedAttempt to attempt")

	// An accept by the pump also sets FieldDecidedAttempt
	r2 := newConflictRig(t)
	for i := 1; i <= 3; i++ {
		r2.failedAttempt("s1-1", fmt.Sprintf("tests red: attempt %d failed", i))
		r2.tick()
	}
	require.Equal(t, 4, r2.snap().Work.Card("s1-1").Int("attempt"), "attempt 4")
	if r2.snap().Work.Card("s1-1").Col == sprint.Ready {
		r2.must(dealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	}
	s2 := r2.snap()
	wc2 := s2.Fleet.Card(s2.Work.Card("s1-1").F("work"))
	require.NotNil(t, wc2)
	r2.must(store.TakeStep(sprint.TakeReq{As: wc2.Row, Sel: sprint.Sel{IDs: []string{wc2.ID}}, Gens: map[string]int{wc2.ID: wc2.Int("gen")}}))
	wc2 = r2.snap().Fleet.Card(wc2.ID)
	r2.must(store.FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{wc2.ID}}, Gens: map[string]int{wc2.ID: wc2.Int("gen")}, Head: refusedHeads[0]}))
	for range 4 {
		s2 = r2.snap()
		if pr2 := s2.Work.Card("s1-1"); pr2.Col != sprint.Review || sprint.ReadsWanted(s2, pr2) == 0 {
			break
		}
		r2.must(store.AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
		for _, rc := range r2.snap().Readers.Of("s1-1") {
			if rc.Col == sprint.Asked || rc.Col == sprint.Reading {
				r2.must(store.ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: rc.Row, Verdict: "ok", Finding: "f:1", Sel: sprint.Sel{IDs: []string{rc.ID}}}))
			}
		}
	}
	require.Equal(t, sprint.Review, r2.snap().Work.Card("s1-1").Col, "card in review at attempt 4")
	r2.must(store.AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Who: sprint.MachineActor}))
	pr2 := r2.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Merging, pr2.Col, "pump accepted into merging")
	assert.Equal(t, "4", pr2.F(sprint.FieldDecidedAttempt), "pump accept also sets FieldDecidedAttempt to attempt")
}

// The same refusal twice on the same brief is still judged a brief defect (tla/Land.tla
// SameRefusalHalts; sprint.AtBriefBound): a repeated refusal indicates the brief is wrong,
// not the worker, and halts the loop.
func TestTheSameRefusalTwiceIsStillABriefDefect(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	why := func(head string) string {
		return "the head " + head + " of s1-1 does not merge: CONFLICT (content): Merge conflict in internal/x.go"
	}
	r.toMergingAt("s1-1", refusedHeads[0])
	r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", Note: why(refusedHeads[0]), ConflictKind: "file", ConflictPaths: []string{"internal/x.go"}}))
	require.Equal(t, sprint.Ready, r.snap().Work.Card("s1-1").Col, "first refusal reworks it")

	r.toMergingAt("s1-1", refusedHeads[1])
	r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", Note: why(refusedHeads[1]), ConflictKind: "file", ConflictPaths: []string{"internal/x.go"}}))

	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Review, pr.Col, "second refusal with same finding is at brief bound: review")
	open := r.openOnCard(sprint.NBriefWrong, "s1-1")
	require.Len(t, open, 1, "brief defect judgment opened")
	assert.Contains(t, open[0].Note.What, "failed the same way twice")
}

// An accepted card that lands cleanly lands on the sprint branch without issue.
func TestAnAcceptedCardThatLandsCleanLands(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	r.toMergingAt("s1-1", refusedHeads[0])
	r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Landed: []sprint.LandedPin{{ID: "s1-1", Head: refusedHeads[0], InBase: true}}}))

	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Landed, pr.Col, "lands cleanly")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "s1-1"), "no brief defect judgment")
}
