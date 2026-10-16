package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
)

// threeFriendsHoldingJobs is three friends, amy, bob and cyd, each holding one
// working card whose owner's beat names it running, with the machine RUNNING.
func threeFriendsHoldingJobs(t *testing.T) (*harness, []string, []string) {
	t.Helper()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{
		{Name: "amy", Width: 1, Class: "flash"},
		{Name: "bob", Width: 1, Class: "flash"},
		{Name: "cyd", Width: 1, Class: "flash"},
	})
	require.NoError(t, err)
	for _, f := range []string{"amy", "bob", "cyd"} {
		h.up(f)
	}
	brief := func(who string) string {
		return "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend " + who + "\n\nThe task."
	}
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{
		{ID: "s1-1", Brief: brief("amy")},
		{ID: "s1-2", Brief: brief("bob")},
		{ID: "s1-3", Brief: brief("cyd")},
	}}))
	h.startMachine()
	h.machine()
	h.start("amy", 1)
	h.start("bob", 1)
	h.start("cyd", 1)
	var friends, cards []string
	for _, f := range []string{"amy", "bob", "cyd"} {
		working := h.snap().Fleet.Cell(sprint.FriendRow(f), sprint.Working)
		require.Len(t, working, 1, "friend %s holds one working card", f)
		_, err := h.st.FriendBeatReport(h.ctx, f, sprint.FriendReport{Running: []string{working[0].ID}}, nil)
		require.NoError(t, err)
		friends = append(friends, f)
		cards = append(cards, working[0].ID)
	}
	return h, friends, cards
}

// beatOwing is one beat of member, at load 0, with its stop-returns count set
// to stopReturns (fleet beat --stop-returns): the member's word how many
// stop-returns its lanes still owe after the machine's stop.
func (h *harness) beatOwing(member string, stopReturns int) {
	h.t.Helper()
	zero := 0.0
	_, err := h.st.BeatOwing(h.ctx, member, &zero, hostload.Source{}, &stopReturns, "")
	require.NoError(h.t, err)
}

// TestStopDebtSettlesFromTheOwnersBeat is the seat's finding: after a STOP,
// the owners' beats (taken after the STOP) no longer name the jobs, so the
// server settles each lease itself and START passes without the owners' own
// stop-return receipts (tla/StopReturn.tla SettleByBeat).
func TestStopDebtSettlesFromTheOwnersBeat(t *testing.T) {
	t.Parallel()
	h, friends, cards := threeFriendsHoldingJobs(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 3)
	// the owners beat after the STOP, no longer naming their jobs
	h.mu.Lock()
	h.now = h.now.Add(time.Second)
	h.mu.Unlock()
	for _, f := range friends {
		_, err := h.st.FriendBeatReport(h.ctx, f, sprint.FriendReport{}, nil)
		require.NoError(t, err)
	}
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "START settles the leases the owners' beats show gone")
	require.True(t, running.Running())
	require.Empty(t, running.StopDebt)
	for _, id := range cards {
		back := h.snap().Fleet.Card(id)
		require.NotNil(t, back)
		require.Equal(t, sprint.Ready, back.Col, "%s returned to its owner's ready pool", id)
	}
	h.clean("beat-settled stop debt")
}

// TestStopDebtStillNamedInBeatStaysOwed pins the other half of the rule: a
// lease whose job the owner's beat, taken after the STOP, still names stays
// owed, so START is refused until its owner returns it (tla/StopReturn.tla
// Named).
func TestStopDebtStillNamedInBeatStaysOwed(t *testing.T) {
	t.Parallel()
	h, _, cards := threeFriendsHoldingJobs(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 3)
	h.mu.Lock()
	h.now = h.now.Add(time.Second)
	h.mu.Unlock()
	// amy still names her job running; bob and cyd do not
	_, err = h.st.FriendBeatReport(h.ctx, "amy", sprint.FriendReport{Running: []string{cards[0]}}, nil)
	require.NoError(t, err)
	_, err = h.st.FriendBeatReport(h.ctx, "bob", sprint.FriendReport{}, nil)
	require.NoError(t, err)
	_, err = h.st.FriendBeatReport(h.ctx, "cyd", sprint.FriendReport{}, nil)
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, cards[0], "a job still named in its owner's beat stays owed")
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	require.Len(t, m.StopDebt, 1, "only the still-named lease stays owed")
	require.Equal(t, cards[0], m.StopDebt[0].ID)
	still := h.snap().Fleet.Card(cards[0])
	require.Equal(t, sprint.Working, still.Col, "the still-named job is not returned")
	// the other two were settled by the beat before the refusal
	require.Equal(t, sprint.Ready, h.snap().Fleet.Card(cards[1]).Col)
	require.Equal(t, sprint.Ready, h.snap().Fleet.Card(cards[2]).Col)
	h.clean("still-named stop debt")
}

