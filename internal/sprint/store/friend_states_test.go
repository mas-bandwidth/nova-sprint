package store

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
)

// threeStatesProblems is what is wrong with the friend states found: each one that is not
// up, down or held, and each of the three not found. None is the sprint's rule (the owner,
// 2026-10-14, v1.3.6: "there is only up, down, held from the sprint POV").
func threeStatesProblems(found map[string]bool) []string {
	want := map[string]bool{sprint.Up: true, sprint.Down: true, sprint.Held: true}
	var out []string
	for _, st := range slices.Sorted(maps.Keys(found)) {
		if !want[st] {
			out = append(out, fmt.Sprintf("a friend state %q that is not up, down or held", st))
		}
	}
	for _, st := range slices.Sorted(maps.Keys(want)) {
		if !found[st] {
			out = append(out, fmt.Sprintf("the friend state %q is never produced", st))
		}
	}
	return out
}

// friendStatesProduced is every status the friends rows and the deal's seats carry, over a
// roster with one friend for each state of the nova-hook record (no record, each hook state,
// proven with a down reason), held and not held, each with a work report that names a job
// running and one that names none, read now and a day later (no friend finishes anything).
func friendStatesProduced(t *testing.T) map[string]bool {
	t.Helper()
	h := newHarness(t)
	hooks := map[string]hook.Status{}
	var specs []FriendSpec
	var held []string
	runs := map[string]bool{}
	for i, st := range []hook.Status{
		{}, {State: hook.StateNone}, {State: hook.StateUnhooked, Reason: "missed 2 challenges"}, {State: hook.StateExpired},
		{State: hook.StatePartial}, {State: hook.StateLive}, {State: hook.StateProven},
		{State: hook.StateProven, Down: "missed 2 challenges"},
	} {
		for _, hold := range []bool{false, true} {
			for _, running := range []bool{false, true} {
				name := fmt.Sprintf("f%d-%v-%v", i, hold, running)
				specs = append(specs, FriendSpec{Name: name, Width: 2})
				if st.State != "" {
					st.Name = name
					hooks[name] = st
				}
				if hold {
					held = append(held, name)
				}
				runs[name] = running
			}
		}
	}
	_, _, _, err := h.st.SyncFriends(h.ctx, specs)
	require.NoError(t, err)
	for _, name := range held {
		require.NoError(t, h.st.SetFriendHeld(h.ctx, name, true, "tester", "rest", time.Time{}, 0))
	}
	for _, sp := range specs {
		rep := sprint.FriendReport{Active: h.now.Add(-time.Hour)}
		if runs[sp.Name] {
			rep.Running = []string{"job-" + sp.Name}
		}
		_, err := h.st.FriendBeatReport(h.ctx, sp.Name, rep, nil)
		require.NoError(t, err)
	}
	h.st.ReadFriendHooks = func(_ context.Context, names ...string) ([]hook.Status, time.Time, error) {
		var out []hook.Status
		for _, n := range names {
			if st, ok := hooks[n]; ok {
				out = append(out, st)
			}
		}
		return out, h.now, nil
	}
	found := map[string]bool{}
	for _, at := range []time.Time{h.now, h.now.Add(24 * time.Hour)} {
		rows, err := h.st.FriendRows(h.ctx, at)
		require.NoError(t, err)
		require.Len(t, rows, len(specs))
		for _, r := range rows {
			found[r.Status] = true
		}
		seats, err := h.st.FriendSeats(h.ctx, at)
		require.NoError(t, err)
		for _, s := range seats {
			found[s.Status] = true
		}
	}
	return found
}

// The sprint can produce exactly three friend states: the status constants (sprint.FriendStates)
// and every value the friends rows (where --json's friends table, view, the dashboard) and the
// deal's seats (friendSeats: the tick's deal, stops and routes) carry, over every state the
// hook record has, held or not, with work reported running or not, now and a day on, are up,
// down and held and nothing else. The retired fourth states (stalled, idle, deaf, asleep, a
// broken session, the beat- and pong-based statuses, dealt no more) are none of them.
func TestTheSprintHasExactlyThreeFriendStates(t *testing.T) {
	t.Parallel()
	constants := map[string]bool{}
	for _, st := range sprint.FriendStates {
		constants[st] = true
	}
	assert.Len(t, sprint.FriendStates, 3, "sprint.FriendStates lists each state once")
	assert.Empty(t, threeStatesProblems(constants), "sprint.FriendStates")

	produced := friendStatesProduced(t)
	assert.Empty(t, threeStatesProblems(produced), "the friends rows and seats")
	for st := range produced {
		assert.True(t, constants[st], "%q is produced and is not in sprint.FriendStates", st)
	}
}

// The reversed witness: the check refuses a fourth state beside the three (as the stall
// ladder's "stalled" was), and a missing one.
func TestTheThreeStatesCheckRefusesAFourthState(t *testing.T) {
	t.Parallel()
	produced := friendStatesProduced(t)
	require.Empty(t, threeStatesProblems(produced))

	fourth := maps.Clone(produced)
	fourth["stalled"] = true
	assert.Equal(t, []string{`a friend state "stalled" that is not up, down or held`}, threeStatesProblems(fourth))

	missing := maps.Clone(produced)
	delete(missing, sprint.Held)
	assert.Equal(t, []string{`the friend state "held" is never produced`}, threeStatesProblems(missing))
}
