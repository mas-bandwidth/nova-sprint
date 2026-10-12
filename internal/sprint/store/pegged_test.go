package store

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A member up whose beats say HeavyLoadPercent or more for the pegged bound with no card
// working raises one judgment, member-pegged, to the coordinator, once an episode
// (pegged.go; found 2026-10-12: a member beat at 100% for 13 hours with no card working,
// leftover busy loops of a finished card, and no judgment was raised). On the mem twin,
// the clock injected, no real time: each step of the clock beats the pegged member at
// its load and runs one tick.

// pegStep moves the clock a minute, beats member at pct (every other live member beats at
// 0), and runs one tick.
func (h *harness) pegStep(member string, pct float64) {
	h.t.Helper()
	h.tick(time.Minute)
	h.beatAt(member, pct)
	h.machine()
}

// peggedSince is the member's episode stamp on the fleet table, "" while none.
func (h *harness) peggedSince(member string) string {
	h.t.Helper()
	v, _ := h.snap().Fleet.Prop(sprint.PropPeggedSince(member))
	return v
}

// clearedPegged is the cleared notes of the pegged judgment.
func (h *harness) clearedPegged() []string {
	h.t.Helper()
	notes, _, err := h.m.NotesSince(h.ctx, "", 100000)
	require.NoError(h.t, err)
	var out []string
	for _, n := range notes {
		if n.Kind == sprint.Happened && n.Type == sprint.NAlarmCleared && strings.HasPrefix(n.What, sprint.NMemberPegged+":") {
			out = append(out, n.What)
		}
	}
	return out
}

// peggedSetup is two members up, one card dealt and none working, the machine running, and
// m1 beating only when the test beats it.
func peggedSetup(t *testing.T) *harness {
	h := newHarness(t)
	h.setup(1)
	h.startMachine()
	h.machine()
	h.setLive("m2")
	return h
}

// (1) A member at 95% with no card working for the bound (10m by default) raises one
// judgment, named for the member, its load, how long and since when, offering the hold,
// ack and a wait; the next tick raises no second one.
func TestAMemberPeggedWithNoCardWorkingRaisesOneJudgment(t *testing.T) {
	t.Parallel()
	h := peggedSetup(t)
	require.Zero(t, h.snap().Fleet.Count("m1", sprint.Working), "no card works on m1")
	h.pegStep("m1", 95) // the first tick that sees m1 pegged: the episode begins
	since := h.now
	require.Equal(t, since.UTC().Format(time.RFC3339), h.peggedSince("m1"), "the episode's start is stamped")
	for range 9 {
		h.pegStep("m1", 95)
	}
	assert.Empty(t, h.openOf(sprint.NMemberPegged), "9m pegged is under the bound")
	h.pegStep("m1", 95)
	open := h.openOf(sprint.NMemberPegged)
	require.Len(t, open, 1, "10m pegged with no card working")
	n := open[0].Note
	assert.Equal(t, sprint.Judgment, n.Kind)
	assert.Equal(t, sprint.MemberSubject("m1"), n.Stream)
	assert.Equal(t, "member m1 at 95.0% for 10m0s with no card working: a leftover process or outside load; read it on the machine (ps) and kill by PID or hold the member; pegged since "+since.UTC().Format(time.RFC3339), n.What)
	assert.Equal(t, []string{"fleet down m1", "ack", "wait 15m"}, n.Decisions)
	assert.Len(t, h.commandsOf(sprint.NMemberPegged), 3, "every decision prints its command")
	h.pegStep("m1", 95)
	assert.Len(t, h.openOf(sprint.NMemberPegged), 1, "raised once while it stands")
	assert.Equal(t, 1, h.written(sprint.NMemberPegged), "no second judgment at the next tick")
	h.clean("pegged")
}

