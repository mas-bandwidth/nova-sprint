package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSoftDependencyLandsInOrderWithoutBlockingTheDeal pins the soft dependency
// contract (tla/SoftDependency.tla: SoftDoesNotBlockDeal, SoftLandsAfter,
// DropReleasesSoft):
//   - Cards A and B admitted in the same stream where B has LAND-AFTER: A and no
//     hard need are both placed in ready (neither is waiting), and both can be
//     dealt, taken, finished, read and accepted to merging in either order.
//   - A soft dependency orders only among cards queued to merge: when B reaches
//     merging while A is still working, B lands with the rest of the stream and A
//     re-merges onto it later (SoftIsOrderOnly); when A and B are both in merging,
//     the batch orders [a, b] so A lands before B, whatever their rank.
//   - When A is dropped, B's soft dependency on A is released.
//   - When A is replaced/relinked to A2, B's soft dependency moves to A2.
func TestSoftDependencyLandsInOrderWithoutBlockingTheDeal(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))

	briefA := "RESULT: a sha=000000000000 tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: main\nPATHS: internal/a.go\n\nTask A."
	briefB := "RESULT: b sha=000000000000 tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: main\nLAND-AFTER: a\nPATHS: internal/b.go\n\nTask B."
	briefC := "RESULT: c sha=000000000000 tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: main\nPATHS: internal/c.go\n\nTask C."

	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "a", Brief: briefA},
		{ID: "b", Brief: briefB},
		{ID: "c", Brief: briefC},
	}}))
	require.Equal(t, Ready, w.state("a"), "a is ready")
	require.Equal(t, Ready, w.state("b"), "b with LAND-AFTER: a is ready, never waiting")
	require.Equal(t, Ready, w.state("c"), "c is ready")

	// Rank b first so its score is ahead of a and c in the queue.
	w.must(Rank(w.s, RankReq{IDs: []string{"b"}, First: true, Who: "coordinator"}))

	// Deal and take all three; finish, read and accept b and c while a is still working.
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 3}}))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{Limit: 10}}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"b.w1", "c.w1"}}, Gens: gensOf(w.s, "b.w1", "c.w1")}))
	w.must(Ask(w.s, AskReq{}))
	for _, id := range []string{"b", "c"} {
		for _, rc := range readsAt(w.s, w.s.Work.Card(id), 1) {
			w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
		}
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
	}
	require.Equal(t, Working, w.state("a"))
	require.Equal(t, Merging, w.state("b"))
	require.Equal(t, Merging, w.state("c"))

	// a is working, not queued to merge: b's soft dependency orders nothing this batch,
	// and MergeStep lands b with c (2026-10-16: four streams refused whole while the
	// LAND-AFTER target was in review).
	w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 10}))
	assert.Equal(t, Landed, w.state("c"), "c has no soft dependency and lands")
	assert.Equal(t, Landed, w.state("b"), "b lands: its soft dependency a is not in merging")
	assert.Equal(t, Working, w.state("a"), "a re-merges onto b later")

	// With a and b both in merging, b ranked first, a batch of 1 lands a first, and the
	// next lands b.
	w = newWorld(t, "reader-a", "reader-b")
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "a", Brief: briefA}, {ID: "b", Brief: briefB}}}))
	w.must(Rank(w.s, RankReq{IDs: []string{"b"}, First: true, Who: "coordinator"}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 2}}))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{Limit: 10}}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"a.w1", "b.w1"}}, Gens: gensOf(w.s, "a.w1", "b.w1")}))
	w.must(Ask(w.s, AskReq{}))
	for _, id := range []string{"a", "b"} {
		for _, rc := range readsAt(w.s, w.s.Work.Card(id), 1) {
			w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
		}
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
	}
	require.Equal(t, Merging, w.state("a"))
	require.Equal(t, Merging, w.state("b"))

	// Check that a single-card batch picks a before b even though b was ranked first.
	firstBatch := w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 1}))
	require.NotEmpty(t, firstBatch.Units)
	assert.Equal(t, "a", firstBatch.Units[0].Key, "a must be ordered before b in the batch")
	assert.Equal(t, Landed, w.state("a"))
	assert.Equal(t, Merging, w.state("b"))

	// When b conflicts at the base tip after a lands, its head is refused (returned and reworked at the tip), then lands after a.
	w.must(Return(w.s, ReturnReq{Sel: Sel{IDs: []string{"b"}}, Reason: "conflict at tip after a", Who: "coordinator"}))
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"b"}}, Fix: "conflict at tip after a", Who: "coordinator"}))
	assert.Equal(t, 2, w.s.Work.Card("b").Int("attempt"))
	if w.state("b") == Ready {
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"b"}}}))
	}
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{Limit: 10}}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"b.w2"}}, Gens: gensOf(w.s, "b.w2")}))
	w.must(Ask(w.s, AskReq{}))
	for _, rc := range readsAt(w.s, w.s.Work.Card("b"), 2) {
		w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
	}
	w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"b"}}}))
	w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 10}))
	assert.Equal(t, Landed, w.state("b"))

	// Dropping A releases B's soft dependency so B lands on its own.
	w2 := newWorld(t, "reader-a", "reader-b")
	w2.readersTable()
	w2.must(FleetStep(w2.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w2.must(Add(w2.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "a", Brief: briefA},
		{ID: "b", Brief: briefB},
	}}))
	w2.must(Deal(w2.s, DealReq{Sel: Sel{IDs: []string{"b"}}}))
	w2.must(Take(w2.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w2.must(Finish(w2.s, FinishReq{Sel: Sel{IDs: []string{"b.w1"}}, Gens: gensOf(w2.s, "b.w1")}))
	w2.must(Ask(w2.s, AskReq{}))
	for _, rc := range readsAt(w2.s, w2.s.Work.Card("b"), 1) {
		w2.must(Read(w2.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
	}
	w2.must(Accept(w2.s, AcceptReq{Sel: Sel{IDs: []string{"b"}}}))
	w2.must(Drop(w2.s, DropReq{Sel: Sel{IDs: []string{"a"}}, Reason: "no longer needed", Who: "coordinator"}))
	assert.Empty(t, SoftDepsOf(w2.s, w2.s.Work.Card("b")), "dropping a releases b's soft dependency")
	w2.must(MergeStep(w2.s, MergeReq{Stream: "s1", Batch: 10}))
	assert.Equal(t, Landed, w2.state("b"), "b lands on its own")

	// Replacing/relinking A with A2 moves B's soft dependency to A2.
	w3 := newWorld(t, "reader-a", "reader-b")
	w3.readersTable()
	w3.must(FleetStep(w3.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w3.must(Add(w3.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "a", Brief: briefA},
		{ID: "b", Brief: briefB},
	}}))
	w3.must(Deal(w3.s, DealReq{Sel: Sel{IDs: []string{"b"}}}))
	w3.must(Take(w3.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w3.must(Finish(w3.s, FinishReq{Sel: Sel{IDs: []string{"b.w1"}}, Gens: gensOf(w3.s, "b.w1")}))
	w3.must(Ask(w3.s, AskReq{}))
	for _, rc := range readsAt(w3.s, w3.s.Work.Card("b"), 1) {
		w3.must(Read(w3.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
	}
	w3.must(Accept(w3.s, AcceptReq{Sel: Sel{IDs: []string{"b"}}}))
	w3.must(Replace(w3.s, AddReq{Stream: "s1", IDs: []string{"a2"}, Replaces: []string{"a"}, Brief: briefA, Who: "coordinator"}))
	assert.Equal(t, []string{"a2"}, SoftDepsOf(w3.s, w3.s.Work.Card("b")), "b's soft dependency moved from a to a2")
	w3.must(MergeStep(w3.s, MergeReq{Stream: "s1", Batch: 10}))
	assert.Equal(t, Landed, w3.state("b"), "a2 is not in merging: b lands")

	// DEEPEN: Four-card overlap chain on main.go and main_test.go (c1 <- c2 <- c3 <- c4)
	// as soft dependencies with width 4 vs as hard needs with width 4.
	softDealRounds, reworksAtTip, landedOrder := runFourCardSoftChain(t)
	hardDealRounds := runFourCardHardChain(t)
	t.Logf("four-card overlap chain (main.go, main_test.go): soft_deps deal_rounds=%d (all 4 working in tick 1, reworks_at_tip=%d, landed=%v) vs hard_needs deal_rounds=%d",
		softDealRounds, reworksAtTip, landedOrder, hardDealRounds)
	assert.Equal(t, 1, softDealRounds, "soft dependency chain of 4 deals all 4 cards in 1 round")
	assert.Equal(t, 3, reworksAtTip, "each of the 3 later cards in the chain costs at most 1 rework at the tip on conflict")
	assert.Equal(t, []string{"c1", "c2", "c3", "c4"}, landedOrder, "lands in chain order")
	assert.Equal(t, 4, hardDealRounds, "hard needs chain of 4 takes 4 serial deal rounds before c4 is dealt")
}

func runFourCardSoftChain(t *testing.T) (dealRounds int, reworks int, landed []string) {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.readersTable()
	members := []string{"m1", "m2", "m3", "m4"}
	for _, m := range members {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m}))
	}
	mkBrief := func(id, landAfter string) string {
		la := ""
		if landAfter != "" {
			la = "LAND-AFTER: " + landAfter + "\n"
		}
		return "RESULT: " + id + " sha=000000000000 tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: main\n" + la + "PATHS: main.go, main_test.go\n\nTask " + id + "."
	}
	ids := []string{"c1", "c2", "c3", "c4"}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "c1", Brief: mkBrief("c1", "")},
		{ID: "c2", Brief: mkBrief("c2", "c1")},
		{ID: "c3", Brief: mkBrief("c3", "c2")},
		{ID: "c4", Brief: mkBrief("c4", "c3")},
	}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	dealRounds = 1
	for i, m := range members {
		w.must(Take(w.s, TakeReq{As: m, Sel: Sel{Limit: 1}}))
		require.Equal(t, Working, w.state(ids[i]), "%s must be working in round 1", ids[i])
	}
	for _, id := range ids {
		wc := id + ".w1"
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc)}))
		w.must(Ask(w.s, AskReq{}))
		for _, rc := range readsAt(w.s, w.s.Work.Card(id), 1) {
			w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
		}
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
	}
	for i, id := range ids {
		if i > 0 {
			// Earlier card landed and moved the tip; id conflicts on main.go and is reworked once at the tip.
			w.must(Return(w.s, ReturnReq{Sel: Sel{IDs: []string{id}}, Reason: "conflict on main.go at tip", Who: "coordinator"}))
			w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{id}}, Fix: "conflict on main.go at tip", Who: "coordinator"}))
			reworks++
			if w.state(id) == Ready {
				w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{id}}}))
			}
			for _, m := range members {
				w.must(Take(w.s, TakeReq{As: m, Sel: Sel{Limit: 10}}))
			}
			wc := id + ".w2"
			w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc)}))
			w.must(Ask(w.s, AskReq{}))
			for _, rc := range readsAt(w.s, w.s.Work.Card(id), 2) {
				w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
			}
			w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
		}
		st := w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 1}))
		require.Equal(t, id, st.Units[0].Key)
		landed = append(landed, id)
	}
	return dealRounds, reworks, landed
}

