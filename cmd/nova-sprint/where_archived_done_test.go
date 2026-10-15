package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// archivedDoneApp is a sprint whose stream live has one of its two cards landed on m1, and, with gone,
// a stream gone of two cards landed on m1 before it, the same steps at the same test clock.
func archivedDoneApp(t *testing.T, gone bool) *testApp {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	if gone {
		ta.ok("add --stream gone --count 2 --brief-file " + proBriefFile(t))
		ta.tierNow("gone-1", "pro")
		ta.tierNow("gone-2", "pro")
	}
	// live keeps a card not landed, so the sprint is not done (a sprint done counts the epoch)
	ta.ok("add --stream live --count 2 --brief-file " + proBriefFile(t))
	ta.tierNow("live-1", "pro")
	ta.tierNow("live-2", "pro")
	ta.ok("start")
	if gone {
		ta.landStream("gone", []string{"input=10 actual_usd=1 actual_by=harness", "input=10 actual_usd=2 actual_by=harness"}, []string{"", ""}, []string{"", ""})
	}
	ta.landStream("live", []string{"input=10 actual_usd=4 actual_by=harness"}, []string{""}, []string{""})
	return ta
}

// The rows' done and ok%, the landed series and the stage times leave an archived stream
// out (the owner, 2026-10-14: "archived cards and work streams should not show up in totals,
// dashboard, costs, anywhere"): with stream gone archived, the fleet table, the series and
// the stage times are those of a sprint that only ever held stream live. On the in-memory
// store with the test clock: no real time, socket or service.
func TestArchivedStreamsLeaveDoneSeriesAndStageTimes(t *testing.T) {
	t.Parallel()
	both := archivedDoneApp(t, true)
	alone := archivedDoneApp(t, false)

	var want whereView
	alone.json("where", &want)
	require.NotNil(t, want.LandedSeries)
	require.Equal(t, 1, want.LandedSeries.Totals.Fleet+want.LandedSeries.Totals.Friends+want.LandedSeries.Totals.Unknown, "live's one landing")

	var before whereView
	both.json("where", &before)
	require.Nil(t, before.Archived)
	require.NotEqual(t, want.Tables["fleet"]["m1"]["done"], before.Tables["fleet"]["m1"]["done"], "on the table, gone's cards are in m1's done")
	require.Equal(t, 3, before.LandedSeries.Totals.Fleet+before.LandedSeries.Totals.Friends+before.LandedSeries.Totals.Unknown)

	both.ok("stream archive gone")
	both.ok("tick")
	var v whereView
	both.json("where", &v)
	require.NotNil(t, v.Archived)
	assert.Equal(t, []string{"gone"}, v.Archived.Streams)
	// what the tick counted of gone: its two primaries and their two ok work cards on m1, each
	// with its finish stamp (so a stats reset's mark can tell them apart)
	ctx := context.Background()
	st, err := both.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	pinned, err := st.Pinned(ctx)
	require.NoError(t, err)
	shapes, err := pinned.B.Shapes(ctx, []string{pinned.Names.Table(sprint.Work)})
	require.NoError(t, err)
	facts, err := pinned.WhereFacts(ctx, shapes[0].Revision)
	require.NoError(t, err)
	require.True(t, facts.ArchivedCounted, "the record taken counted the archived streams")
	assert.Equal(t, []string{"gone-1", "gone-2"}, facts.ArchivedPrimaries)
	require.Len(t, facts.ArchivedFleet, 2)
	for _, c := range facts.ArchivedFleet {
		assert.Equal(t, [2]string{"m1", sprint.DoneOK}, [2]string{c.Row, c.Col})
		assert.NotZero(t, c.At, "the work card's finish stamp")
	}
	for _, col := range []string{"done", sprint.DoneOK, sprint.DoneFailed, "ok%"} {
		assert.Equal(t, want.Tables["fleet"]["m1"][col], v.Tables["fleet"]["m1"][col], "m1's %s is live's alone", col)
	}
	assert.Equal(t, want.Tables["friends"], v.Tables["friends"], "the friends' done and ok% likewise")
	require.NotNil(t, v.LandedSeries)
	assert.Equal(t, want.LandedSeries.Totals, v.LandedSeries.Totals, "the series counts live's landing alone")
	assert.Equal(t, want.LandedSeries.Fleet, v.LandedSeries.Fleet)
	assert.Equal(t, want.StageTimes, v.StageTimes, "the stage times are live's alone, overall and by stream")

	// unarchived, gone is in every figure again
	both.ok("stream unarchive gone")
	both.ok("tick")
	var back whereView
	both.json("where", &back)
	assert.Equal(t, before.Tables["fleet"]["m1"]["done"], back.Tables["fleet"]["m1"]["done"])
	assert.Equal(t, before.LandedSeries.Totals, back.LandedSeries.Totals)
	both.clean()
	alone.clean()
}

// lessArchivedDone takes off each row's archived ok and failed cards finished after the
// stats reset's mark (all of them with no mark), never below zero, and leaves the rest.
func TestLessArchivedDoneCountsFromTheMark(t *testing.T) {
	t.Parallel()
	mark := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tb := ntable.Table{
		Columns: []ntable.Column{{Name: sprint.DoneOK, Projection: ntable.Count}, {Name: sprint.DoneFailed, Projection: ntable.Count}},
		Rows: []ntable.Row{
			{Key: "m1", Cells: []ntable.Cell{{Count: 5}, {Count: 1}}},
			{Key: "m2", Cells: []ntable.Cell{{Count: 2}, {Count: 0}}},
		},
	}
	cards := []sprint.ArchivedFleetCard{
		{Row: "m1", Col: sprint.DoneOK, At: mark.Add(-time.Hour).Unix()}, // before the mark: not in the cells since it
		{Row: "m1", Col: sprint.DoneOK, At: mark.Add(time.Hour).Unix()},
		{Row: "m1", Col: sprint.DoneFailed, At: mark.Add(time.Minute).Unix()},
		{Row: "m2", Col: sprint.DoneOK},                                     // no stamp: history
		{Row: "m2", Col: sprint.DoneFailed, At: mark.Add(time.Hour).Unix()}, // never below zero
	}
	cell := func(t ntable.Table, row, j int) int64 { return t.Rows[row].Cells[j].Count }

	since := lessArchivedDone(tb, cards, mark)
	assert.Equal(t, [4]int64{4, 0, 2, 0}, [4]int64{cell(since, 0, 0), cell(since, 0, 1), cell(since, 1, 0), cell(since, 1, 1)})
	all := lessArchivedDone(tb, cards, time.Time{})
	assert.Equal(t, [4]int64{3, 0, 1, 0}, [4]int64{cell(all, 0, 0), cell(all, 0, 1), cell(all, 1, 0), cell(all, 1, 1)})
	assert.Equal(t, int64(5), cell(tb, 0, 0), "the table passed in is not changed")
	assert.Equal(t, tb, lessArchivedDone(tb, nil, mark))
}
