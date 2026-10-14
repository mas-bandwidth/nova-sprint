package store

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/hostload"
)

// A member that drains (its binary was replaced: it takes no new card until its running
// cards end) says so on its beat, and the server holds it (the owner, 2026-10-11 ~03:50Z:
// "Why are cards stuck in ready."; b3 held 2 dealt cards with 15 slots idle). The model is
// tla/NoRoom.tla (NeverDealtToDraining, DealtReturns).

// drainBeat is one beat of member at load 0 that says it drains.
func (h *harness) drainBeat(member string) {
	h.t.Helper()
	zero := 0.0
	_, err := h.st.BeatDraining(h.ctx, member, &zero, hostload.Source{}, nil, "", nil, true)
	require.NoError(h.t, err)
}

// readyWork is the work cards ready on the member's row, by id.
func readyWork(s *sprint.Snapshot, member string) []string {
	var out []string
	for _, c := range s.Fleet.Cell(member, sprint.Ready) {
		if c.F("kind") == "work" {
			out = append(out, c.ID)
		}
	}
	return out
}

// The beat record holds the drain: stamped at its first beat, kept while it drains, cleared by
// a beat that does not say it; the snapshot's no-room word for it is a drain's while it is fresh.
func TestTheBeatHoldsTheDrainAndItsStart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.drainBeat("m1")
	began := h.st.now().UTC().Truncate(time.Second)
	h.mu.Lock()
	h.now = h.now.Add(5 * time.Second)
	h.mu.Unlock()
	h.drainBeat("m1")
	beats, err := h.st.Beats(h.ctx, []string{"m1"})
	require.NoError(t, err)
	assert.True(t, beats["m1"].Draining)
	assert.Equal(t, began, beats["m1"].DrainingSince, "a later drain beat keeps the first one's stamp")
	s := h.table()
	require.NoError(t, h.st.readerStatesInto(h.ctx, s))
	assert.True(t, s.Draining("m1"), "the server holds m1 draining: %q", s.NoRoom["m1"])
	assert.False(t, s.Draining("m2"))

	h.beat() // a beat without the word clears it
	beats, err = h.st.Beats(h.ctx, []string{"m1"})
	require.NoError(t, err)
	assert.False(t, beats["m1"].Draining)
	assert.True(t, beats["m1"].DrainingSince.IsZero())
}