// TestStopDebtOwnerNotBeatenSinceStopIsReported pins the report rule: an owner
// that has not beaten since the STOP keeps its lease owed and is named in a
// judgment pushed to the seat (tla/StopReturn.tla Report).
func TestStopDebtOwnerNotBeatenSinceStopIsReported(t *testing.T) {
	t.Parallel()
	h, friends, _ := threeFriendsHoldingJobs(t)
	_, _, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	h.mu.Lock()
	h.now = h.now.Add(time.Second)
	h.mu.Unlock()
	// none of the owners beat after the STOP
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "stop-return", "START is refused while the owners have not beaten")
	notes, _, _ := h.m.NotesSince(h.ctx, "", 1000)
	var found bool
	for _, n := range notes {
		if n.Type != sprint.NStopDebtBeat {
			continue
		}
		found = true
		require.Equal(t, sprint.Judgment, n.Kind, "the not-beaten owner is reported in a judgment, not a happened note")
		require.NotEmpty(t, n.Decisions, "the judgment carries the coordinator's decisions")
		for _, f := range friends {
			require.Contains(t, n.What, f, "the report names %s", f)
			require.Contains(t, n.Primaries, sprint.FriendRow(f), "the judgment is on the owner's row %s", f)
		}
	}
	require.True(t, found, "an owner that has not beaten since the STOP is reported")
	h.clean("reported stop debt")
}

// machineHoldingOneJob is a machine (m1 or m2) holding one working card, with
// the machine RUNNING. It returns the machine's row and the card it holds.
func machineHoldingOneJob(t *testing.T) (*harness, string, *sprint.Card) {
	t.Helper()
	h := newHarness(t)
	h.setup(1)
	h.startMachine()
	h.machine()
	s := h.snap()
	wc := s.Fleet.Card(s.Work.Card("s1-1").F("work"))
	require.NotNil(t, wc)
	gen := max(wc.Int("gen"), 1)
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: gen}}))
	return h, wc.Row, wc
}

// TestStopDebtMachineLeaseSettlesWhenItsBeatOwesNothing pins the machine half
// of the rule (Finding 2): a machine's beat names no job, but its stop-returns
// count is its word how many its lanes still owe; zero after the STOP settles
// its lease, so START never waits on the machine's own receipt.
func TestStopDebtMachineLeaseSettlesWhenItsBeatOwesNothing(t *testing.T) {
	t.Parallel()
	h, row, wc := machineHoldingOneJob(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 1)
	require.Equal(t, row, stopped.StopDebt[0].Row)
	h.mu.Lock()
	h.now = h.now.Add(time.Second)
	h.mu.Unlock()
	h.beatOwing(row, 0)
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "START settles the machine lease whose beat owes no stop-returns")
	require.True(t, running.Running())
	require.Empty(t, running.StopDebt)
	require.Equal(t, sprint.Ready, h.snap().Fleet.Card(wc.ID).Col)
	h.clean("machine beat-settled stop debt")
}

// TestStopDebtMachineLeaseStaysOwedWhileItOwesStopReturns pins the other side:
// a machine that has beaten since the STOP but still owes stop-returns keeps
// its lease owed until its own stop-return.
func TestStopDebtMachineLeaseStaysOwedWhileItOwesStopReturns(t *testing.T) {
	t.Parallel()
	h, row, wc := machineHoldingOneJob(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 1)
	h.mu.Lock()
	h.now = h.now.Add(time.Second)
	h.mu.Unlock()
	h.beatOwing(row, 1)
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "stop-return", "a machine that still owes stop-returns stays owed")
	require.ErrorContains(t, err, wc.ID, "START names the still-owed machine job")
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	require.Len(t, m.StopDebt, 1, "the machine lease stays owed until its own stop-return")
	require.Equal(t, wc.ID, m.StopDebt[0].ID)
	h.must(StopReturnStep(sprint.StopReturnReq{As: wc.Row, IDs: []string{wc.ID}, Gens: map[string]int{wc.ID: max(wc.Int("gen"), 1)}, Reason: "child exited"}))
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err)
	require.True(t, running.Running(), "START passes once the machine stop-returns")
	h.clean("machine lease stays owed")
}

