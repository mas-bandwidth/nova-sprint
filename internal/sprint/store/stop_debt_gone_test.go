package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// oneFriendHoldingJob is friend amy holding one working card her beat names running, with
// the machine RUNNING: the card and its generation.
func oneFriendHoldingJob(t *testing.T) (*harness, *sprint.Card, int) {
	t.Helper()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1, Class: "flash"}})
	require.NoError(t, err)
	h.up("amy")
	brief := "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend amy\n\nThe task."
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: brief}}}))
	h.startMachine()
	h.machine()
	h.start("amy", 1)
	working := h.snap().Fleet.Cell(sprint.FriendRow("amy"), sprint.Working)
	require.Len(t, working, 1, "amy holds one working card")
	wc := working[0]
	_, err = h.st.FriendBeatReport(h.ctx, "amy", sprint.FriendReport{Running: []string{wc.ID}}, nil)
	require.NoError(t, err)
	return h, wc, max(wc.Int("gen"), 1)
}

// stopDebtNotes is every STOP-debt judgment (NStopDebtBeat) written so far.
func stopDebtNotes(h *harness) []sprint.Note {
	h.t.Helper()
	notes, _, err := h.m.NotesSince(h.ctx, "", 1000)
	require.NoError(h.t, err)
	var out []sprint.Note
	for _, n := range notes {
		if n.Type == sprint.NStopDebtBeat {
			out = append(out, n)
		}
	}
	return out
}

// TestStartPassesAStopDebtWhoseJobLeftTheTable is the 2026-10-18 incident: after a STOP, the
// STOPPED reconcile (LiveReturnStep, exempt from the debt guard) returned the owned jobs off
// the table, writing the STOP's receipt on the records it retired, but START read only the
// placed cells, so stopOwned found no card ("out of the table is owned") and START refused
// for good, with stop-return unable to settle them ("not a live card"). START now reads
// each debt's own record (stopDebtExtras): a receipt on a retired record settles the debt
// and START names it. A card removed without a receipt, or still on the table with none,
// still refuses START.
// tla/StopReturn.tla Retire, GoneNeverBlocksStart
func TestStartPassesAStopDebtWhoseJobLeftTheTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// setup is a STOPPED machine with one captured debt: the debt's row, id and
		// generation, after the job left its table (or did not, for the regression guard)
		setup  func(t *testing.T) (h *harness, row, id string, gen int)
		starts bool
	}{
		{"fleet job retired by the friend reconcile's live return", func(t *testing.T) (*harness, string, string, int) {
			h, wc, gen := oneFriendHoldingJob(t)
			_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, stopped.StopDebt, 1)
			// the incident's path: amy's nova-hook unhooks after the STOP, and the STOPPED tick's
			// reconcile (LiveReturnStep, exempt from the debt guard) retires her working card off
			// the fleet table ("working -> retired, returned by friend reconcile: nova-hook
			// unhooked"); the receipt it writes sits on the kept record, which no whole-table
			// read sees
			h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
				return []hook.Status{{Name: "amy", State: hook.StateUnhooked}}, h.now, nil
			}
			var res TickResult
			require.NoError(t, h.st.liveStopped(h.ctx, nil, &res))
			require.Len(t, res.Parts, 1, "the reconcile returned her card")
			require.Contains(t, res.Parts[0].Moved[0], wc.ID+" working -> retired, returned by friend reconcile: nova-hook unhooked")
			require.Nil(t, h.snap().Fleet.Card(wc.ID), "the whole-table read no longer sees the job")
			m, _, err := h.st.Machine(h.ctx)
			require.NoError(t, err)
			require.Len(t, m.StopDebt, 1, "the debt stays on the machine record: the receipt is on a record no read sees")
			return h, wc.Row, wc.ID, gen
		}, true},
		{"fleet job removed without a receipt", func(t *testing.T) (*harness, string, string, int) {
			h, wc, gen := oneFriendHoldingJob(t)
			_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, stopped.StopDebt, 1)
			// out-of-band removal writes no receipt: the debt stays owned, the safe side
			h.poke(sprint.Fleet, ntable.BatchMemberEntry{ID: wc.ID, Remove: true})
			require.Nil(t, h.snap().Fleet.Card(wc.ID))
			return h, wc.Row, wc.ID, gen
		}, false},
		{"fleet job still on the table, no receipt", func(t *testing.T) (*harness, string, string, int) {
			h, wc, gen := oneFriendHoldingJob(t)
			_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, stopped.StopDebt, 1)
			return h, wc.Row, wc.ID, gen
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, row, id, gen := tc.setup(t)
			key := row + ":" + id + "@" + fmt.Sprint(gen)
			// no owner beats after the STOP: the beat settle has no word on the debt
			_, running, res, err := h.st.SetMachine(h.ctx, true)
			if !tc.starts {
				require.ErrorContains(t, err, key, "a job still on the table with no receipt holds START")
				require.ErrorContains(t, err, "lack same-owner cancellation receipts")
				return
			}
			require.NoError(t, err, "START passes a debt whose job left the table")
			require.True(t, running.Running())
			require.Empty(t, running.StopDebt, "START clears the debt")
			require.Len(t, res.Said, 1, "the start says what it settled")
			require.Contains(t, res.Said[0], "1 debt(s) settled: job returned off the table with its STOP receipt (live-returned after the STOP): "+key)
			notes, _, err := h.m.NotesSince(h.ctx, "", 1000)
			require.NoError(t, err)
			var last *sprint.Note
			for i := range notes {
				if notes[i].Type == sprint.NMachineStarted {
					last = &notes[i]
				}
			}
			require.NotNil(t, last, "the start wrote its note")
			require.Contains(t, last.What, key, "the start's note names the settled gone debt")
			require.Empty(t, stopDebtNotes(h), "a returned job is not reported as owed")
		})
	}
}

