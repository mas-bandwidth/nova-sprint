package card

// Emma's four from the second cold read of layer 2, folded from
// /Volumes/nova/ai/rowan/working/handoff/SPRINT-V2-L2-READ2-emma-2026-11-06.md
// (the probes that failed at c3fac873). Each one now expects the ruling.
// Her failure lines are the attribution, not the behaviour.
//
//	1A DEFECT: Relink ignored c.After and created a needs/after cycle
//	1B DEFECT: Relink lacks InThisOp and refuses same-op Cancel+Relink
//	2A DEFECT: a stale read reported fix with an empty head
//	2B DEFECT: a stale read reported ok and overwrote its head
//	3A DEFECT: work Report fail with Head equal to the base was accepted
//	3B DEFECT: work Withdraw with Head equal to the base was accepted
//	3C DEFECT: a ready work consumer accepted Withdraw with a head
//	4A DEFECT: Card.Gen -1 reached the store as ErrInvalid
//	4B DEFECT: HoldStream named one producer twice and the store rejected it
//	4C DEFECT: AskRead of an empty head failed in the store
//	4D DEFECT: two AskReads of one producer could not be one op

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeRead2RelinkAfterCycleAndInThisOp(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"), producer("p2"), producer("p3", "p1"))
	w.raw("p2-after-p3", store.SetCardAfter{Card: "p2", After: []string{"p3"}})
	w.move("p1", Cancel{Reason: "dropped"})
	all, _ := w.all()
	_, r := Move(all["p3"], nil, w.rows(), StreamFromStore(w.snap().Streams["s1"]), all, Relink{
		Need: "p1", To: "p2", Reason: "relink p3 to p2 even though p2 is after p3",
	})
	refusal{r, "p3 needs p2 needs p3"}.check(t)
	assert.Equal(t, []string{"p1"}, w.card("p3").Needs)
	assert.Equal(t, []string{"p3"}, w.card("p2").After)

	s := Stream{Name: "s1"}
	q1 := Card{ID: "q1", Kind: "producer", Stream: "s1", State: Ready, Tier: "pro", Priority: "normal", Brief: "do q1", Gen: 1}
	q2 := Card{ID: "q2", Kind: "producer", Stream: "s1", State: Ready, Tier: "pro", Priority: "normal", Brief: "do q2", Gen: 1}
	q3 := Card{ID: "q3", Kind: "producer", Stream: "s1", State: Waiting, Tier: "pro", Priority: "normal", Needs: []string{"q1"}, Brief: "do q3", Gen: 1}
	cards := map[string]Card{"q1": q1, "q2": q2, "q3": q3}
	_, r = Move(q3, nil, nil, s, cards, Relink{Need: "q1", To: "q2", Reason: "relinked in same op"})
	refusal{r, "need q1 of producer q3 is ready, not fail"}.check(t)
	plan, r := Move(q3, nil, nil, s, cards, Relink{Need: "q1", To: "q2", Reason: "relinked in same op", InThisOp: true})
	require.Nil(t, r)
	assert.Contains(t, plan.Writes, store.Write(store.SetCardNeeds{Card: "q3", Needs: []string{"q2"}}))
}

func TestProbeRead2ReadConsumerHeadBypass(t *testing.T) {
	t.Parallel()
	p := Card{
		ID: "p1", Kind: "producer", Stream: "s1", State: Review, Tier: "pro", Priority: "normal",
		Review: 1, BaseSHA: base, Head: h2, Author: "r1", Brief: "do p1", Gen: 5,
	}
	c := Card{
		ID: "p1.r2.1", Kind: "read", Stream: "s1", Of: "p1", Holder: "r2",
		State: Working, Tier: "pro", Review: 1, BaseSHA: base, Head: h1, Author: "r1",
		Brief: "do p1", Gen: 2,
	}
	r2 := rows()["r2"]
	_, r := MoveConsumer(c, p, r2, Report{Row: "r2", Result: "fix", Reason: "stale finding on h1", Cost: 1, Priced: true})
	refusal{r, "read p1.r2.1 was cut at " + h1 + "; producer p1 is at " + h2}.check(t)
	_, r = MoveConsumer(c, p, r2, Report{Row: "r2", Result: "ok", Reason: "lgtm", Head: h2, Cost: 1, Priced: true})
	refusal{r, "read p1.r2.1 was cut at " + h1 + ", the report names " + h2}.check(t)
}