// (2) The same load with a card working on the member is work, not an anomaly: no
// judgment and no episode.
func TestAMemberPeggedWithACardWorkingRaisesNone(t *testing.T) {
	t.Parallel()
	h := peggedSetup(t)
	s := h.snap()
	wc := s.Fleet.Card(s.Work.Card("s1-1").F("work"))
	require.NotNil(t, wc)
	m := wc.Row
	h.must(TakeStep(sprint.TakeReq{As: m, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Who: m}))
	require.Equal(t, 1, h.snap().Fleet.Count(m, sprint.Working))
	other := "m1"
	if m == "m1" {
		other = "m2"
	}
	h.setLive(other)
	for range 12 {
		h.pegStep(m, 95)
	}
	assert.Empty(t, h.openOf(sprint.NMemberPegged), "a card works there")
	assert.Zero(t, h.written(sprint.NMemberPegged))
	assert.Empty(t, h.peggedSince(m), "no episode while a card works")
}

// (3) The load falls to 10%: the judgment closes with one cleared note and the episode
// ends; back at 95% for the bound, a second episode raises a second judgment.
func TestASecondPeggedEpisodeRaisesASecondJudgment(t *testing.T) {
	t.Parallel()
	h := peggedSetup(t)
	for range 11 {
		h.pegStep("m1", 95)
	}
	require.Len(t, h.openOf(sprint.NMemberPegged), 1, "the first episode")
	h.pegStep("m1", 10)
	assert.Empty(t, h.openOf(sprint.NMemberPegged), "the load fell under 90%: closed")
	assert.Empty(t, h.peggedSince("m1"), "the episode ended")
	assert.Equal(t, []string{sprint.NMemberPegged + ": m1 beats at 10.0%, under 90%"}, h.clearedPegged())
	for range 10 {
		h.pegStep("m1", 95)
	}
	assert.Empty(t, h.openOf(sprint.NMemberPegged), "9m into the second episode")
	h.pegStep("m1", 95)
	assert.Len(t, h.openOf(sprint.NMemberPegged), 1, "the second episode at its bound")
	assert.Equal(t, 2, h.written(sprint.NMemberPegged), "one judgment per episode, two episodes")
	h.clean("two episodes")
}

// (4) The bound is the sprint's setting, pegged_bound (set --pegged-bound): at 2m the
// judgment is raised at 2m. A value that is no duration above zero is refused.
func TestThePeggedBoundIsTheSprintsSetting(t *testing.T) {
	t.Parallel()
	h := peggedSetup(t)
	setBound := func(v string) Result {
		step := SetStep(sprint.SetReq{Who: h.st.Actor})
		set := step.Plan
		step.Plan = func(s *sprint.Snapshot) sprint.Plan { return sprint.WithPeggedBound(set(s), s, v) }
		return h.run(step)
	}
	bad := setBound("-1m")
	require.Len(t, bad.Refused, 1)
	assert.Contains(t, bad.Refused[0].Why, "--pegged-bound wants a duration above zero")
	assert.Equal(t, sprint.PeggedBoundDefault, h.snap().PeggedBound())
	res := setBound("2m")
	require.Empty(t, res.Refused)
	v, _ := h.snap().Work.Prop(sprint.PropPeggedBound)
	require.Equal(t, "2m", v)
	require.Equal(t, 2*time.Minute, h.snap().PeggedBound())
	h.pegStep("m1", 95) // the episode begins
	h.pegStep("m1", 95)
	assert.Empty(t, h.openOf(sprint.NMemberPegged), "1m into the episode")
	h.pegStep("m1", 95)
	open := h.openOf(sprint.NMemberPegged)
	require.Len(t, open, 1, "raised at 2m")
	assert.Contains(t, open[0].Note.What, "member m1 at 95.0% for 2m0s with no card working")
}

// setPeggedBound is nova-sprint set --pegged-bound v, which must not be refused.
func (h *harness) setPeggedBound(v string) {
	h.t.Helper()
	step := SetStep(sprint.SetReq{Who: h.st.Actor})
	set := step.Plan
	step.Plan = func(s *sprint.Snapshot) sprint.Plan { return sprint.WithPeggedBound(set(s), s, v) }
	h.must(step)
}

