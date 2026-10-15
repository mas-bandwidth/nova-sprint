package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// missingAttempt2World is the live state of 2026-10-15 (cards vg-stone-shape-research-a, j16495,
// and virtualgo-reconstruction-a, j16496): s1-1 in review at attempt 2, its one attempt-2 read
// (friend amy's) closed broken on a branch origin does not hold while no branch of the work
// holds the head, so the read is retired branch-missing and NReadBranchMissing is open on the
// primary. Origin's branches are a fixture (AskReq.Holds). tla/ReadBranchMissing.tla.
func missingAttempt2World(t *testing.T) (*world, []FriendSeat, Open) {
	t.Helper()
	w := readCardsWorld(t, 4, "m1")
	putReview(w, "s1-1", "s1-1: work (s1)\n", 2, 1, "head-s1-1")
	putAttemptWork(w, "s1-1", 2, "sprint/s1-1.g2.e0", "head-s1-1", "yes")
	w.s.Fleet.Card(WorkCardID("s1-1", 2)).Fields["member"] = "m1"
	seats := []FriendSeat{readerSeat("amy", 2, []string{"flash"}, []string{"reader"})}
	w.s.Friends = seats
	dealReads(t, w, seats)
	reads := readCardsOf(w, "s1-1")
	require.Len(t, reads, 1, "the fixture: one attempt-2 read, amy's")
	w.must(FriendReadCloseChecked(w.s, "amy", "s1-1", reads[0].ID, missingReport,
		map[string]MissingBranch{reads[0].ID: {Named: "sprint/s1-1.g2.e0"}}))
	require.Equal(t, RetiredByMissingBranch, w.s.Fleet.Card(reads[0].ID).F("retired_by"))
	js := w.openOn("s1-1")
	require.Len(t, js, 1, "the fixture: the judgment is open")
	require.Equal(t, NReadBranchMissing, js[0].Note.Type)
	require.Equal(t, 2, js[0].Note.Attempt)
	require.True(t, readBranchMissingOpen(w.s, w.s.Work.Card("s1-1")))
	return w, seats, js[0]
}

