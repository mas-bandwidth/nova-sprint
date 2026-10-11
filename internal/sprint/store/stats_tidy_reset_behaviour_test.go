package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// seedDone puts n finished work cards on a fleet row, one a minute apart, oldest first:
// the oldest fall outside every keep window and beyond the row's newest ten (the rest
// rule's sample), so a fleet tidy moves them and keeps the newest ten.
func seedDone(t *testing.T, h *harness, row string, n int, base time.Time) []string {
	t.Helper()
	table := h.st.Names.Table(sprint.Fleet)
	ids := make([]string, n)
	for j := range n {
		id := fmt.Sprintf("%s-job%02d.w1", row, j)
		ids[j] = id
		fin := base.Add(time.Duration(j) * time.Minute).UTC().Format(time.RFC3339)
		_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: table, Epoch: fmt.Sprint(h.st.epoch),
			ExpectedTableRevision: fmt.Sprint(h.m.Revision(table)), OperationID: "seed-" + id,
			Members: []ntable.BatchMemberEntry{{ID: id, Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: row, Col: sprint.DoneOK, Score: float64(j + 1)},
				Set: map[string]string{"kind": "work", "primary": strings.TrimSuffix(id, ".w1"), "attempt": "1",
					"ok": "yes", "finished": fin, "model": "prov-a/x", "usage": "wall=900s"}}}})
		require.NoError(t, err, "seed %s", id)
	}
	return ids
}

// placed reports whether the fleet card id sits on a cell.
func fleetPlaced(h *harness, id string) bool { return h.snap().Fleet.Placed(id) != nil }

// archiveOf reads the archive record at key.
func archiveOf(t *testing.T, h *harness, key string) StatsArchive {
	t.Helper()
	raw, ok, err := h.m.GetKey(h.ctx, key)
	require.NoError(t, err)
	require.True(t, ok, "the archive record %s is written", key)
	var a StatsArchive
	require.NoError(t, json.Unmarshal([]byte(raw), &a))
	return a
}

// putStats writes the stats record directly, as a store that died mid-tidy leaves one.
func putStats(t *testing.T, h *harness, rec StatsRecord) {
	t.Helper()
	b, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, h.m.SetKey(h.ctx, keyStats, string(b)))
}

// TidyStats doc: "DryRun writes nothing and says what would move." The plan counts the
// history-only cards (TidyStats, stats_tidy.go:210).
func TestATidyDryRunCountsWhatWouldMoveAndWritesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	ids := seedDone(t, h, "m1", 13, t0.Add(-48*time.Hour))
	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "dry", DryRun: true})
	require.NoError(t, err)
	assert.True(t, res.DryRun)
	assert.Equal(t, 3, res.Moved)
	assert.Equal(t, 10, res.Kept)
	_, ok, err := h.m.GetKey(h.ctx, res.Archive)
	require.NoError(t, err)
	assert.False(t, ok, "a dry run writes no archive")
	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Zero(t, rec.Since, "a dry run records no tidy")
	assert.Nil(t, rec.Tidying, "a dry run leaves no marker")
	for _, id := range ids[:3] {
		assert.True(t, fleetPlaced(h, id), "%s stays on its done cell", id)
	}
}

// TidyStats doc: the archive is written done, the record names the tidy, the cards leave
// the done cells and no marker stands (stats_tidy.go:210).
func TestAFleetTidyMovesHistoryOffTheDoneCellsAndRecordsIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	ids := seedDone(t, h, "m1", 13, t0.Add(-48*time.Hour))
	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "history"})
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	assert.Equal(t, 3, res.Moved)
	assert.True(t, strings.HasPrefix(res.Archive, "stats:archive:"), "key prefix: %s", res.Archive)
	var movedIDs []string
	for _, r := range res.Record.Rows {
		for _, c := range r.Moved {
			movedIDs = append(movedIDs, c.ID)
		}
	}
	for _, id := range ids[:3] {
		assert.False(t, fleetPlaced(h, id), "%s left its done cell", id)
		assert.Contains(t, movedIDs, id, "%s is recorded as moved", id)
	}
	arch := archiveOf(t, h, res.Archive)
	assert.Equal(t, ArchiveDone, arch.State)
	assert.Equal(t, "history", arch.Reason)
	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.False(t, rec.Since.IsZero(), "the record names the tidy")
	assert.Equal(t, []string{res.Archive}, rec.Archives)
	assert.NotZero(t, rec.Kinds[sprint.TidyFleet], "the fleet kind's time")
	assert.Nil(t, rec.Tidying, "no tidy marker stands")
}