// A member that reports draining is dealt nothing; what it was dealt and has not taken
// returns on the next tick, each return a note to the coordinator, and goes to a member that
// does not drain; the card it runs stays with it; the member beside it is unaffected; and the
// doctor names a drain older than ten minutes with the card it still runs.
func TestADrainingMemberIsDealtNothingAndItsDealtCardsReturn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(8)
	h.startMachine()
	h.machine() // the deal gives both members cards, ready on their rows
	s := h.snap()
	onM1, onM2 := readyWork(s, "m1"), readyWork(s, "m2")
	require.GreaterOrEqual(t, len(onM1), 2, "the deal gave m1 cards")
	require.NotEmpty(t, onM2, "the deal gave m2 cards")
	// m1 takes one: it runs it, then its binary is replaced and it drains
	running := onM1[0]
	h.must(TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{IDs: []string{running}}, Gens: map[string]int{running: s.Fleet.Card(running).Int("gen")}, Who: "m1"}))
	returned := onM1[1:]
	primaries := map[string]string{}
	for _, id := range returned {
		primaries[id] = s.Fleet.Card(id).F("primary")
	}
	h.live = []string{"m2"} // the harness's beats leave m1's own to it
	h.drainBeat("m1")

	h.machine()
	s = h.snap()
	assert.Empty(t, readyWork(s, "m1"), "what m1 was dealt and has not taken is returned")
	assert.Equal(t, sprint.Working, s.Fleet.Card(running).Col, "the card m1 runs stays with it")
	assert.Equal(t, "m1", s.Fleet.Card(running).Row)
	for _, id := range onM2 {
		assert.Equal(t, "m2", s.Fleet.Card(id).Row, "%s: the member that does not drain keeps its cards", id)
	}
	notes := h.notesTo(sprint.NDrainReturned)
	assert.Len(t, notes, len(returned), "each return is a note to the coordinator")
	for _, n := range notes {
		assert.Equal(t, h.st.Actor, n.To)
		assert.Contains(t, n.What, "returned from m1")
	}

	for range 2 {
		h.tick(10 * time.Second)
		h.drainBeat("m1")
		h.machine()
	}
	s = h.snap()
	assert.Empty(t, readyWork(s, "m1"), "a draining member is dealt nothing")
	for id, pr := range primaries {
		wc := s.Fleet.Card(s.Work.Card(pr).F("work"))
		require.NotNil(t, wc, "%s: %s is dealt again", id, pr)
		assert.Equal(t, "m2", wc.Row, "%s: %s is dealt to the member that does not drain", id, pr)
		assert.Equal(t, 0, wc.Int("redeals"), "%s: a card never taken spends no redeal", id)
	}

	// past the doctor's bound the drain is named with the card it still runs
	h.mu.Lock()
	h.now = h.now.Add(sprint.DoctorDrainBound + time.Minute)
	h.mu.Unlock()
	h.beat()
	h.drainBeat("m1")
	dr, err := h.st.DoctorRead(h.ctx)
	require.NoError(t, err)
	checks := sprint.DoctorDraining(sprint.DoctorInput{Snap: dr.Snap, Req: dr.Req, ReaderBeats: dr.ReaderBeats})
	require.Len(t, checks, 1, "%v", checks)
	assert.Equal(t, sprint.DoctorWarn, checks[0].Status)
	assert.Equal(t, "m1", checks[0].Subject)
	assert.Contains(t, checks[0].Evidence, "running=1 "+running)

	// a beat that no longer says it drains: m1 is dealt again
	h.live = []string{"m1", "m2"}
	h.beat()
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s1", Count: 8}))
	h.machine()
	assert.NotEmpty(t, readyWork(h.snap(), "m1"), "a member that drains no more is dealt cards")
}

// flashSetup: m1 and m2 up and n flash primaries in s1, the readers a, b and c away and a
// reader row of machine m1's (reader-m1) up, so that the reads are reader-m1's to take.
func flashSetup(t *testing.T, h *harness, n int) {
	t.Helper()
	require.NoError(t, h.m.RowsAdd(h.ctx, "t-readers", []string{"reader-m1"}))
	require.NoError(t, h.st.BeatReaders(h.ctx))
	for _, rd := range []string{"reader-a", "reader-b", "reader-c"} {
		require.NoError(t, h.st.SetReaderAway(h.ctx, rd, true, "tester"))
	}
	for _, m := range []string{"m1", "m2"} {
		h.m.SetMachineWidth(m, strconv.Itoa(testWidth))
		h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: m}))
	}
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: n}))
	h.startMachine()
}

// readsOn is the read cards asked or reading on the reader's row.
func readsOn(s *sprint.Snapshot, reader string) []*sprint.Card {
	return append(append([]*sprint.Card{}, s.Readers.Cell(reader, sprint.Asked)...), s.Readers.Cell(reader, sprint.Reading)...)
}

