package store

// Unit tests of the two stats verbs' store steps, from inside the store package:
// Store.TidyStats (stats_tidy.go:210) and Store.ResetStats (stats_reset.go:97).
// Their doc comments are the contract (docs/SPEC-SPRINT.md section 11,
// Statistics; tla/StatsReset.tla). One behaviour per test, over the harness's
// Mem store (newHarness), asserting on the results and on what the store's own
// reads give back, never on unexported fields.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// seedDone writes n finished ok work cards on row, all old enough not to be kept
// by the tidy's running-time window, so a fleet tidy has history to move.
func seedDone(t *testing.T, h *harness, row string, n int) []string {
	t.Helper()
	old := h.now.Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	s := h.snap()
	man := ntable.BatchManifest{Schema: 1, Table: h.st.Names.Table(sprint.Fleet), Epoch: "0",
		ExpectedTableRevision: fmt.Sprint(s.Fleet.Revision), OperationID: "seed-" + row}
	var ids []string
	for i := range n {
		id := fmt.Sprintf("seed-%02d.w1", i)
		ids = append(ids, id)
		man.Members = append(man.Members, ntable.BatchMemberEntry{ID: id, Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: row, Col: sprint.DoneOK, Score: float64(i + 1)},
			Set:    map[string]string{"kind": "work", "finished": old, "ok": "yes"}})
	}
	_, err := h.m.Apply(h.ctx, man)
	require.NoError(t, err)
	return ids
}

// archiveAt is the archive record at key, read back as a caller would.
func archiveAt(t *testing.T, h *harness, key string) StatsArchive {
	t.Helper()
	raw, ok, err := h.m.GetKey(h.ctx, key)
	require.NoError(t, err)
	require.True(t, ok, "the archive record %s is written", key)
	var a StatsArchive
	require.NoError(t, json.Unmarshal([]byte(raw), &a))
	return a
}

// doneOn is the fleet row's done cell ids at row, ordered as the table gives them.
func doneOn(h *harness, row string) []string {
	var out []string
	for _, c := range h.snap().Fleet.Cell(row, sprint.DoneOK) {
		out = append(out, c.ID)
	}
	return out
}

// (1) TidyStats with DryRun counts what it would move and keep and writes nothing.
func TestATidyDryRunCountsTheMoveAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13) // the rest rule's sample is 10: three are history only
	before, err := h.m.Snapshot()
	require.NoError(t, err)
	ids := doneOn(h, "m1")

	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "dry", DryRun: true})
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	assert.True(t, res.DryRun, "the result says it was a dry run")
	assert.Equal(t, 3, res.Moved, "three of thirteen would move")
	assert.Equal(t, 10, res.Kept)
	assert.Equal(t, ids, doneOn(h, "m1"), "a dry run moves no card")
	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "a dry run writes nothing")
	_, ok, err := h.m.GetKey(h.ctx, res.Archive)
	require.NoError(t, err)
	assert.False(t, ok, "a dry run writes no archive")
	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Zero(t, rec.Since, "a dry run records no tidy")
}

// (2) A fleet tidy takes the done cards off their done cells, keeps their
// records, and writes the archive as done; StatsTidied then names the archive
// and the kind's time, and no tidy marker stands.
func TestATidyMovesTheDoneCardsAndArchivesThem(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13)

	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "history"})
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	assert.Equal(t, 3, res.Moved)
	assert.Equal(t, 10, res.Kept)
	assert.Equal(t, 10, len(doneOn(h, "m1")), "the history left the done cell")

	arch := archiveAt(t, h, res.Archive)
	assert.Equal(t, ArchiveDone, arch.State)
	assert.Equal(t, "history", arch.Reason)
	require.Len(t, arch.Rows, 1)
	assert.Equal(t, "m1", arch.Rows[0].Row)
	assert.Len(t, arch.Rows[0].Moved, 3)
	for _, c := range arch.Rows[0].Moved {
		assert.False(t, c.Finished.IsZero(), "%s carries its finish stamp", c.ID)
	}

	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{res.Archive}, rec.Archives)
	assert.Equal(t, h.now.UTC(), rec.Since)
	require.Contains(t, rec.Kinds, sprint.TidyFleet)
	assert.Equal(t, h.now.UTC(), rec.Kinds[sprint.TidyFleet], "the kind's time is the tidy's")
	assert.Nil(t, rec.Tidying, "no tidy marker stands after it ends")
}

