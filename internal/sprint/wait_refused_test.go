package sprint

import (
	"strings"
	"testing"
	"time"

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
	assert.LessOrEqual(t, len(WaitAllowedOnACard), 8, "the exceptions stay few: a new one is a decision")
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
	for _, c := range commands(g, card, "", nil) {
		assert.NotEqual(t, "wait", c.Decision, "the inbox offers no refused wait")
	}
}

// An overdue judgment that holds a card offers no wait anywhere the rendering shows it: the
// inbox group's commands, the hook's push and the act line all render from commands() (cmd/
// nova-sprint/reads.go groupText), so one fix covers all three. The act line names the action
// WaitRefused gives for the kind; a wait stays the act of an alarm (not covered here: a kind
// that takes a wait still prints it, TestEveryDecisionPrintsItsCommands).
func TestOverdueCardHoldingJudgmentOffersNoWait(t *testing.T) {
	t.Parallel()
	kinds := append(WaitRefusedKinds(), NStalled)
	for _, k := range kinds {
		ds := append(append([]string{}, Decisions[k]...), TickDecisions[k]...)
		if len(ds) == 0 {
			ds = []string{"act"}
		}
		n := Note{ID: "j1", Kind: Judgment, Type: k, Stream: "s1", Primaries: []string{"s1-1"}, Decisions: ds}
		g := Group{ID: "j1", Kind: Judgment, Type: k, Stream: "s1", Size: 1, Notes: []string{"j1"}, Members: []string{"s1-1"}, Decisions: append(append([]string{}, ds...), "act")}
		onWork := func(id string) bool { return id == "s1-1" }
		require.NotEmpty(t, WaitRefusalOn(n, onWork), "%s must refuse wait", k)
		want := WaitRefusedAction(n)
		var actLines []string
		for _, c := range commands(g, n, "", onWork) {
			assert.False(t, c.Decision == "wait" || strings.HasPrefix(c.Decision, "wait "), "%s offers a refused decision %q", k, c.Decision)
			for _, l := range c.Lines {
				assert.NotContains(t, l, "nova-sprint wait", "%s offers a refused line %q", k, l)
			}
			if c.Decision == "act" {
				actLines = append(actLines, c.Lines...)
			}
		}
		assert.NotEmpty(t, actLines, "%s: an overdue card-holding judgment renders an act line", k)
		require.NotEmpty(t, want, "%s: WaitRefused names an action", k)
		assert.Contains(t, strings.Join(actLines, "\n"), want, "%s: the act line names the action WaitRefused gives", k)
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

// The run-time rule: a judgment that names a work-table card refuses wait whatever its kind, a
// stop naming a card (NStopPinWaits) and a kind added later alike; one that names no card does
// not.
func TestAJudgmentNamingACardRefusesWaitWhateverItsKind(t *testing.T) {
	t.Parallel()
	w, _ := friendInReview(t, false, "friend amy LAND: done")
	for _, k := range []string{NStopPinWaits, "a kind added next year"} {
		n := Note{ID: "j1", Kind: Judgment, Type: k, Primaries: []string{"s1-1"}}
		assert.NotEmpty(t, WaitRefusalIn(w.s, n), "%s names a card: refused", k)
		w.s.Open = []Open{{Key: OpenKey("j1", "s1-1"), Note: n}}
		p := Wait(w.s, WaitReq{Note: "j1", Until: w.s.Now.Add(time.Hour), Who: w.s.Coordinator})
		require.Len(t, p.Refused, 1, "%s: the wait step refuses", k)
	}
	friend := Note{ID: "j2", Kind: Judgment, Type: NFriendIdle, Primaries: []string{"friend.amy"}}
	assert.Empty(t, WaitRefusalIn(w.s, friend), "a friend is not a card")
	unknown := Note{ID: "j3", Kind: Judgment, Type: "a kind added next year", Primaries: []string{"timer:x"}}
	assert.Empty(t, WaitRefusalIn(w.s, unknown), "a judgment naming no card takes a wait")
}

// The decisions a note carries for itself (a stop that names a card, a drift on a card's base)
// are printed by the same rule the wait verb applies: a judgment naming a work-table card is
// never offered a wait, whatever its kind, unless WaitAllowedOnACard names it.
func TestNoPerNoteDecisionOffersAWaitItsVerbRefuses(t *testing.T) {
	t.Parallel()
	kinds := map[string][]string{}
	for _, m := range []map[string][]string{Decisions, TickDecisions} {
		for k, ds := range m {
			kinds[k] = ds
		}
	}
	for _, k := range []string{NStopPinWaits, NDriftCardBase} {
		require.Contains(t, kinds, k, "%s has decisions of its own", k)
	}
	for k, ds := range kinds {
		if _, allowed := WaitAllowedOnACard[k]; allowed {
			continue
		}
		n := Note{ID: "j1", Kind: Judgment, Type: k, Stream: "s1", Primaries: []string{"s1-1"}, Decisions: append(append([]string(nil), ds...), "wait")}
		for _, c := range NoteCommands(n, []string{"s1-1"}) {
			assert.False(t, c.Decision == "wait" || strings.HasPrefix(c.Decision, "wait "), "%s names a card and offers %q", k, c.Decision)
		}
	}
}
