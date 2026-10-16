package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// sentinelApp is a sprint whose stream live has one of its two cards landed on m1, and, with
// sentinels, a sentinel released into landed (gate) and one still waiting (later) in live.
func sentinelApp(t *testing.T, sentinels bool) *testApp {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	if sentinels {
		// first in live's line, so it waits on no card and is released into landed at once
		ta.ok("add --stream live --sentinel gate")
		ta.ok("release gate --reason 'nothing waits on it'")
	}
	ta.ok("add --stream live --count 2 --brief-file " + proBriefFile(t))
	ta.tierNow("live-1", "pro")
	ta.tierNow("live-2", "pro")
	ta.ok("start")
	ta.landStream("live", []string{"input=10 actual_usd=4 actual_by=harness"}, []string{""}, []string{""})
	if sentinels {
		ta.ok("add --stream live --sentinel later") // waits on live-2: stays in waiting
	}
	ta.ok("tick")
	return ta
}

// A sentinel is counted nowhere (the owner, 2026-10-14: sentinels are excluded everywhere):
// with a landed sentinel and a waiting one in stream live, where --json's work table, the
// headline, the ready buffer, the cards by tier, the stream's tiers, the landed series, the
// stage times and the cards left per release are those of the same sprint without them, and
// the drawn work table's row and footer count the two real cards alone. On the in-memory
// store with the test clock: no real time, socket or service.
func TestSentinelsAreCountedNowhere(t *testing.T) {
	t.Parallel()
	with := sentinelApp(t, true)
	without := sentinelApp(t, false)

	var want, v whereView
	without.json("where", &want)
	with.json("where", &v)
	// gate landed, later waiting (and any stop the machine adds behind them): sentinels all
	require.Subset(t, sentinelPrimariesOf(t, with), []string{"gate", "later"})
	require.Empty(t, sentinelPrimariesOf(t, without))
	require.Equal(t, sprint.Landed, with.primary("gate").Col, "gate is a landed sentinel")

	assert.Equal(t, want.Tables["work"]["live"], v.Tables["work"]["live"], "live's cells count its two cards alone")
	assert.Equal(t, "1", v.Tables["work"]["live"]["landed"], "the released sentinel is no landed card")
	assert.Equal(t, [2]int64{want.Landed, want.All}, [2]int64{v.Landed, v.All}, "the headline")
	assert.Equal(t, [2]int64{1, 2}, [2]int64{v.Landed, v.All})
	assert.Equal(t, want.Ready, v.Ready)
	assert.Equal(t, want.Tiers, v.Tiers)
	assert.Equal(t, want.StreamCosts["live"].Tiers, v.StreamCosts["live"].Tiers)
	require.NotNil(t, v.LandedSeries)
	assert.Equal(t, want.LandedSeries.Totals, v.LandedSeries.Totals, "the series counts no sentinel")
	assert.Equal(t, want.StageTimes, v.StageTimes)
	assert.Equal(t, want.Releases, v.Releases)

	// the drawn table: live's row and the footer count its two cards
	row := func(ta *testApp) string {
		for _, l := range strings.Split(ta.ok("where"), "\n") {
			if strings.HasPrefix(l, "live ") {
				return l
			}
		}
		return ""
	}
	require.NotEmpty(t, row(with))
	assert.Equal(t, row(without), row(with), "the drawn row")
	assert.Equal(t, without.workFooter(), with.workFooter(), "the drawn footer")
	with.clean()
	without.clean()
}

// sentinelPrimariesOf is the sentinel primaries the sprint's rows hold, by id.
func sentinelPrimariesOf(t *testing.T, ta *testApp) []string {
	t.Helper()
	var v whereView
	ta.json("where --rows", &v)
	var ids []string
	for _, r := range v.Rows {
		if r.Fields["kind"] == "sentinel" {
			ids = append(ids, r.ID)
		}
	}
	slices.Sort(ids)
	return ids
}
