package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cardHoldingKinds is every judgment kind that holds a card from progressing, read from the
// code apart from WaitRefused: the judgments a rework discharges on its primary
// (ReworkResolves), the stream stops, and the kinds that keep a card waiting or stopped.
func cardHoldingKinds() []string {
	out := append([]string(nil), ReworkResolves...)
	return append(out, NConflict, NRed, NCross, NRejected, NBaseRed, NBriefDefect, NBlocked, NMissingNeed, NSentinelReached)
}

// Parking is impossible for every judgment that holds a card: each refuses wait, naming the
// action instead, but the few kinds WaitAllowedOnACard names with why.
func TestEveryKindThatHoldsACardRefusesWait(t *testing.T) {
	t.Parallel()
	for _, k := range cardHoldingKinds() {
		if why, ok := WaitAllowedOnACard[k]; ok {
			assert.NotEmpty(t, why, "%s: an exception says why", k)
			assert.NotContains(t, WaitRefused, k, "%s: an exception is not refused too", k)
			continue
		}
		refusal := WaitRefusal(Note{Kind: Judgment, Type: k, ID: "j1"})
		assert.NotEmpty(t, refusal, "%s holds a card and must refuse wait", k)
		assert.Contains(t, refusal, "holds its card", k)
	}
	assert.Len(t, WaitAllowedOnACard, 3, "the exceptions stay few: a new one is a decision")
}

// The inbox never offers a verb the kind refuses: no kind of WaitRefused lists wait among its
// decisions, and every kind of WaitRefused has decisions to offer instead.
func TestNoJudgmentOffersADecisionItsVerbRefuses(t *testing.T) {
	t.Parallel()
	for k, ds := range Decisions {
		assert.Empty(t, OffersRefusedVerb(k, ds), "%s offers a refused verb: %v", k, ds)
	}
	for k, ds := range TickDecisions {
		assert.Empty(t, OffersRefusedVerb(k, ds), "%s offers a refused verb: %v", k, ds)
	}
	for _, k := range WaitRefusedKinds() {
		ds := append(append([]string(nil), Decisions[k]...), TickDecisions[k]...)
		assert.NotEmpty(t, ds, "%s takes no wait and must offer what to do instead", k)
	}
}

// A stopped stream holds its cards: the held count counts the cards it holds from landing
// (Stella's read of 2026-10-11: "stopped streams count zero held cards").
func TestAStoppedStreamCountsTheCardsItHolds(t *testing.T) {
	t.Parallel()
	w, _ := friendInReview(t, false, "friend amy LAND: done")
	require.Equal(t, Review, w.state("s1-1"))
	assert.Zero(t, CardsHeldByJudgments(w.s), "no judgment is open on it")
	stop := Note{Kind: Judgment, Type: NRed, Stream: "s1", StreamLevel: true, ID: "j-red"}
	w.s.Open = append(w.s.Open, Open{Key: OpenKey(stop.ID, StreamSubject("s1")), Note: stop})
	assert.Equal(t, 1, CardsHeldByJudgments(w.s), "the stopped stream holds its card in review")
}