// A broken read on a branch origin does not hold, with no branch of the work holding the head,
// raises a judgment whose decisions are all verbs that resolve it (tla/ReadBranchMissing.tla,
// OfferedDecisionResolves; the live refusals of j16495 were rework "resolves no obligation",
// ask --another "not asked yet at attempt 2", ask "asked already"):
//   - rework --answers closes it;
//   - ask re-asks the read on the branch origin holds the head on, closing it in the same step;
//   - ask is refused naming the judgment and the verbs that resolve it when no branch does;
//   - ask and ask --another give the one reason (AskRefusalsAgree).
func TestAMissingReadBranchJudgmentHasAVerbThatResolvesIt(t *testing.T) {
	t.Parallel()
	coord := func(w *world) string { return w.s.Coordinator }

	t.Run("rework answers it", func(t *testing.T) {
		t.Parallel()
		w, _, j := missingAttempt2World(t)
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "push the branch the work names", Answers: []string{j.Note.ID}, Who: coord(w)}))
		require.Empty(t, w.openOn("s1-1"), "rework closed the judgment")
	})

	t.Run("ask re-asks the read on the branch origin holds the head on", func(t *testing.T) {
		t.Parallel()
		w, _, j := missingAttempt2World(t)
		holds := "sprint/s1-1.g3.e0"
		p := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Answers: []string{j.Note.ID}, Who: coord(w), Holds: map[string]string{"s1-1": holds}})
		require.Empty(t, p.Refused)
		w.must(p)
		require.Empty(t, w.openOn("s1-1"), "the ask closed the judgment")
		reads := readCardsOf(w, "s1-1")
		require.Len(t, reads, 1, "the read is asked again at the card's attempt")
		require.Equal(t, "2", reads[0].F("attempt"))
		require.Equal(t, holds, reads[0].F("branch"), "on the branch origin holds the head")
		require.Equal(t, holds, w.s.Work.Card("s1-1").F(FieldReadBranch), "and every later read of that head names it")
		require.Equal(t, "head-s1-1", w.s.Work.Card("s1-1").F(FieldReadBranchHead))
		// without --answers too: the ask closes the judgment it resolves
		w2, _, _ := missingAttempt2World(t)
		w2.must(Ask(w2.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: coord(w2), Holds: map[string]string{"s1-1": holds}}))
		require.Empty(t, w2.openOn("s1-1"))
		require.Len(t, readCardsOf(w2, "s1-1"), 1)
	})

	t.Run("ask is refused naming the judgment when no branch holds the head", func(t *testing.T) {
		t.Parallel()
		w, _, j := missingAttempt2World(t)
		for _, req := range []AskReq{
			{Sel: Sel{IDs: []string{"s1-1"}}, Who: coord(w)},
			{Sel: Sel{IDs: []string{"s1-1"}}, Answers: []string{j.Note.ID}, Who: coord(w)},
		} {
			p := Ask(w.s, req)
			require.Len(t, p.Refused, 1, "%+v", p)
			why := p.Refused[0].Why
			require.Contains(t, why, "judgment "+j.Note.ID)
			require.Contains(t, why, "rework", "it names the verbs that resolve it")
			require.Contains(t, why, "drop")
			require.NotContains(t, why, "asked already")
			require.NotContains(t, why, "not asked yet")
			require.False(t, strings.Contains(why, "\n"), "one line")
			require.Empty(t, p.Units)
		}
		require.Len(t, w.openOn("s1-1"), 1, "refused, the judgment stays open")
	})

	t.Run("ask and ask --another refuse from one state", func(t *testing.T) {
		t.Parallel()
		w, _, _ := missingAttempt2World(t)
		plain := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: coord(w)})
		another := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true, Who: coord(w)})
		require.Len(t, plain.Refused, 1)
		require.Len(t, another.Refused, 1)
		require.Equal(t, plain.Refused[0].Why, another.Refused[0].Why, "AskRefusalsAgree")
		// a branch holding the head: the plain ask goes, --another says to ask first
		holds := map[string]string{"s1-1": "sprint/s1-1.g3.e0"}
		plain = Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: coord(w), Holds: holds})
		another = Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true, Who: coord(w), Holds: holds})
		require.Empty(t, plain.Refused)
		require.Len(t, another.Refused, 1)
		require.Contains(t, another.Refused[0].Why, "ask s1-1 asks the read again")
		require.NotContains(t, another.Refused[0].Why, "not asked yet")
	})

	t.Run("ask --another names the judgment when no read of the attempt is visible", func(t *testing.T) {
		t.Parallel()
		// the live refusal "not asked yet at attempt 2" (j16495): askedAtAttempt found no read
		// card of the attempt, which the judgment, not an unasked card, explains
		w, _, j := missingAttempt2World(t)
		for _, rc := range w.s.Fleet.WithPrefix("s1-1.r2.") {
			w.s.Fleet.Drop(rc.ID)
		}
		require.False(t, askedAtAttempt(w.s, w.s.Work.Card("s1-1")), "the fixture: no read of the attempt is visible")
		plain := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: coord(w)})
		another := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true, Who: coord(w)})
		require.Len(t, another.Refused, 1)
		require.Len(t, plain.Refused, 1)
		require.NotContains(t, another.Refused[0].Why, "not asked yet", "the live refusal of ask --another")
		require.Contains(t, another.Refused[0].Why, "judgment "+j.Note.ID)
		require.Equal(t, plain.Refused[0].Why, another.Refused[0].Why, "AskRefusalsAgree")
	})

	t.Run("the no-stall decisions offer only verbs the ask state accepts", func(t *testing.T) {
		t.Parallel()
		w, _, _ := missingAttempt2World(t)
		pr := w.s.Work.Card("s1-1")
		ds := (&held{s: w.s}).decisions(pr)
		require.NotContains(t, ds, "ask --another")
		require.NotContains(t, ds, "ask", "ask is refused while no branch on origin holds the head")
		require.Contains(t, ds, "rework")
		require.Contains(t, ds, "drop")
	})

	t.Run("a legacy readers-table read is asked again too", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
		w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
		w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{Limit: 10}}))
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1")}))
		w.must(Ask(w.s, AskReq{}))
		reads := readsAt(w.s, w.s.Work.Card("s1-1"), 1)
		require.Len(t, reads, 2)
		first := reads[0]
		w.must(Read(w.s, ReadReq{As: first.F("reader"), Sel: Sel{IDs: []string{first.ID}}, Verdict: "broken", Finding: "STEP 2: origin has no branch; push the head before asking",
			Missing: map[string]MissingBranch{first.ID: {Named: "sprint/s1-1.g1.e0"}}}))
		js := w.openOn("s1-1")
		require.Len(t, js, 1)
		require.Equal(t, NReadBranchMissing, js[0].Note.Type)
		p := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: w.s.Coordinator})
		require.Len(t, p.Refused, 1)
		require.Contains(t, p.Refused[0].Why, "judgment "+js[0].Note.ID)
		w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Answers: []string{js[0].Note.ID}, Who: w.s.Coordinator, Holds: map[string]string{"s1-1": "sprint/s1-1.g2.e0"}}))
		require.Empty(t, w.openOn("s1-1"))
		require.Len(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1), 2, "the read is asked again: the pair is out again")
	})
}
