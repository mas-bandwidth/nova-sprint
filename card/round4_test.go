package card

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Round 4 ruling 6. Start then Hold deals the work and parks the other ready
// producers in the same op. Hold then Start refuses, and the reason says held.
func TestStartThenHoldDealsInTheOp(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"), producer("p2"))
	w.move("p1", Resolve{})
	w.move("p2", Resolve{})
	s := w.snap()
	start, r := w.plan("p1", Start{Row: "r1", Branch: "b", Base: base})
	require.Nil(t, r)
	hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{
		Reason: "freeze", Ready: []Card{w.card("p1"), w.card("p2")}, Seq: s.Seq,
	})
	require.Nil(t, r)
	merged, mr := Merge(start, hold)
	require.Nil(t, mr, "Start then Hold")
	w.op("start-hold", merged)
	assert.Equal(t, []any{true, Working, Waiting, Ready}, []any{w.snap().Streams["s1"].Held, w.card("p1").State, w.card("p2").State, w.card("p1.w1").State})
}

func TestHoldThenStartRefusesHeld(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	s := w.snap()
	hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{
		Reason: "freeze", Ready: []Card{w.card("p1")}, Seq: s.Seq,
	})
	require.Nil(t, r)
	start, r := w.plan("p1", Start{Row: "r1", Branch: "b", Base: base})
	require.Nil(t, r)
	_, mr := Merge(hold, start)
	require.NotNil(t, mr)
	assert.Contains(t, mr.Reason, "held")
}

// A preempt withdraws a working consumer. The youngest on the row is the step's choice.
func TestPreemptWithdrawNeedsWorking(t *testing.T) {
	t.Parallel()
	ready := wk(Ready, "", "")
	_, r := MoveConsumer(ready, pr(Working), rows()["r1"], Withdraw{Reason: "preempted by blocker p2", Stream: Stream{Name: "s1"}, Preempt: true})
	refusal{r, "consumer p1.w1 is ready; a preempt withdraws a working consumer, and the step chooses the youngest on the row"}.check(t)
}

func TestWaiveRedOnReviewZero(t *testing.T) {
	t.Parallel()
	p := pr(Review)
	p.Review = 0
	_, r := Move(p, nil, rows(), Stream{Name: "s1"}, nil, WaiveRed{Reason: "x"})
	refusal{r, "producer p1 has no review yet"}.check(t)
}

// Red is recorded on the producer's red column. WaiveRed accepts while it is
// set and fences the plan on it. Start and Finished clear it.
func TestRedRecorded(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	w.read("p1", "r2", "ok", "")
	w.move("p1", ReadYes{})
	w.move("p1", Red{Head: h1, Reason: "ci red"})
	assert.True(t, w.card("p1").Red)
	plan, r := w.plan("p1", WaiveRed{Reason: "the red was not the code"})
	require.Nil(t, r, "a recorded red may be waived")
	assert.Contains(t, plan.Expect, store.Expect(store.CardRed{Card: "p1", Red: true}))
}

func TestStartAndFinishedClearRed(t *testing.T) {
	t.Parallel()
	p := pr(Ready)
	p.Red = true
	plan, r := Move(p, nil, rows(), Stream{Name: "s1"}, nil, Start{Row: "r1", Branch: "b", Base: base})
	require.Nil(t, r)
	assert.Contains(t, plan.Writes, store.Write(store.SetCardRed{Card: "p1", Red: false}))
	p = pr(Working)
	p.Red = true
	c := wk(Done, "ok", h2)
	plan, r = Move(p, []Card{c}, rows(), Stream{Name: "s1"}, nil, Finished{Consumer: c.ID})
	require.Nil(t, r)
	assert.Contains(t, plan.Writes, store.Write(store.SetCardRed{Card: "p1", Red: false}))
}