// The owner, 2026-10-11: "held machines don't take work rowan." A reader row belongs to its
// machine (reader-<m>, reader-<m>-<n>): while the machine is held the ask asks that reader
// nothing, and once it is released the asks resume.
func TestAHeldMachinesReaderIsAskedNothingUntilItIsReleased(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.readersTable() // explicit legacy reader-table fixture
	flashSetup(t, h, 2)
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "tester"}))
	h.machine() // the deal gives m2 the cards
	h.work("m2")
	for range 2 {
		h.tick(10 * time.Second)
		h.machine()
	}
	s := h.snap()
	states, err := h.st.ReaderStates(h.ctx, []string{"reader-m1"}, h.st.now())
	require.NoError(t, err)
	require.Equal(t, sprint.ReaderUp, states["reader-m1"], "the reader itself is up")
	for _, id := range []string{"s1-1", "s1-2"} {
		require.Equal(t, sprint.Review, s.StateOf(id), "%s waits for a read", id)
	}
	assert.Empty(t, readsOn(s, "reader-m1"), "a held machine's reader is asked nothing")

	h.must(FleetStep(sprint.FleetReq{Op: "release", Member: "m1", Who: "tester"}))
	for range 2 {
		h.tick(10 * time.Second)
		h.machine()
	}
	assert.NotEmpty(t, readsOn(h.snap(), "reader-m1"), "released, its reader is asked again")
}

// A reader that drains itself (its queue's beat says so, ReaderBeatDraining) is asked nothing,
// and the reads asked of it and not begun go back on the next tick to another reader, each a
// note to the coordinator; a read begun finishes where it is. This runs the reader's own drain;
// the machine's own drain, and the reader on it, is
// TestAProbedMemberThatDrainsIsDealtNothingAndItsReaderIsAskedNothing.
func TestADrainingReadersAskedReadsReturnToAnotherReader(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.readersTable() // explicit legacy reader-table fixture
	flashSetup(t, h, 2)
	h.machine()
	h.work("m1")
	h.work("m2")
	h.machine() // the ask asks both reads of reader-m1, the one reader up
	s := h.snap()
	asked := readsOn(s, "reader-m1")
	require.Len(t, asked, 2, "both reads asked of reader-m1")
	begun := asked[0]
	h.must(ReadStep(sprint.ReadReq{As: "reader-m1", Begin: true, Sel: sprint.Sel{IDs: []string{begun.ID}}}))

	// reader-a comes up, and reader-m1's binary is replaced: its queue says it drains
	require.NoError(t, h.st.SetReaderAway(h.ctx, "reader-a", false, "tester"))
	wrote, err := h.st.ReaderBeatDraining(h.ctx, "reader-m1", "", true)
	require.NoError(t, err)
	require.True(t, wrote)
	h.machine()
	h.machine()
	s = h.snap()
	on := readsOn(s, "reader-m1")
	require.Len(t, on, 1, "only the read begun stays: %v", on)
	assert.Equal(t, begun.ID, on[0].ID)
	assert.Equal(t, sprint.Reading, on[0].Col)
	moved := readsOn(s, "reader-a")
	require.Len(t, moved, 1, "the read not begun is asked of reader-a")
	assert.NotEqual(t, begun.F("primary"), moved[0].F("primary"))
	notes := h.notesTo(sprint.NDrainReturned)
	require.Len(t, notes, 1, "the return is a note to the coordinator")
	assert.True(t, strings.Contains(notes[0].What, "reader-m1 takes no work"), notes[0].What)
}