// (3) A second tidy within sprint.TidyAgainAfter is refused with nothing
// written; one just after it runs.
func TestASecondTidyWithinTheWindowIsRefused(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13)
	first, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "first"})
	require.NoError(t, err)
	require.Empty(t, first.Refused)

	h.tick(30 * time.Second)
	before, err := h.m.Snapshot()
	require.NoError(t, err)
	second, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "second"})
	require.NoError(t, err)
	assert.Contains(t, second.Refused, "a second within 1m0s is refused, nothing written")
	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "a refused tidy writes nothing")

	h.tick(sprint.TidyAgainAfter)
	third, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "third"})
	require.NoError(t, err)
	assert.Empty(t, third.Refused, "one just after the window runs")
}

// (4) A fresh tidy marker makes a tidy fail "in flight" and the marker stays; a
// marker older than TidyStale is taken as ended, and the result says so.
func TestATidyRefusesOnAFreshMarkerAndTakesAStaleOneAsEnded(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13)
	writeMarker := func(at time.Time) {
		b, err := json.Marshal(StatsRecord{Epoch: 0, Tidying: &TidyInFlight{Archive: "stats:archive:x", At: at}})
		require.NoError(t, err)
		require.NoError(t, h.m.SetKey(h.ctx, keyStats, string(b)))
	}

	writeMarker(h.now)
	_, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "while fresh"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "another stats tidy is in flight")
	raw, ok, err := h.m.GetKey(h.ctx, keyStats)
	require.NoError(t, err)
	require.True(t, ok)
	var kept StatsRecord
	require.NoError(t, json.Unmarshal([]byte(raw), &kept))
	require.NotNil(t, kept.Tidying, "the fresh marker stays")
	assert.Equal(t, "stats:archive:x", kept.Tidying.Archive)

	writeMarker(h.now.Add(-TidyStale - time.Minute))
	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "past stale"})
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	assert.Contains(t, fmt.Sprint(res.Said), "taken as ended", "the result says the stale marker was taken as ended")
}

// (5) Routes alone moves no card; the streams kind records the streams and
// clears the where record.
func TestATidyOfRoutesAndStreamsMovesNoCardAndClearsTheWhereRecord(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	ids := seedDone(t, h, "m1", 3)
	require.NoError(t, h.m.SetKey(h.ctx, keyWhere, `{"streams":{}}`))

	routes, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyRoutes}, Reason: "routes"})
	require.NoError(t, err)
	require.Empty(t, routes.Refused)
	assert.Zero(t, routes.Moved, "routes alone moves no card")
	assert.Equal(t, ids, doneOn(h, "m1"), "the done cards are where they were")

	h.tick(2 * time.Minute)
	streams, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyStreams}, Reason: "streams"})
	require.NoError(t, err)
	require.Empty(t, streams.Refused)
	raw, ok, err := h.m.GetKey(h.ctx, keyWhere)
	require.NoError(t, err)
	assert.True(t, ok && raw == "", "the streams kind clears the where record (got %q, ok %v)", raw, ok)
}

// refusingApply is a Backend whose Apply refuses every manifest: a move the
// store will not take.
type refusingApply struct{ *Mem }

func (r refusingApply) Apply(context.Context, ntable.BatchManifest) (ntable.Receipt, error) {
	return ntable.Receipt{}, errors.New("the store refused the move")
}

// (6) A move the store refuses: the error names the archive key, the archive is
// written failed, and no marker stands.
func TestATidyWhoseMoveTheStoreRefusesKeepsAFailedArchive(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	ids := seedDone(t, h, "m1", 13)
	st := *h.st
	st.B = refusingApply{Mem: h.m}

	res, err := st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "refused"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), res.Archive, "the error names the archive key")
	arch := archiveAt(t, h, res.Archive)
	assert.Equal(t, ArchiveFailed, arch.State, "the archive is written failed")
	assert.Equal(t, ids, doneOn(h, "m1"), "nothing moved")

	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Nil(t, rec.Tidying, "no tidy marker stands after a failed move")
	assert.Equal(t, []string{res.Archive}, rec.Archives, "the failed archive's key is kept for teardown")
}

