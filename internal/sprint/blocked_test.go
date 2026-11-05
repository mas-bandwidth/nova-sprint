package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The blocked state (blocked.go, tla/Blocked.tla). The clock is the world's, moved by hand;
// every plan goes through Applied, as the store applies it, so the flaps are counted where
// the live store counts them.

// applied applies a step's plan as the store does (Applied) and fails on a refusal.
func applied(w *world, p Plan) Plan {
	w.t.Helper()
	return w.must(Applied(w.s, p))
}

// dealtTaken is a world with s1-1 dealt and taken on its member.
func dealtTaken(t *testing.T) *world {
	t.Helper()
	w := setup(t, 1)
	applied(w, Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	take(w)
	return w
}

func take(w *world) {
	w.t.Helper()
	c := w.s.Fleet.Card(w.s.Work.Card("s1-1").F("work"))
	applied(w, Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
}

// flapOnce is one flap of the live churn (friend_start.go, friendUnstartedWorking): the
// working card goes back ready on its member with no verdict, then is taken again.
func flapOnce(w *world) {
	w.t.Helper()
	w.tick(time.Minute)
	c := w.s.Fleet.Card(w.s.Work.Card("s1-1").F("work"))
	require.Equal(w.t, Working, c.Col)
	var p Plan
	p.on(w.s)
	p.Units = []Unit{{Key: c.ID, Stream: "s1", Changes: []Change{change(Fleet, moveEntry(c, c.Row, Ready, map[string]string{"untaken_since": stamp(w.s.Now)}))}}}
	applied(w, p)
	take(w)
}

func flapsOf(w *world, id string) Flaps {
	return FlapsOf(w.s.Work.Card(id), w.s.Now, w.s.FlapWindow())
}

func blockTick(w *world) Plan {
	w.t.Helper()
	p, _ := TickBlock(w.s, TickReq{})
	return applied(w, p)
}

// blockedWorld is s1-1 blocked from working: limit+1 flaps, then the block part.
func blockedWorld(t *testing.T) *world {
	t.Helper()
	w := dealtTaken(t)
	for i := 0; i <= FlapLimitDefault; i++ {
		flapOnce(w)
	}
	blockTick(w)
	require.Equal(t, Blocked, w.state("s1-1"))
	require.Empty(t, Check(w.s, nil), "the invariants hold on a blocked card")
	return w
}

// At the limit a primary works on; one past it, the block part blocks it, from working,
// withdrawing its work card (its attempt kept) and raising one judgment.
func TestAPrimaryIsBlockedOnePastTheFlapLimit(t *testing.T) {
	t.Parallel()
	w := dealtTaken(t)
	for i := 0; i < FlapLimitDefault; i++ {
		flapOnce(w)
	}
	f := flapsOf(w, "s1-1")
	require.Equal(t, FlapLimitDefault, f.Total, "one flap a bounce: %v", f)
	assert.Equal(t, FlapLimitDefault, f.ByKind[FlapNoVerdict])
	assert.Empty(t, blockTick(w).Units, "at the limit: not blocked")
	assert.Equal(t, Working, w.state("s1-1"))

	flapOnce(w)
	p := blockTick(w)
	require.Len(t, p.Units, 1)
	assert.Contains(t, p.Units[0].Moved, "s1-1 working -> blocked (6 flaps in 30m0s: no-verdict=6)")
	c := w.s.Work.Card("s1-1")
	assert.Equal(t, Blocked, c.Col)
	assert.Equal(t, Working, State(c.F(FieldBlockedFrom)))
	assert.Empty(t, c.F("work"), "the primary no longer names a dealt card")
	wc := w.s.Fleet.Card("s1-1.w1")
	assert.Equal(t, Withdrawn, wc.Col, "its deal is withdrawn on entry")
	assert.Equal(t, "1", wc.F("attempt"), "its attempt kept")
	require.Len(t, w.notesOf(NFlapBlocked), 1)
	n := w.notesOf(NFlapBlocked)[0]
	assert.Equal(t, "s1-1", n.Card)
	subject, body, ok := BlockedPush(n)
	require.True(t, ok)
	assert.Equal(t, "BLOCKED s1-1: 6 flaps in 30m0s", subject)
	assert.Contains(t, body, "flaps: no-verdict=6")
	assert.Contains(t, body, "last: ")
	assert.Contains(t, body, "s1-1.w1 working -> ready (m")

	// a second tick posts no second judgment, and moves nothing
	assert.Empty(t, blockTick(w).Units)
	assert.Len(t, w.notesOf(NFlapBlocked), 1)

	cards := BlockedCards(w.s)
	require.Len(t, cards, 1)
	assert.Equal(t, 6, cards[0].Flaps.Total)
}

// The limit and the window are one setting each (set --flap-limit, --flap-window): a lower
// limit blocks sooner, and a flap older than the window is not counted.
func TestTheFlapLimitAndWindowAreSettings(t *testing.T) {
	t.Parallel()
	w := dealtTaken(t)
	applied(w, WithFlapSettings(Plan{Refused: []Refusal{{Key: "set", Why: "nothing to set"}}}, w.s, "2", "10m"))
	assert.Equal(t, 2, w.s.FlapLimit())
	assert.Equal(t, 10*time.Minute, w.s.FlapWindow())
	assert.NotEmpty(t, WithFlapSettings(Plan{}, w.s, "0", "").Refused, "a limit below 1 is refused")
	assert.NotEmpty(t, WithFlapSettings(Plan{}, w.s, "", "soon").Refused, "a window that is no duration is refused")

	flapOnce(w)
	flapOnce(w)
	assert.Empty(t, blockTick(w).Units, "two flaps at a limit of two")
	w.tick(10 * time.Minute)
	assert.Zero(t, flapsOf(w, "s1-1").Total, "past the window the flaps are not counted")
	flapOnce(w)
	flapOnce(w)
	assert.Empty(t, blockTick(w).Units, "the old flaps fell out of the window")
	flapOnce(w)
	assert.Len(t, blockTick(w).Units, 1, "three inside the window at a limit of two")

	applied(w, WithFlapSettings(Plan{Refused: []Refusal{{Key: "set", Why: "nothing to set"}}}, w.s, ReadTierDefault, ReadTierDefault))
	assert.Equal(t, FlapLimitDefault, w.s.FlapLimit())
	assert.Equal(t, FlapWindowDefault, w.s.FlapWindow())
}

// Nothing moves a blocked primary but unblock and drop, and nothing deals it.
func TestEveryVerbThatWouldMoveABlockedCardIsRefused(t *testing.T) {
	t.Parallel()
	w := blockedWorld(t)
	sel := Sel{IDs: []string{"s1-1"}}
	for name, p := range map[string]Plan{
		"deal":   Deal(w.s, DealReq{Sel: sel}),
		"ask":    Ask(w.s, AskReq{Sel: sel}),
		"rework": Rework(w.s, ReworkReq{Sel: sel, Fix: "x"}),
		"accept": Accept(w.s, AcceptReq{Sel: sel}),
		"merge":  MergeStep(w.s, MergeReq{Stream: "s1", Batch: 10}),
		"recut":  Recut(w.s, RecutReq{ID: "s1-1"}),
		"needs":  NeedsSet(w.s, NeedsSetReq{ID: "s1-1", Clear: true, Who: "coordinator"}),
		"return": Return(w.s, ReturnReq{Sel: sel, Reason: "x"}),
	} {
		p = Applied(w.s, p)
		for _, u := range p.Units {
			for _, c := range u.Changes {
				assert.False(t, c.Table == Work && c.Entry.ID == "s1-1", "%s moved the blocked card: %+v", name, c.Entry)
			}
		}
		assert.Equal(t, Blocked, w.state("s1-1"), name)
	}

	// a move of its withdrawn card back into a member's ready, by any path, is refused
	wc := w.s.Fleet.Card("s1-1.w1")
	var p Plan
	p.on(w.s)
	p.Units = []Unit{{Key: wc.ID, Changes: []Change{change(Fleet, moveEntry(wc, wc.Row, Ready, nil))}}}
	p = Applied(w.s, p)
	assert.Empty(t, p.Units)
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "is blocked")

	// a lawful-move check on a hand-written move out of blocked refuses it
	pr := w.s.Work.Card("s1-1")
	p = Plan{}
	p.on(w.s)
	p.Units = []Unit{{Key: pr.ID, Changes: []Change{change(Work, moveEntry(pr, pr.Row, Ready, nil))}}}
	p = Applied(w.s, p)
	assert.Empty(t, p.Units)
	require.NotEmpty(t, p.Refused)
	assert.Contains(t, p.Refused[0].Why, "is blocked")
}

// Blocked has no timeout and no automatic exit: a day on, the tick's parts move nothing, and
// the judgment stays open. brief may edit it while it is blocked.
func TestABlockedCardSitsUntilTheCoordinatorDecides(t *testing.T) {
	t.Parallel()
	w := blockedWorld(t)
	for i := 0; i < 3; i++ {
		w.tick(24 * time.Hour)
		for _, part := range []func(*Snapshot, TickReq) (Plan, int){TickBlock, TickResolve, TickDeal} {
			p, _ := part(w.s, TickReq{})
			applied(w, p)
		}
		assert.Equal(t, Blocked, w.state("s1-1"))
	}
	assert.Len(t, w.notesOf(NFlapBlocked), 1, "one judgment however long it sits")
	assert.Len(t, openOf(w, NFlapBlocked, "s1-1"), 1)

	applied(w, Brief(w.s, briefOne("s1-1", "the task, written again")))
	assert.Equal(t, Blocked, w.state("s1-1"), "brief edits it and leaves it blocked")
	assert.Equal(t, []string{"unblock", "drop", "brief"}, Decisions[NFlapBlocked])
}

// unblock moves it to waiting with its flaps cleared and its judgment closed; the resolve
// takes it to ready and the deal deals it again.
func TestUnblockGoesToWaitingAndClearsTheCounts(t *testing.T) {
	t.Parallel()
	w := blockedWorld(t)
	assert.NotEmpty(t, Unblock(w.s, UnblockReq{ID: "s1-1", Who: "coordinator"}).Refused, "no reason")
	assert.NotEmpty(t, Unblock(w.s, UnblockReq{ID: "s1-1", Reason: "x", Who: "m1"}).Refused, "not the coordinator")
	assert.NotEmpty(t, Unblock(w.s, UnblockReq{ID: "s1-9", Reason: "x", Who: "coordinator"}).Refused, "no such card")

	p := applied(w, Unblock(w.s, UnblockReq{ID: "s1-1", Reason: "the env is fixed", Who: "coordinator"}))
	assert.Equal(t, "s1-1 blocked -> waiting (unblocked: the env is fixed)", p.Units[0].Moved)
	c := w.s.Work.Card("s1-1")
	assert.Equal(t, Waiting, c.Col, "never straight to ready")
	assert.Empty(t, c.F(FieldFlaps))
	assert.Empty(t, c.F(FieldBlockedAt))
	assert.Equal(t, "the env is fixed", c.F("unblock_reason"))
	assert.Empty(t, openOf(w, NFlapBlocked, "s1-1"), "its judgment is closed")
	assert.NotEmpty(t, Unblock(w.s, UnblockReq{ID: "s1-1", Reason: "x", Who: "coordinator"}).Refused, "not blocked any more")

	resolve, _ := TickResolve(w.s, TickReq{})
	applied(w, resolve)
	require.Equal(t, Ready, w.state("s1-1"))
	applied(w, Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	assert.Equal(t, Working, w.state("s1-1"))
	assert.NotEmpty(t, w.s.Work.Card("s1-1").F("work"), "dealt again")
	assert.Zero(t, flapsOf(w, "s1-1").Total)
}

// drop takes a blocked primary off, as it takes any open card.
func TestDropTakesABlockedCard(t *testing.T) {
	t.Parallel()
	w := blockedWorld(t)
	applied(w, Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-1"}}, Reason: "not worth it"}))
	assert.NotEqual(t, Blocked, w.state("s1-1"))
}

// Forward progress never counts: a finish, two oks, the accept and the landing leave no flap;
// a normal two-wave card (w1 reworked on its read, w2 accepted) ends with one, unblocked.
func TestProgressIsNoFlapAndATwoWaveCardEndsWithOne(t *testing.T) {
	t.Parallel()
	const head = "0123456789012345678901234567890123456789"
	w := dealtTaken(t)
	finish := func() {
		id := w.s.Work.Card("s1-1").F("work")
		if c := w.s.Fleet.Card(id); c.Col == Ready {
			take(w)
		}
		applied(w, Finish(w.s, FinishReq{Sel: Sel{IDs: []string{id}}, Gens: gensOf(w.s, id), Head: head, Report: "done"}))
	}
	read := func(attempt int, verdict string) {
		applied(w, Ask(w.s, AskReq{}))
		for _, r := range readsAt(w.s, w.s.Work.Card("s1-1"), attempt) {
			req := ReadReq{Usage: "input=1000 output=100", As: r.F("reader"), Verdict: verdict, Sel: Sel{IDs: []string{r.ID}}}
			if verdict == "broken" {
				req.Finding = "line 1: off by one"
			}
			applied(w, Read(w.s, req))
			if verdict == "broken" {
				break
			}
		}
	}
	finish()
	read(1, "broken")
	assert.Zero(t, flapsOf(w, "s1-1").Total, "a finish and a read with a verdict are no flap")
	applied(w, Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "off by one"}))
	f := flapsOf(w, "s1-1")
	assert.Equal(t, 1, f.Total, "the rework is one flap: %v", f)
	assert.Equal(t, 1, f.ByKind[FlapRework])
	finish()
	read(2, "ok")
	applied(w, Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Equal(t, Merging, w.state("s1-1"))
	applied(w, MergeStep(w.s, MergeReq{Stream: "s1", Batch: 10}))
	require.Equal(t, Landed, w.state("s1-1"))
	assert.Equal(t, 1, flapsOf(w, "s1-1").Total, "the second wave, the accept and the landing add none")
	assert.Empty(t, w.notesOf(NFlapBlocked))
}