func runFourCardHardChain(t *testing.T) (dealRounds int) {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.readersTable()
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m}))
	}
	mkBrief := func(id string) string {
		return "RESULT: " + id + " sha=000000000000 tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: main\nPATHS: main.go, main_test.go\n\nTask " + id + "."
	}
	ids := []string{"c1", "c2", "c3", "c4"}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "c1", Brief: mkBrief("c1")},
		{ID: "c2", Brief: mkBrief("c2"), Needs: []string{"c1"}},
		{ID: "c3", Brief: mkBrief("c3"), Needs: []string{"c2"}},
		{ID: "c4", Brief: mkBrief("c4"), Needs: []string{"c3"}},
	}}))
	for _, id := range ids {
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
		dealRounds++
		for _, m := range []string{"m1", "m2", "m3", "m4"} {
			w.must(Take(w.s, TakeReq{As: m, Sel: Sel{Limit: 10}}))
		}
		wc := id + ".w1"
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc)}))
		w.must(Ask(w.s, AskReq{}))
		for _, rc := range readsAt(w.s, w.s.Work.Card(id), 1) {
			w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
		}
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
		w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 1}))
		w.must(Resolve(w.s, ResolveReq{}))
	}
	return dealRounds
}

// TestSoftDependencyOnAHeldQueuedCardSkipsOnlyItsDependent pins that a card that
// cannot be placed after its queued soft dependency is skipped for this batch and
// named with its reason, and every other queued card lands
// (tla/SoftDependency.tla HeldNeverEmptiesBatch): a is in merging but held (a need
// on d, still working, stands in for whatever holds it), b has LAND-AFTER: a, and c
// is free.
func TestSoftDependencyOnAHeldQueuedCardSkipsOnlyItsDependent(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	brief := func(id, after string) string {
		la := ""
		if after != "" {
			la = "LAND-AFTER: " + after + "\n"
		}
		return "RESULT: " + id + " sha=000000000000 tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: main\n" + la + "PATHS: internal/" + id + ".go\n\nTask " + id + "."
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "a", Brief: brief("a", "")}, {ID: "b", Brief: brief("b", "a")}, {ID: "c", Brief: brief("c", "")}, {ID: "d", Brief: brief("d", "")},
	}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"a.w1", "b.w1", "c.w1"}}, Gens: gensOf(w.s, "a.w1", "b.w1", "c.w1")}))
	w.must(Ask(w.s, AskReq{}))
	for _, id := range []string{"a", "b", "c"} {
		for _, rc := range readsAt(w.s, w.s.Work.Card(id), 1) {
			w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
		}
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
	}
	require.Equal(t, Working, w.state("d"))
	w.s.Work.Card("a").Fields["needs"] = "d" // the fake hold: a cannot be placed this batch

	ordered, why := MergeSoftOrder(w.s, MergePriorityOrder(w.s, w.s.Merge.Cell("s1", Queued)))
	var ids []string
	for _, c := range ordered {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"c"}, ids, "c is placed; a is held and b cannot be placed after it")
	assert.Equal(t, "b: soft dependency on a not landed; b is skipped this batch", why, "the skipped card is named with its reason")

	w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 10}))
	assert.Equal(t, Landed, w.state("c"), "the batch lands every other queued card")
	assert.Equal(t, Merging, w.state("a"))
	assert.Equal(t, Merging, w.state("b"))
}