// TidyStats doc: "A tidy within sprint.TidyAgainAfter of the last ... is refused, nothing
// written" (stats_tidy.go:210, TidyAgainAfter stats_tidy.go:37).
func TestASecondTidyWithinAMinuteIsRefusedAndOneAfterItRuns(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13, t0.Add(-48*time.Hour))
	first, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "first"})
	require.NoError(t, err)
	require.Empty(t, first.Refused)
	h.tick(30 * time.Second)
	second, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "second"})
	require.NoError(t, err)
	assert.Contains(t, second.Refused, "a second within")
	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "first", rec.Reason, "the refused tidy wrote nothing")
	h.tick(31 * time.Second)
	third, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "third"})
	require.NoError(t, err)
	assert.Empty(t, third.Refused, "just over the minute: it runs")
	rec, err = h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "third", rec.Reason)
}

// TidyStats doc: a tidy in flight refuses another; past TidyStale the marker is taken as
// ended (stats_tidy.go:210, TidyStale stats_tidy.go:67).
func TestAFreshTidyMarkerRefusesATidyAndAStaleOneIsTakenAsEnded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13, t0.Add(-48*time.Hour))
	putStats(t, h, StatsRecord{Tidying: &TidyInFlight{Archive: "stats:archive:old", At: t0}})
	_, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "now"})
	require.Error(t, err, "a fresh marker refuses the tidy")
	assert.Contains(t, err.Error(), "in flight")
	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, rec.Tidying, "the marker stays")
	assert.Equal(t, "stats:archive:old", rec.Tidying.Archive)

	putStats(t, h, StatsRecord{Tidying: &TidyInFlight{Archive: "stats:archive:old", At: t0.Add(-TidyStale - time.Minute)}})
	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "now"})
	require.NoError(t, err)
	assert.Empty(t, res.Refused)
	found := false
	for _, s := range res.Said {
		found = found || strings.Contains(s, "taken as ended")
	}
	assert.True(t, found, "the result says the marker is past TidyStale: %v", res.Said)
}

// TidyStats doc: routes counters alone move no card (stats_tidy.go:210).
func TestATidyOfRoutesAloneMovesNoCard(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	ids := seedDone(t, h, "m1", 13, t0.Add(-48*time.Hour))
	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyRoutes}, Reason: "routes"})
	require.NoError(t, err)
	assert.Zero(t, res.Moved, "routes alone moves nothing")
	for _, id := range ids {
		assert.True(t, fleetPlaced(h, id), "%s stays", id)
	}
}

// TidyStats doc: the streams kind records the streams and the where record is counted
// again (stats_tidy.go:210).
func TestATidyOfStreamsRecordsTheStreamsAndClearsTheWhereRecord(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	require.NoError(t, h.m.SetKey(h.ctx, keyWhere, "stale"))
	res, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyStreams}, Reason: "streams"})
	require.NoError(t, err)
	assert.NotNil(t, res.Record.Streams, "the streams are recorded")
	v, ok, err := h.m.GetKey(h.ctx, keyWhere)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, v, "the where record is cleared for the next tick")
}

// applyRefuses is the twin store whose Apply never applies: a move the store refuses.
type applyRefuses struct {
	*Mem
}

func (a applyRefuses) AtEpoch(epoch uint64, old bool) Backend {
	return applyRefuses{a.Mem.AtEpoch(epoch, old).(*Mem)}
}

func (a applyRefuses) Apply(ctx context.Context, m ntable.BatchManifest) (ntable.Receipt, error) {
	return ntable.Receipt{}, errors.New("the store refused the move")
}

// TidyStats doc: a move that fails leaves its archive failed, clears the marker and names
// the archive (stats_tidy.go:210).
func TestATidyWhoseMoveIsRefusedKeepsItsArchiveFailedAndClearsTheMarker(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13, t0.Add(-48*time.Hour))
	st := *h.st
	st.B = applyRefuses{h.m}
	res, err := st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "fails"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), res.Archive, "the error names the archive key")
	arch := archiveOf(t, h, res.Archive)
	assert.Equal(t, ArchiveFailed, arch.State, "the archive is written failed")
	rec, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Nil(t, rec.Tidying, "no marker stands")
}

// ResetStats doc: "DryRun writes nothing and returns the mark"; a reset writes the mark
// and clears the where record; a second names the mark it replaced (stats_reset.go:97).
func TestAResetDryRunWritesNothingAndAResetWritesTheMark(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 3, t0.Add(-48*time.Hour))
	dry, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "dry", DryRun: true})
	require.NoError(t, err)
	assert.True(t, dry.DryRun)
	require.Contains(t, dry.Mark.Rows, "m1")
	assert.Equal(t, 3, dry.Mark.Rows["m1"].OK)
	m, err := h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	assert.Nil(t, m, "a dry run writes no mark")
	require.NoError(t, h.m.SetKey(h.ctx, keyWhere, "stale"))
	res, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "count from now"})
	require.NoError(t, err)
	assert.Nil(t, res.Replaced)
	m, err = h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "count from now", m.Reason)
	v, _, err := h.m.GetKey(h.ctx, keyWhere)
	require.NoError(t, err)
	assert.Empty(t, v, "the where record is cleared")
	h.tick(time.Minute)
	again, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "again"})
	require.NoError(t, err)
	require.NotNil(t, again.Replaced)
	assert.Equal(t, "count from now", again.Replaced.Reason, "the second names the mark it replaced")
}

