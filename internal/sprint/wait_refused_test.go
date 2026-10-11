package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cardVerbs are the decisions that act on a card (or a stream's cards): a judgment that offers
// one names a card it holds until the decision is made.
var cardVerbs = []string{"rework", "drop", "return", "accept", "brief", "rebase", "ask", "resolve", "rank that card", "take the suspect", "release", DecisionRecut}

// cardHoldingKinds is every judgment kind that holds a card from progressing, derived from the
// code apart from WaitRefused, so a new kind cannot be missed: every kind whose decisions
// (Decisions, TickDecisions) offer a verb on a card, and every kind a rework discharges on its
// primary (ReworkResolves).
func cardHoldingKinds() []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, k := range ReworkResolves {
		add(k)
	}
	for _, m := range []map[string][]string{Decisions, TickDecisions} {
		for k, ds := range m {
			for _, d := range ds {
				for _, v := range cardVerbs {
					if strings.HasPrefix(d, v) {
						add(k)
					}
				}
			}
		}
	}
	return out
}

// Parking is impossible for every judgment that holds a card: each refuses wait, naming the
// action instead, but the few kinds WaitAllowedOnACard names with why.
func TestEveryKindThatHoldsACardRefusesWait(t *testing.T) {
	t.Parallel()
	for _, k := range cardHoldingKinds() {
		if _, ok := WaitRefusedWhenItNamesACard[k]; ok {
			assert.NotEmpty(t, WaitRefusal(Note{Kind: Judgment, Type: k, ID: "j1", Primaries: []string{"s1-1"}}), "%s names a card: refused", k)
			continue
		}
		if why, ok := WaitAllowedOnACard[k]; ok {
			assert.NotEmpty(t, why, "%s: an exception says why", k)
			assert.NotContains(t, WaitRefused, k, "%s: an exception is not refused too", k)
			continue
		}
		refusal := WaitRefusal(Note{Kind: Judgment, Type: k, ID: "j1"})
		assert.NotEmpty(t, refusal, "%s holds a card and must refuse wait", k)
		assert.Contains(t, refusal, "holds its card", k)
	}
	assert.LessOrEqual(t, len(WaitAllowedOnACard), 4, "the exceptions stay few: a new one is a decision")
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

// A stall that names a card holds it in place and refuses wait; a stall about a stream with
// no card named holds nothing and still takes one. The inbox offers no wait on the first.
func TestAStallRefusesWaitOnlyWhenItNamesACard(t *testing.T) {
	t.Parallel()
	card := Note{ID: "j1", Kind: Judgment, Type: NStalled, Stream: "s1", Primaries: []string{"s1-1"}, Decisions: Decisions[NStalled]}
	assert.NotEmpty(t, WaitRefusal(card), "a stall that names a card holds it")
	stream := Note{ID: "j2", Kind: Judgment, Type: NStalled, Stream: "s1", StreamLevel: true, Decisions: Decisions[NStalled]}
	assert.Empty(t, WaitRefusal(stream), "a stall about a stream holds nothing")
	g := Group{ID: "j1", Kind: Judgment, Type: NStalled, Stream: "s1", Size: 1, Notes: []string{"j1"}, Members: []string{"s1-1"}, Decisions: card.Decisions}
	for _, c := range commands(g, card, "") {
		assert.NotEqual(t, "wait", c.Decision, "the inbox offers no refused wait")
	}
}

// A judgment shared by two cards, one of them answered, holds only the other: the count reads
// each open entry's own subject, never the note's whole list (Stella's read of 2026-10-11).
func TestAPartlyAnsweredJudgmentCountsOnlyTheCardsStillHeld(t *testing.T) {
	t.Parallel()
	w, _ := friendInReview(t, false, "friend amy LAND: done")
	require.Equal(t, Review, w.state("s1-1"))
	shared := Note{ID: "j-shared", Kind: Judgment, Type: NReadBroken, Stream: "s1", Primaries: []string{"s1-1", "s1-2"}}
	// s1-1's entry was answered and closed; s1-2's entry stays open, its note naming both
	w.s.Open = append(w.s.Open, Open{Key: OpenKey(shared.ID, "s1-2"), Note: shared})
	assert.Zero(t, CardsHeldByJudgments(w.s), "s1-1 was answered: the open entry holds s1-2 alone, which is not on the work table here")
}
