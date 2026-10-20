package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noReaderTierWorld is three heavy primaries in review whose work unit m1 has no
// reader row serving heavy, and a roster reader-m2 that reads flash only: no
// reader of the roster serves heavy besides the card's own worker, whatever the
// rows' states.
func noReaderTierWorld(t *testing.T) (*world, []string) {
	t.Helper()
	w := newWorld(t, "reader-m2")
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 4}))
	w.s.Readers.Texts = map[string]map[string]string{"reader-m2": {"tiers": "flash"}}
	w.s.ReaderStates = map[string]string{"reader-m2": ReaderUp}
	ids := []string{"s1-1", "s1-2", "s1-3"}
	for i, id := range ids {
		putReview(w, id, "tier: heavy", 1, float64(i), "head-"+id)
		putAttemptWork(w, id, 1, "sprint/"+id, "work-"+id, "yes")
		wc := w.s.Fleet.Card(WorkCardID(id, 1))
		wc.Row, wc.Fields["member"] = "m1", "m1"
	}
	return w, ids
}

// TestNoReaderForTheTierIsOneJudgmentWithNoReaskLoop pins the structural wait:
// three heavy cards in review with no reader row serving heavy besides their own
// worker are one NNoReaderForTier judgment naming all three and the tier, with
// the owner's three decisions, no transient cannot ask, no 30m re-ask default,
// and at twice its deadline it is on the owner's delivery lane.
//
// tla/WaitFor.tla ServerDefault/OwnerDeliver (class E cannotaskloop);
// docs/SPEC-SPRINT.md section 6, The readers.
func TestNoReaderForTheTierIsOneJudgmentWithNoReaskLoop(t *testing.T) {
	t.Parallel()
	w, ids := noReaderTierWorld(t)
	plan, _ := TickAsk(w.s, TickReq{})
	w.must(plan)

	notes := w.notesOf(NNoReaderForTier)
	require.Len(t, notes, 1, "three structural waits of one tier are one judgment: %+v", notes)
	n := notes[0]
	assert.True(t, n.StreamLevel, "one judgment per tier")
	assert.Equal(t, TierSubject("heavy"), n.Stream)
	assert.Equal(t, "heavy", n.Tier)
	assert.ElementsMatch(t, ids, n.Primaries, "it names the three")
	assert.Equal(t, 3, n.Count)
	assert.Equal(t, NoReaderForTierDecisions, n.Decisions)
	assert.Contains(t, n.What, "no reader row serves heavy besides the card's own worker")
	assert.Empty(t, w.notesOf(NCannotAsk), "a structural wait is not the transient cannot ask")
	assert.Empty(t, w.notesOf(NFewReaders))
	assert.Empty(t, w.notesOf(NReviewNoReader))

	plan, _ = TickAsk(w.s, TickReq{})
	w.must(plan)
	assert.Len(t, w.notesOf(NNoReaderForTier), 1, "a judgment once, not a tick")

	// no 30m re-ask default: at twice its deadline nothing is answered by the
	// machine, and the owner lane carries the open decision.
	w.s.Now = w.s.Now.Add(2 * DeadlineJudgment)
	defaults := TickDecisionDefaults(w.s, TickReq{})
	assert.Empty(t, defaults.Units, "no wait 30m and re-ask for a structural wait")
	assert.Equal(t, noReaderTierExit, SafeDecisionDefault(NNoReaderForTier))
	body, _, _ := DecisionBatch(w.s.Now, w.s.Open, nil, "owner", nil, w.s)
	assert.Contains(t, body, n.ID, "the owner lane carries the judgment at twice its deadline")

	// the reversed witness: with the split removed by the test seam the same
	// world is the transient wait, never NNoReaderForTier.
	t.Run("the split removed", func(t *testing.T) {
		w, _ := noReaderTierWorld(t)
		noReaderForTierSeam.Store(w.s, func(*Card) (string, bool) { return "", false })
		t.Cleanup(func() { noReaderForTierSeam.Delete(w.s) })
		plan, _ := TickAsk(w.s, TickReq{})
		w.must(plan)
		assert.Empty(t, w.notesOf(NNoReaderForTier), "without the split no tier judgment is raised")
		assert.NotEmpty(t, w.notesOf(NFewReaders), "the transient wait is what remains")
	})
}

// TestARoomOrDownCannotAskStaysTheReaskWait pins the other half of the split: a
// reader row that serves the card's read tier but is down is the transient wait
// the tick re-asks when it frees, never NNoReaderForTier.
//
// tla/WaitFor.tla ReaderFrees/Read; docs/SPEC-SPRINT.md section 6, The readers.
func TestARoomOrDownCannotAskStaysTheReaskWait(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-m2")
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 4}))
	w.s.Readers.Texts = map[string]map[string]string{"reader-m2": {"tiers": "flash"}}
	w.s.ReaderStates = map[string]string{"reader-m2": ReaderAway}
	putReview(w, "s1-1", "tier: flash", 1, 1, "head-s1-1")
	putAttemptWork(w, "s1-1", 1, "sprint/s1-1", "work-s1-1", "yes")
	wc := w.s.Fleet.Card(WorkCardID("s1-1", 1))
	wc.Row, wc.Fields["member"] = "m1", "m1"

	plan, _ := TickAsk(w.s, TickReq{})
	w.must(plan)
	assert.Empty(t, w.notesOf(NNoReaderForTier), "a reader row serves the tier: not structural")
	assert.Len(t, w.notesOf(NFewReaders), 1, "the transient wait stands")
	assert.Empty(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1))

	w.s.ReaderStates["reader-m2"] = ReaderUp
	plan, _ = TickAsk(w.s, TickReq{})
	w.must(plan)
	assert.Empty(t, w.openOn(StreamSubject("")), "the transient wait closed when its reader freed")
	assert.Len(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1), 1, "the reader freed: the card is asked in that tick")
	assert.Empty(t, w.notesOf(NNoReaderForTier))
}

// TestTheTierJudgmentClosesWhenAReaderServesIt pins the exit by fact: in the
// tick a reader row serving the tier appears, the judgment closes and the cards
// are asked (tla/WaitFor.tla ReaderFrees; docs/SPEC-SPRINT.md section 6).
func TestTheTierJudgmentClosesWhenAReaderServesIt(t *testing.T) {
	t.Parallel()
	w, ids := noReaderTierWorld(t)
	plan, _ := TickAsk(w.s, TickReq{})
	w.must(plan)
	require.Len(t, w.notesOf(NNoReaderForTier), 1)
	require.Len(t, w.openOn(StreamSubject(TierSubject("heavy"))), 1)

	// two heavy reader rows appear (heavy needs two readers a card)
	w.s.Readers.SetRows(append(w.s.Readers.Rows(), "reader-m3", "reader-m4"))
	w.s.Readers.Texts["reader-m3"] = map[string]string{"tiers": "heavy"}
	w.s.Readers.Texts["reader-m4"] = map[string]string{"tiers": "heavy"}
	w.s.ReaderStates["reader-m3"], w.s.ReaderStates["reader-m4"] = ReaderUp, ReaderUp

	plan, _ = TickAsk(w.s, TickReq{})
	w.must(plan)
	assert.Empty(t, w.openOn(StreamSubject(TierSubject("heavy"))), "a reader row serves the tier: the judgment closes")
	for _, id := range ids {
		assert.Len(t, readsAt(w.s, w.s.Work.Card(id), 1), 2, "%s is asked both reads in that tick", id)
	}
}
