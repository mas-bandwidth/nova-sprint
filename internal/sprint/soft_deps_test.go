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
//   - When B reaches merging before A has landed, the merge step skips B ("soft
//     dependency on a not landed") while any other ready-to-land card in the
//     stream lands; once A is in merging too, the batch orders [a, b] so A lands
//     before B in the same pass.
//   - When A is dropped, B's soft dependency on A is released and B lands on its own.
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

	// b's soft dependency a has not landed: MergeStep skips b and lands c.
	w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 10}))
	assert.Equal(t, Landed, w.state("c"), "c has no soft dependency and lands while b waits on a")
	assert.Equal(t, Merging, w.state("b"), "b stays in merging while a is unlanded")

	// With only b in merging and a still working, MergeStep refuses b naming the soft dependency.
	refused := MergeStep(w.s, MergeReq{Stream: "s1", Batch: 10})
	require.NotEmpty(t, refused.Refused, "b alone in merging with a unlanded must be refused")
	assert.Contains(t, refused.Refused[0].Why, "soft dependency on a not landed")
	assert.Equal(t, Merging, w.state("b"))

	// Finish, read and accept a to merging too. Even though b has a lower score than a,
	// a batch of 1 lands a first, and a batch of 10 lands [a, b] in order.
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"a.w1"}}, Gens: gensOf(w.s, "a.w1")}))
	w.must(Ask(w.s, AskReq{}))
	for _, rc := range readsAt(w.s, w.s.Work.Card("a"), 1) {
		w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
	}
	w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"a"}}}))
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
	require.NotEmpty(t, MergeStep(w2.s, MergeReq{Stream: "s1", Batch: 10}).Refused, "blocked while a is on the table")
	w2.must(Drop(w2.s, DropReq{Sel: Sel{IDs: []string{"a"}}, Reason: "no longer needed", Who: "coordinator"}))
	w2.must(MergeStep(w2.s, MergeReq{Stream: "s1", Batch: 10}))
	assert.Equal(t, Landed, w2.state("b"), "dropping a releases b to land on its own")

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
	refused3 := MergeStep(w3.s, MergeReq{Stream: "s1", Batch: 10})
	require.NotEmpty(t, refused3.Refused, "b's soft dependency moved from a to a2")
	assert.Contains(t, refused3.Refused[0].Why, "soft dependency on a2 not landed")

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