// TestUnsettledStopDebtReadsTheRetiredRecord is the rule on a snapshot, for both tables: an
// entry whose card left the table carrying its receipt (stopped_from_gen = the captured gen,
// a later gen) is settled and named; one off the table with no record read, or with a record
// that carries no receipt, stays open (the safe side).
// tla/StopReturn.tla Retire, GoneNeverBlocksStart
func TestUnsettledStopDebtReadsTheRetiredRecord(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, table := range []string{sprint.Fleet, sprint.Readers} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			tbl := sprint.NewTable(table)
			tbl.SetRows([]string{"reader-a"})
			// retired with the receipt, kept off the table
			tbl.Put(&sprint.Card{ID: "ret", Fields: map[string]string{"gen": "3", "stopped_from_gen": "2", "retired": "x"}})
			// kept off the table with no receipt
			tbl.Put(&sprint.Card{ID: "bare", Fields: map[string]string{"gen": "2", "retired": "x"}})
			s := &sprint.Snapshot{Now: now}
			if table == sprint.Fleet {
				s.Fleet, s.Readers = tbl, sprint.NewTable(sprint.Readers)
			} else {
				s.Fleet, s.Readers = sprint.NewTable(sprint.Fleet), tbl
			}
			m := Machine{StopIssued: true, StopDebt: []StopLease{{Table: table, Row: "reader-a", ID: "ret", Gen: 2}}}
			retired, err := unsettledStopDebt(m, s, nil, now)
			require.NoError(t, err, "a receipt on the retired record settles the debt")
			require.Equal(t, []string{"reader-a:ret@2"}, retired)
			for _, d := range []StopLease{
				{Table: table, Row: "reader-a", ID: "bare", Gen: 2},   // record read, no receipt
				{Table: table, Row: "reader-a", ID: "unread", Gen: 2}, // no record read at all
			} {
				m.StopDebt = []StopLease{d}
				_, err := unsettledStopDebt(m, s, nil, now)
				require.ErrorContains(t, err, "reader-a:"+d.ID+"@2", "%s stays owned", d.ID)
			}
			// the extras name the off-table ids by table, and only those
			ex := stopDebtExtras([]StopLease{{Table: table, ID: "ret"}, {Table: "work", ID: "w"}})(s)
			require.Equal(t, map[string][]string{table: {"ret"}}, ex)
		})
	}
}

// TestSettleStopDebtByBeatSkipsAReturnedJob: the beat settle run before START's fence
// (settleStopDebtByBeat) reads each debt's own record and skips one the reconcile returned
// off the table with its receipt: it writes no judgment naming its row as "no beat since
// the STOP" and runs no stop-return for it (which would refuse "not a live card"). Owners
// that have not beaten whose jobs are still on the table are reported as before.
// tla/StopReturn.tla Retire, GoneNeverBlocksStart
func TestSettleStopDebtByBeatSkipsAReturnedJob(t *testing.T) {
	t.Parallel()
	h, friends, cards := threeFriendsHoldingJobs(t)
	_, stopped, _, err := h.st.StopUntil(h.ctx, "operator stopped children", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 3)
	h.clock(time.Second)
	// amy's nova-hook unhooks after the STOP: the reconcile returns her job off the table
	h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
		return []hook.Status{{Name: "amy", State: hook.StateUnhooked}, {Name: "bob", State: hook.StateProven}, {Name: "cyd", State: hook.StateProven}}, h.now, nil
	}
	var res TickResult
	require.NoError(t, h.st.liveStopped(h.ctx, nil, &res))
	require.Nil(t, h.snap().Fleet.Card(cards[0]), "amy's job left the table")
	require.True(t, h.snap().Fleet.Card(cards[1]).Placed(), "bob's job stays")
	require.NoError(t, h.st.settleStopDebtByBeat(h.ctx))
	notes := stopDebtNotes(h)
	require.Len(t, notes, 1, "one judgment reports the owners that have not beaten")
	require.NotContains(t, notes[0].Primaries, sprint.FriendRow(friends[0]), "the returned job's row is not reported")
	require.NotContains(t, notes[0].What, friends[0])
	for _, f := range friends[1:] {
		require.Contains(t, notes[0].Primaries, sprint.FriendRow(f), "%s still holds her job and has not beaten", f)
	}
	// START: the returned debt is settled; the two still on the table hold it
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "2 STOP-owned work/read jobs lack same-owner cancellation receipts")
	require.NotContains(t, err.Error(), cards[0], "the returned job is not among the open debts")
}
