package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Archived work is in no total (the owner, 2026-10-14: "archived cards and work streams
// should not show up in totals, dashboard, costs, anywhere"): with one stream archived and
// one live, every total where --json carries is the live stream's alone, with --archived
// too, and the archived stream's figures are under archived only. On the in-memory store,
// time passed in by the test app's clock: no real time, socket or service.
func TestWhereTotalsLeaveArchivedStreamsOut(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream gone --count 2 --brief-file " + proBriefFile(t))
	ta.ok("add --one --stream live --count 1 --brief-file " + writeBrief(t, "a flash card, tier: flash"))
	for _, id := range []string{"gone-1", "gone-2"} {
		ta.tierNow(id, "pro")
	}
	ta.ok("start")
	ta.landStream("gone", []string{"input=10 actual_usd=1 actual_by=harness", "input=10 actual_usd=2 actual_by=harness"}, []string{"", ""}, []string{"", ""})
	ta.ok("tick")

	var before whereView
	ta.json("where", &before)
	require.Nil(t, before.Archived)
	require.Contains(t, before.StreamCosts, "gone", "on the table, its costs are in stream_costs")
	require.NotNil(t, before.CostCheck)
	require.Equal(t, "3", before.CostCheck.Streams, "the epoch's spend before the archive")
	require.Equal(t, 2, before.Tiers["pro"])

	assert.Contains(t, ta.ok("stream archive gone"), "STREAM-ARCHIVE OK streams=gone")
	for _, args := range []string{"where", "where --archived", "where --rows"} {
		var v whereView
		ta.json(args, &v)
		require.NotNil(t, v.Archived, args)
		assert.Equal(t, archivedView{Streams: []string{"gone"}, Cards: 2, Landed: 2, Cost: "$3.00"}, *v.Archived, args+": archived carries the archived stream's figures")

		// the headline: the live stream's one card, none landed
		assert.Equal(t, [2]int64{0, 1}, [2]int64{v.Landed, v.All}, args)
		// costs: no archived stream in stream_costs, its dollars off both sums of the check
		assert.NotContains(t, v.StreamCosts, "gone", args)
		for k := range v.StreamCosts {
			assert.Equal(t, "live", k, args)
		}
		require.NotNil(t, v.CostCheck, args)
		assert.Equal(t, "0", v.CostCheck.Streams, args)
		assert.Equal(t, "0", v.CostCheck.Cards, args)
		assert.True(t, v.CostCheck.OK, args)
		assert.NotContains(t, v.ReadSpend, "$", args+": no archived read in the day's spend")
		// cards by tier: the live stream's flash card alone
		assert.Equal(t, map[string]int{"flash": 1}, v.Tiers, args)
		// stream counts and per-stream lists: the live stream alone
		for _, c := range v.Streams {
			assert.NotEqual(t, "gone", c.Stream, args+": no archived stream clock")
		}
		assert.NotContains(t, v.Stalled, "gone", args)
		if v.StageTimes != nil {
			assert.NotContains(t, v.StageTimes.Streams, "gone", args)
		}
		assert.NotContains(t, v.StreamPriorities, "gone", args)
	}

	// unarchived, the stream is on the table again and in every total
	ta.ok("stream unarchive gone")
	var back whereView
	ta.json("where", &back)
	assert.Nil(t, back.Archived)
	assert.Equal(t, before.StreamCosts["gone"], back.StreamCosts["gone"])
	assert.Equal(t, before.CostCheck.Streams, back.CostCheck.Streams)
	assert.Equal(t, before.Tiers, back.Tiers)
	assert.True(t, hasClock(back.Streams, "gone"))
	ta.clean()
}

func hasClock(cs []sprint.StreamClock, stream string) bool {
	for _, c := range cs {
		if c.Stream == stream {
			return true
		}
	}
	return false
}

// liveCostCheck takes the archived streams' exact spend off both sums and keeps the gap.
func TestLiveCostCheckTakesTheArchivedSpendOffBothSums(t *testing.T) {
	t.Parallel()
	k := &sprint.CostCheck{Streams: "10.5", Cards: "10.75", Gap: "0.25", OK: false}
	streams := map[string]sprint.TierCosts{"a": {TotalUSD: "4.25", Tiers: map[string]int{"pro": 2}}, "b": {TotalUSD: "6.25", Tiers: map[string]int{"pro": 1, "flash": 3}}}
	gone := &archivedView{Streams: []string{"a"}}
	got := liveCostCheck(k, streams, gone)
	assert.Equal(t, "6.25", got.Streams)
	assert.Equal(t, "6.5", got.Cards)
	assert.Equal(t, "0.25", got.Gap)
	assert.Equal(t, "10.5", k.Streams, "the record's check is not changed")
	assert.Equal(t, map[string]int{"pro": 1, "flash": 3}, liveTiers(map[string]int{"pro": 3, "flash": 3}, streams, gone))
	assert.Equal(t, map[string]sprint.TierCosts{"b": streams["b"]}, liveStreamCosts(streams, gone))
	assert.Same(t, k, liveCostCheck(k, streams, nil), "no archived stream: the check as it is")
}