// (7) ResetStats DryRun returns the mark it would write and writes nothing; a
// reset writes the mark (StatsReset reads it back) and clears the where record;
// a second reset names the mark it replaced.
func TestAResetDryRunThenWriteThenReplace(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 3)

	before, err := h.m.Snapshot()
	require.NoError(t, err)
	dry, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "dry", DryRun: true})
	require.NoError(t, err)
	assert.True(t, dry.DryRun)
	assert.Nil(t, dry.Replaced)
	assert.Equal(t, 3, dry.Mark.Rows["m1"].OK, "a dry run returns the mark it would write")
	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "a dry run writes nothing")
	m, err := h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	assert.Nil(t, m, "a dry run writes no mark")

	require.NoError(t, h.m.SetKey(h.ctx, keyWhere, `{"streams":{}}`))
	res, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "first"})
	require.NoError(t, err)
	assert.Nil(t, res.Replaced)
	m, err = h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, m, "the reset's mark reads back")
	assert.Equal(t, "first", m.Reason)
	raw, ok, err := h.m.GetKey(h.ctx, keyWhere)
	require.NoError(t, err)
	assert.True(t, ok && raw == "", "the reset clears the where record (got %q, ok %v)", raw, ok)

	h.tick(time.Second)
	again, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "second"})
	require.NoError(t, err)
	require.NotNil(t, again.Replaced, "a second reset names the mark it replaced")
	assert.Equal(t, "first", again.Replaced.Reason)
}

// (8) The same --op with the same reason replays the mark and writes nothing;
// the same op with another reason is an OpConflictError; an op holding "~" or
// "/" is refused before any read.
func TestAResetReplaysAndRefusesByOperationId(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 3)

	first, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "count from now", Op: "reset-1"})
	require.NoError(t, err)
	assert.False(t, first.Replay)
	assert.Equal(t, "reset-1", first.Mark.Op)

	again, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "count from now", Op: "reset-1"})
	require.NoError(t, err)
	assert.True(t, again.Replay, "the same op and reason replays the mark")
	assert.Equal(t, first.Mark.At, again.Mark.At, "and writes no second")

	_, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "another reason", Op: "reset-1"})
	var conflict *OpConflictError
	require.ErrorAs(t, err, &conflict, "the same op with another reason is refused")
	assert.True(t, conflict.OtherArgs)

	before, err := h.m.Snapshot()
	require.NoError(t, err)
	for _, op := range []string{"bad~op", "bad/op"} {
		_, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "x", Op: op})
		require.Error(t, err, "an op %q is refused", op)
	}
	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "an invalid op is refused before any read or write")
}

// (9) A fresh tidy marker refuses the reset with nothing written; a stale one
// does not.
func TestAResetRefusesWhileATidyIsInFlightAndNotWhenStale(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 3)
	writeMarker := func(at time.Time) {
		b, err := json.Marshal(StatsRecord{Epoch: 0, Tidying: &TidyInFlight{Archive: "stats:archive:x", At: at}})
		require.NoError(t, err)
		require.NoError(t, h.m.SetKey(h.ctx, keyStats, string(b)))
	}

	writeMarker(h.now)
	before, err := h.m.Snapshot()
	require.NoError(t, err)
	res, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "while fresh"})
	require.NoError(t, err)
	assert.Contains(t, res.Refused, "a stats tidy is in flight")
	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "a refused reset writes nothing")

	writeMarker(h.now.Add(-TidyStale - time.Minute))
	res, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "past stale"})
	require.NoError(t, err)
	assert.Empty(t, res.Refused, "past a stale marker the reset goes on")
	m, err := h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "past stale", m.Reason)
}

// refusingSwap is a store whose SwapKey never succeeds: the stats record keeps
// changing under the reset.
type refusingSwap struct{ *Mem }

func (r refusingSwap) SwapKey(context.Context, string, string, bool, string) (bool, error) {
	return false, nil
}

// (10) A wrapper whose SwapKey always fails: the reset errors after its tries,
// saying it kept changing.
func TestAResetSaysWhenTheStatsRecordKeptChanging(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 3)
	st := *h.st
	st.B = refusingSwap{Mem: h.m}

	_, err := st.ResetStats(h.ctx, ResetReq{Reason: "churning"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kept changing under the reset")
	assert.Contains(t, err.Error(), fmt.Sprint(statsSwapTries))
}

// (11) A tidy's kinds and archives are kept across a reset.
func TestAResetKeepsATidysKindsAndArchives(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13)
	tidy, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "history"})
	require.NoError(t, err)
	require.Empty(t, tidy.Refused)

	h.tick(2 * time.Minute)
	_, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "count from now"})
	require.NoError(t, err)
	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{tidy.Archive}, rec.Archives, "the tidy's archive is kept across the reset")
	require.Contains(t, rec.Kinds, sprint.TidyFleet, "the tidy's kinds are kept across the reset")
	assert.Equal(t, rec.Since, rec.Kinds[sprint.TidyFleet], "the kind's time is still the tidy's, unchanged by the reset")
}
