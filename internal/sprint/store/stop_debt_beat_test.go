package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
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
// note to the seat (tla/StopReturn.tla Report).
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
		if n.Type == sprint.NStopDebtBeat {
			found = true
			for _, f := range friends {
				require.Contains(t, n.What, f, "the report names %s", f)
			}
		}
	}
	require.True(t, found, "an owner that has not beaten since the STOP is reported")
	h.clean("reported stop debt")
}

