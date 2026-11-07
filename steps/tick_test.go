package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cardTake(row string) card.Event { return card.Take{Row: row} }

// TestTickHappyPath: one producer from waiting to landed, tick by tick,
// every op accepted (conformance, happy).
func TestTickHappyPath(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	p := s.tick()
	require.Len(t, s.tickOps(), 1, "one op: Resolve")
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "working")
	s.requireState("p1.w1", "ready")
	assert.Equal(t, "r1", s.card("p1.w1").Holder, "the idle lane with most room, by name")
	assert.Equal(t, "sprint/s1/p1", s.card("p1.w1").Branch)
	assert.Equal(t, base, s.card("p1.w1").BaseSHA)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "review")
	s.requireState("p1.r1.1", "ready")
	assert.Equal(t, "r2", s.card("p1.r1.1").Holder, "the rank above the writer's, never the author")
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "merging")
	p = s.tick()
	require.Len(t, p.Edges, 1, "a land request")
	req := s.request("land", "s1")
	assert.Equal(t, []string{"p1"}, req.Cards)
	assert.Equal(t, []string{sha(1)}, req.Heads)
	assert.Equal(t, base, req.Base)
	p = s.tick()
	assert.Empty(t, p.Edges, "one land request in flight per stream")
	s.land(req)
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "landed")
	assert.Empty(t, p.Carry)
	s.settle()
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "landed", Landed: req.Heads})
	p = s.tick()
	assert.Empty(t, p.Ops, "a repeated result is consumed with no move")
	assert.Empty(t, p.Carry)
}

// TestTickFixPath: a read says fix; the producer goes back to ready with
// the finding, is dealt again carrying its head, and a second read says yes.
func TestTickFixPath(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "fix", sha(1), "the test is wrong")
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	assert.Equal(t, "the test is wrong", s.card("p1").Finding)
	assert.Equal(t, 1, s.card("p1").Bounces)
	s.tick()
	s.requireState("p1", "working")
	w2 := s.card("p1.w2")
	assert.Equal(t, sha(1), w2.Prior, "the fix continues from the head")
	assert.Equal(t, "the test is wrong", w2.Finding)
	s.take("p1.w2")
	s.report("p1.w2", "ok", sha(2), "fixed")
	s.tick()
	s.take("p1.r2.1")
	s.report("p1.r2.1", "ok", sha(2), "fine")
	s.tick()
	s.requireState("p1", "merging")
}

// TestTickRedAndRetry: a red result sends the producer back to review; a new
// read, yes, and a second land request with an id of its own.
func TestTickRedAndRetry(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.run(2)
	first := s.request("land", "s1")
	s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: "red", Blamed: sha(1)})
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "review")
	assert.True(t, s.card("p1").Red)
	assert.Equal(t, 2, s.card("p1").Review)
	s.settle()
	s.tick()
	s.requireState("p1.r2.1", "ready")
	s.take("p1.r2.1")
	s.report("p1.r2.1", "ok", sha(1), "fine again")
	s.run(2)
	second := s.request("land", "s1")
	assert.NotEqual(t, first.ID, second.ID, "the second landing of one head has its own id")
}

// TestTickFaultSpendsNothing: a fault report returns the producer to ready
// with no bounce, and the next deal carries the pushed head.
func TestTickFaultSpendsNothing(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "fault", "", "exit 137")
	s.requireState("p1", "ready")
	assert.Equal(t, 0, s.card("p1").Bounces)
	s.tick()
	s.requireState("p1.w2", "ready")
}