// (4b) A bound raised in the middle of an episode whose judgment is open neither closes
// it nor raises it again: only the episode's real ends (the load under 90%, a card
// working, held or down) close it, and no cleared note says under 90% of a member at
// 95%. Found by a reader's probe of #146 at e6c993f8: the raised bound closed the open
// judgment with "m1 beats at 95.0%, under 90%" and raised a second one at the new bound
// in the same uninterrupted episode.
func TestARaisedPeggedBoundMidEpisodeNeitherClosesNorRaisesAgain(t *testing.T) {
	t.Parallel()
	h := peggedSetup(t)
	for range 11 {
		h.pegStep("m1", 95)
	}
	open := h.openOf(sprint.NMemberPegged)
	require.Len(t, open, 1, "the first judgment at 10m")
	first := open[0].Note.ID
	since := h.peggedSince("m1")
	h.setPeggedBound("20m")
	for range 12 { // to 22m into the episode, past the new bound
		h.pegStep("m1", 95)
		open = h.openOf(sprint.NMemberPegged)
		require.Len(t, open, 1, "the episode stands: its judgment stays open")
		require.Equal(t, first, open[0].Note.ID, "the same judgment, never closed and raised again")
	}
	assert.Equal(t, since, h.peggedSince("m1"), "one uninterrupted episode")
	assert.Equal(t, 1, h.written(sprint.NMemberPegged), "no second judgment in one episode")
	assert.Empty(t, h.clearedPegged(), "nothing cleared: the load never fell under 90%")
	// the real end still closes it, and its note names the measured cause
	h.pegStep("m1", 10)
	assert.Empty(t, h.openOf(sprint.NMemberPegged))
	assert.Equal(t, []string{sprint.NMemberPegged + ": m1 beats at 10.0%, under 90%"}, h.clearedPegged())
	h.clean("bound raised mid-episode")
}

// (4c) The mirror: a bound lowered in the middle of an episode whose judgment is open
// raises no second judgment, and closes nothing.
func TestALoweredPeggedBoundMidEpisodeRaisesNoSecondJudgment(t *testing.T) {
	t.Parallel()
	h := peggedSetup(t)
	for range 11 {
		h.pegStep("m1", 95)
	}
	require.Len(t, h.openOf(sprint.NMemberPegged), 1, "the first judgment at 10m")
	h.setPeggedBound("2m")
	for range 5 {
		h.pegStep("m1", 95)
		require.Len(t, h.openOf(sprint.NMemberPegged), 1)
	}
	assert.Equal(t, 1, h.written(sprint.NMemberPegged), "no second judgment in one episode")
	assert.Empty(t, h.clearedPegged())
	h.clean("bound lowered mid-episode")
}

// (5) A held member raises none however it beats, and a held member's open judgment
// closes; a member that stops beating goes down and raises none.
func TestAHeldOrDownMemberRaisesNone(t *testing.T) {
	t.Parallel()
	h := peggedSetup(t)
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "tester"}))
	for range 12 {
		h.pegStep("m1", 95)
	}
	assert.Empty(t, h.openOf(sprint.NMemberPegged), "a held member raises none")
	assert.Empty(t, h.peggedSince("m1"))

	d := peggedSetup(t)
	d.pegStep("m1", 95) // its last beat
	for range 12 {
		d.tick(time.Minute)
		d.machine()
	}
	assert.NotEqual(t, sprint.Up, d.snap().MemberCtl("m1").F("status"), "m1 stopped beating")
	assert.Empty(t, d.openOf(sprint.NMemberPegged), "a down member raises none")
	assert.Zero(t, d.written(sprint.NMemberPegged))
	assert.Empty(t, d.peggedSince("m1"))

	o := peggedSetup(t)
	for range 11 {
		o.pegStep("m1", 95)
	}
	require.Len(t, o.openOf(sprint.NMemberPegged), 1)
	o.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "tester"}))
	o.pegStep("m1", 95)
	assert.Empty(t, o.openOf(sprint.NMemberPegged), "held: the episode ends and its judgment closes")
	assert.Equal(t, []string{sprint.NMemberPegged + ": m1 is held"}, o.clearedPegged())
}