// TestStopDebtMachineLeaseItsLiveSetNamesStaysOwed: a machine's beat that carries a
// live set (v1.2.6, tla/LiveRuns.tla) names its jobs, so a lease its fresh live set
// still names at the lease's generation stays owed though its stop-returns count is
// zero, as a friend's named job does (tla/StopReturn.tla SettleByBeat, d.id \notin
// beat); once its live set no longer names it, START settles it by the beat. Before
// this the count alone was read, and a lease named running was returned and dealt
// again under a live child.
func TestStopDebtMachineLeaseItsLiveSetNamesStaysOwed(t *testing.T) {
	t.Parallel()
	h, row, wc := machineHoldingOneJob(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 1)
	h.mu.Lock()
	h.now = h.now.Add(time.Second)
	h.mu.Unlock()
	gen := max(wc.Int("gen"), 1)
	h.liveBeat(row, sprint.LiveKey(wc.ID, gen)) // stop-returns 0, the job still named running
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, wc.ID, "a lease the machine's live set still names stays owed")
	require.Equal(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "the named job is not returned")
	h.liveBeat(row) // its live set names nothing now
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "START settles the lease once the live set no longer names it")
	require.True(t, running.Running())
	require.Equal(t, sprint.Ready, h.snap().Fleet.Card(wc.ID).Col)
	h.clean("machine lease its live set names")
}

// TestStopDebtMachineLeaseStaleLiveSetStillNamesStaysOwed: the owner's last beat since the
// STOP keeps its word for the settle, fresh or stale (tla/StopReturn.tla SettleByBeat, d.id
// \notin beat). After a STOP nothing new is taken, so a post-STOP beat that names the card
// running or held stays true until the owner beats again or returns it; a silent row's stale
// beat is not an empty live set (tla/LiveRuns.tla "staleasabsent" is the reversed witness).
// Before this the live set was read fresh-only (sprint.LiveNamesAt, liveOf), so a machine
// that beat once after the STOP naming its held report then went silent past BeatDeadline
// had its lease settled at START, the card dealt again at gen+1, and the held report refused
// as stale when it came.
func TestStopDebtMachineLeaseStaleLiveSetStillNamesStaysOwed(t *testing.T) {
	t.Parallel()
	h, row, wc := machineHoldingOneJob(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 1)
	h.clock(time.Second)
	gen := max(wc.Int("gen"), 1)
	held := sprint.LiveKey(wc.ID, gen) + ":" + sprint.LiveHeld
	h.liveBeat(row, held)                      // stop-returns 0, the finished report still held
	h.clock(sprint.BeatDeadline + time.Second) // the machine goes silent: the beat is stale
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, wc.ID, "a stale post-STOP beat that names the held report keeps its lease owed")
	require.Equal(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "the named job is not returned")
	require.Equal(t, gen, max(h.snap().Fleet.Card(wc.ID).Int("gen"), 1), "the card stays at its generation")
	// it speaks again, fresh, still naming the held report: START passes and the report is
	// taken at its own generation
	h.liveBeat(row, held)
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "a fresh beat that still names the held report does not hold the start back")
	require.True(t, running.Running())
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: gen}}))
	require.NotEqual(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "the held report is taken at its generation")
	h.clean("machine stale live set still names")
}

// TestStopDebtMachineLeaseStaleLiveSetNotNamingSettles pins the other side: a stale
// post-STOP beat whose live set does not name the card is no evidence it stays, so START
// settles the lease by the beat as before (tla/StopReturn.tla SettleByBeat, d.id \notin
// beat).
func TestStopDebtMachineLeaseStaleLiveSetNotNamingSettles(t *testing.T) {
	t.Parallel()
	h, row, wc := machineHoldingOneJob(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 1)
	h.clock(time.Second)
	h.liveBeat(row) // stop-returns 0, the set names nothing
	h.clock(sprint.BeatDeadline + time.Second)
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "a stale post-STOP beat that does not name the card settles its lease")
	require.True(t, running.Running())
	require.Empty(t, running.StopDebt)
	require.Equal(t, sprint.Ready, h.snap().Fleet.Card(wc.ID).Col)
	h.clean("machine stale live set not naming")
}