// The composition of the night's two drain cards: a member that passed its probe
// (flashSetup up both members with EnvProbePassed) and then drains (its binary was
// replaced) is dealt nothing, and the reader on its machine (reader-m1) is asked
// nothing; the reads waiting for review are asked of another reader once one is up, and
// when the drain clears (a beat that no longer says it) the member is dealt again and its
// reader is asked again. The halves run alone in
// TestADrainingMemberIsDealtNothingAndItsDealtCardsReturn (drains a machine, no reader on
// it) and TestADrainingReadersAskedReadsReturnToAnotherReader (the reader's own drain);
// this runs the composition itself. The model is tla/NoRoom.tla (NeverDealtToDraining,
// DealtReturns, and the ask that asks a draining machine's reader nothing).
func TestAProbedMemberThatDrainsIsDealtNothingAndItsReaderIsAskedNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.readersTable() // explicit legacy reader-table fixture
	flashSetup(t, h, 8)
	// m1 passed its probe and then its binary is replaced: it drains
	h.live = []string{"m2"}
	h.drainBeat("m1")
	h.machine() // the deal gives m2 the cards, none to the member that drains
	h.work("m2")
	for range 2 {
		h.tick(10 * time.Second)
		h.drainBeat("m1")
		h.machine()
	}
	s := h.snap()
	assert.Empty(t, readyWork(s, "m1"), "a probed member that drains is dealt nothing")
	assert.Empty(t, readsOn(s, "reader-m1"), "a draining machine's reader is asked nothing")

	// another reader comes up: the reads waiting for review are asked of it
	require.NoError(t, h.st.SetReaderAway(h.ctx, "reader-a", false, "tester"))
	for range 2 {
		h.tick(10 * time.Second)
		h.drainBeat("m1")
		h.machine()
	}
	assert.NotEmpty(t, readsOn(h.snap(), "reader-a"), "the reads waiting for review are asked of reader-a")

	// the drain clears: m1 is dealt again and its reader is asked again
	h.live = []string{"m1", "m2"}
	h.beat()
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s1", Count: 8}))
	h.machine()
	assert.NotEmpty(t, readyWork(h.snap(), "m1"), "a member that drains no more is dealt cards")
	h.work("m1")
	h.machine()
	assert.NotEmpty(t, readsOn(h.snap(), "reader-m1"), "a reader whose machine drains no more is asked again")
}

// ReaderOwner: reader-<m> and reader-<m>-<n> belong to fleet member m; a friend's reader and a
// reader named for no member belong to none.
func TestAReaderRowBelongsToItsMachine(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(0)
	s := h.snap()
	s.Friends = []sprint.FriendSeat{{Name: "zhi"}}
	for reader, want := range map[string]string{"reader-m1": "m1", "reader-m1-2": "m1", "reader-m2-10": "m2", "reader-zhi": "", "reader-a": "", "reader-m1-x": ""} {
		got, ok := s.ReaderOwner(reader)
		assert.Equal(t, want, got, reader)
		assert.Equal(t, want != "", ok, reader)
	}
}

// The coordinator's verbs that deal outside the tick give a draining member nothing either: a
// hold of another member redeals its cards round the members that do not drain, and a rework
// deals its next attempt to one.
func TestAHoldAndAReworkDealNothingToADrainingMember(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.live = []string{"m1", "m2", "m3"}
	h.beat()
	h.setup(6)
	h.m.SetMachineWidth("m3", strconv.Itoa(testWidth))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m3"}))
	h.startMachine()
	h.machine()
	s := h.snap()
	require.NotEmpty(t, readyWork(s, "m1"), "the deal gave m1 cards")
	onM3 := readyWork(s, "m3")
	// m3's binary is replaced: it drains
	h.live = []string{"m1", "m2"}
	h.drainBeat("m3")

	// the hold of m1 redeals its ready cards: never to m3
	h.must(HoldStep(sprint.HoldReq{Names: []string{"m1"}, Reason: "test", Who: "tester"}))
	s = h.snap()
	assert.Empty(t, readyWork(s, "m1"), "the held member's ready cards are redealt")
	assert.ElementsMatch(t, onM3, readyWork(s, "m3"), "the hold redeals nothing to the member that drains")

	// a rework of a card m2 ran deals its next attempt: never to m3
	h.work("m2")
	var pr string
	for _, c := range h.snap().Work.Column(sprint.Review) {
		pr = c.ID
		break
	}
	require.NotEmpty(t, pr, "a card m2 finished waits in review")
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{pr}}, Fix: "test", Who: "tester"}))
	s = h.snap()
	if wc := s.Fleet.Card(s.Work.Card(pr).F("work")); wc != nil {
		assert.NotEqual(t, "m3", wc.Row, "the rework deals nothing to the member that drains")
	}
	assert.ElementsMatch(t, onM3, readyWork(s, "m3"), "m3 holds only what it held before it drained")
}