// ResetStats doc: the same operation id replays; another reason, or an id an earlier step
// recorded, is an OpConflictError; an id with "~" or "/" is refused (stats_reset.go:97).
func TestAResetOpIdReplaysAndConflictsAndBadShapesAreRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 2, t0.Add(-48*time.Hour))
	first, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "r", Op: "reset-1"})
	require.NoError(t, err)
	assert.False(t, first.Replay)
	h.tick(time.Minute)
	again, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "r", Op: "reset-1"})
	require.NoError(t, err)
	assert.True(t, again.Replay, "the same id replays")
	assert.Equal(t, first.Mark.At, again.Mark.At, "and writes no second")
	_, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "other", Op: "reset-1"})
	var conflict *OpConflictError
	require.ErrorAs(t, err, &conflict, "another reason under the same id")
	assert.True(t, conflict.OtherArgs)

	step := DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}})
	step.CallerOp = "deal-1"
	h.must(step)
	_, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "x", Op: "deal-1"})
	require.ErrorAs(t, err, &conflict, "an id an earlier step recorded")

	for _, op := range []string{"bad~op", "bad/op"} {
		_, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "x", Op: op})
		require.Error(t, err, "op %q is refused", op)
	}
}

// ResetStats doc: a tidy in flight refuses the reset with nothing written; past TidyStale
// it goes on (stats_reset.go:97, stats_tidy.go:67).
func TestAResetIsRefusedByAFreshTidyMarkerAndNotByAStaleOne(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 2, t0.Add(-48*time.Hour))
	putStats(t, h, StatsRecord{Tidying: &TidyInFlight{Archive: "stats:archive:old", At: t0}})
	res, err := h.st.ResetStats(h.ctx, ResetReq{Reason: "now"})
	require.NoError(t, err)
	assert.Contains(t, res.Refused, "a stats tidy is in flight")
	m, err := h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	assert.Nil(t, m, "nothing written while the marker stands")

	putStats(t, h, StatsRecord{Tidying: &TidyInFlight{Archive: "stats:archive:old", At: t0.Add(-TidyStale - time.Minute)}})
	res, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "now"})
	require.NoError(t, err)
	assert.Empty(t, res.Refused, "past a stale marker the reset goes on")
	m, err = h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, m)
}

// swapMoved is the twin store whose compare-and-set never takes: the record always moved.
type swapMoved struct {
	*Mem
}

func (s swapMoved) AtEpoch(epoch uint64, old bool) Backend {
	return swapMoved{s.Mem.AtEpoch(epoch, old).(*Mem)}
}

func (s swapMoved) SwapKey(context.Context, string, string, bool, string) (bool, error) {
	return false, nil
}

// ResetStats doc: a store whose compare-and-set keeps losing errors after its tries,
// saying the record kept changing (stats_reset.go:97).
func TestAResetWhoseSwapAlwaysLosesErrorsAfterItsTries(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 2, t0.Add(-48*time.Hour))
	st := *h.st
	st.B = swapMoved{h.m}
	_, err := st.ResetStats(h.ctx, ResetReq{Reason: "count from now"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kept changing")
	m, err := h.st.StatsReset(h.ctx)
	require.NoError(t, err)
	assert.Nil(t, m, "nothing written")
}

// ResetStats doc: the record's tidy (its time, kinds, bases and archives) is kept as it
// is at the write (stats_reset.go:97).
func TestATidysKindsAndArchivesAreKeptAcrossAReset(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	seedDone(t, h, "m1", 13, t0.Add(-48*time.Hour))
	tidy, err := h.st.TidyStats(h.ctx, TidyReq{Kinds: []string{sprint.TidyFleet}, Reason: "history"})
	require.NoError(t, err)
	before, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	h.tick(time.Minute)
	_, err = h.st.ResetStats(h.ctx, ResetReq{Reason: "count from now"})
	require.NoError(t, err)
	after, err := h.st.StatsTidied(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, before.Kinds, after.Kinds, "the kinds are kept")
	assert.Equal(t, []string{tidy.Archive}, after.Archives, "the archives are kept")
}