func TestProbeRead2WorkHeadAndWithdrawGuards(t *testing.T) {
	t.Parallel()
	p := Card{
		ID: "p1", Kind: "producer", Stream: "s1", State: Working, Tier: "pro", Priority: "normal",
		BaseSHA: base, Head: h1, Author: "r1", Attempt: 2, Brief: "do p1", Gen: 3,
	}
	c := Card{
		ID: "p1.w2", Kind: "work", Stream: "s1", Of: "p1", Holder: "r1",
		State: Working, Tier: "pro", Attempt: 2, BaseSHA: base, PriorHead: h1, Brief: "do p1", Gen: 2,
	}
	r1 := rows()["r1"]
	_, r := MoveConsumer(c, p, r1, Report{Row: "r1", Result: "fail", Reason: "tests failed", Head: base, Cost: 1, Priced: true})
	refusal{r, "head " + base + " is the base; nothing was done"}.check(t)
	_, r = MoveConsumer(c, p, r1, Withdraw{Reason: "preempted", Head: base, Stream: Stream{Name: "s1"}, Preempt: true})
	refusal{r, "head " + base + " is the base; nothing was done"}.check(t)
	c.State = Ready
	_, r = MoveConsumer(c, p, r1, Withdraw{Reason: "cancelled before take", Head: h2, Stream: Stream{Name: "s1"}})
	refusal{r, "consumer p1.w2 is ready; it has not pushed a head"}.check(t)
}

func TestProbeRead2ValueRulesAndBatchAskRead(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"), producer("p2"))
	w.move("p1", Resolve{})
	w.move("p2", Resolve{})

	pNeg := Card{
		ID: "p1", Kind: "producer", Stream: "s1", State: Ready, Tier: "pro", Priority: "normal",
		Brief: "do p1", Gen: -1,
	}
	_, r := Move(pNeg, nil, nil, Stream{Name: "s1", Held: true}, nil, Park{Reason: "held"})
	refusal{r, "gen -1 is outside the range 0..2147483647"}.check(t)

	p1 := w.card("p1")
	_, r = MoveStream(Stream{Name: "s1"}, HoldStream{Reason: "incident", Ready: []Card{p1, p1}, Seq: w.snap().Seq})
	refusal{r, "producer p1 is listed twice in HoldStream; name each ready producer once"}.check(t)
	assert.False(t, w.snap().Streams["s1"].Held)

	w.raw("p2-review-empty-head",
		store.SetCardState{Card: "p2", State: Review},
		store.SetCardAuthor{Card: "p2", Author: "r1"},
		store.SetCardReview{Card: "p2"},
	)
	_, r = w.plan("p2", AskRead{Row: "r2"})
	refusal{r, "head is empty; it needs a full commit sha"}.check(t)
	_, ok := w.snap().Cards["p2.r1.1"]
	assert.False(t, ok)

	w.move("p1", Start{Row: "r1", Branch: "s1/p1", Base: base})
	w.consume("p1.w1", Report{Row: "r1", Result: "ok", Head: h1})
	w.move("p1", Finished{Consumer: "p1.w1"})
	a, r := w.plan("p1", AskRead{Row: "r2"})
	require.Nil(t, r)
	b, r := w.plan("p1", AskRead{Row: "r3"})
	require.Nil(t, r)
	_, r = Merge(a, b)
	refusal{r, "merge of AskRead p1 and AskRead p1 refused: a second write to card p1.r1.1 field kind; one op writes a field once; several reads of one head are AskReads"}.check(t)
	plan, r := w.plan("p1", AskReads{Rows: []string{"r2", "r3"}})
	require.Nil(t, r)
	w.op("askreads", plan)
	assert.Equal(t, Working, w.card("p1.r1.1").State)
	assert.Equal(t, Working, w.card("p1.r1.2").State)
}
