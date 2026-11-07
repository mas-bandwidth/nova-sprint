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

// TestTickRedAndRetry: a red result sends the producer to fix with the CI
// finding (intent rule 30); the fix attempt carries it, then a new read,
// yes, and a second land request with an id of its own.
func TestTickRedAndRetry(t *testing.T) {
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
	s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: "red", Blamed: sha(1), Reason: "unit (linux) https://ci/1"})
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	assert.True(t, s.card("p1").Red)
	assert.Equal(t, 2, s.card("p1").Review)
	finding := "CI red at " + sha(1) + " on " + first.Base + ": unit (linux) https://ci/1"
	assert.Equal(t, finding, s.card("p1").Finding)
	s.run(2)
	s.requireState("p1.w2", "ready")
	assert.Equal(t, finding, s.card("p1.w2").Finding, "the fix attempt carries the CI finding")
	s.take("p1.w2")
	s.report("p1.w2", "ok", sha(2), "fixed")
	s.tick()
	read := ""
	for id, c := range s.snap().Cards {
		if c.Kind == "read" && c.Of == "p1" && c.Review == 3 {
			read = id
		}
	}
	require.NotEmpty(t, read, "a read of the fix's review")
	s.take(read)
	s.report(read, "ok", sha(2), "fine again")
	s.run(2)
	second := s.request("land", "s1")
	assert.NotEqual(t, first.ID, second.ID, "the second landing of one head has its own id")
}

// TestTickNoCIJudges: a landing whose group sha never got a check (defect
// 40) opens one no-ci judgement naming the branch and the fix; the producer
// stays merging, nothing lands, and the request is asked again.
func TestTickNoCIJudges(t *testing.T) {
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
	s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: "no-ci", Reason: "no check was reported on " + sha(9) + " within 10m0s of its push"})
	s.run(2)
	require.Empty(t, s.rejected)
	s.requireState("p1", "merging")
	j := s.snap().Judgements["no-ci.s1.1"]
	assert.Equal(t, "open", j.State)
	assert.Contains(t, j.Question, "sprint/s1/group")
	assert.Contains(t, j.Question, "push to sprint/**")
	assert.Equal(t, first.ID, s.request("land", "s1").ID, "asked again under the one judgement")
	s.arrive(InboxItem{Kind: "land", Request: s.request("land", "s1").ID, Outcome: "no-ci"})
	s.run(2) // consumed with no op: the run stops after that tick
	s.run(2) // and the next tick asks again
	_, second := s.snap().Judgements["no-ci.s1.2"]
	assert.False(t, second, "one judgement while it is open")
	// a forge with no token (defect 39) is its own judgement, the fix in it
	s.arrive(InboxItem{Kind: "land", Request: s.request("land", "s1").ID, Outcome: "forge", Reason: "the forge has no GitHub token for sprint: run it under nova-secrets exec --only GH_GATE_TOKEN"})
	s.run(2)
	s.requireState("p1", "merging")
	assert.Contains(t, s.snap().Judgements["forge.s1.1"].Question, "nova-secrets exec --only GH_GATE_TOKEN")
}

// TestTickFaultSpendsNothing: a fault report returns the producer to ready
// with no bounce, and the next deal carries the pushed head.
func TestTickFaultSpendsNothing(t *testing.T) {
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
