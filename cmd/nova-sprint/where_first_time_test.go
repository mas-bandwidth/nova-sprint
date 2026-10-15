package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWhereShowsFirstTimeLandRateAndTarget pins item (c): nova-sprint where shows a
// "first time" column on the work table (rate and count, "-" for none) and a line under
// the work table "first time: <overall> target 90%" naming the streams under target, and
// --json carries the same on the work rows and the top-level view.
func TestWhereShowsFirstTimeLandRateAndTarget(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream s1 --count 2 --brief-file " + proBriefFile(t))
	ta.ok("add --stream s2 --count 1 --one --brief-file " + proBriefFile(t))
	for _, id := range []string{"s1-1", "s1-2", "s2-1"} {
		ta.tierNow(id, "pro")
	}
	ta.ok("start")

	// Before any card is counted, both streams show "-" and the summary line shows "- target 90%".
	out := ta.ok("where")
	var headerLine, summaryLine string
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "work") && strings.Contains(trim, "|"):
			headerLine = trim
		case strings.HasPrefix(trim, "first time:"):
			summaryLine = trim
		}
	}
	require.NotEmpty(t, headerLine)
	assert.Contains(t, headerLine, "first time")
	assert.Contains(t, summaryLine, "first time: - target 90%")

	// In s1: s1-1 lands on attempt 1 (first-time); s1-2 fails attempt 1, is reworked and lands on attempt 2.
	ta.ok("tick")
	ta.ok("take --as m1 s1-1.w1@1 s1-2.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1 --usage 'input=10 actual_usd=0.01 actual_by=harness'")
	ta.ok("finish --as m1 s1-2.w1@1 --failed --report 'verdict not-done; test failed' --usage 'input=10 actual_usd=0.01 actual_by=harness'")
	ta.ok("rework s1-2 --fix 'fix test'")
	ta.ok("tick")
	ta.ok("take --as m1 s1-2.w2@1")
	ta.ok("finish --as m1 s1-2.w2@1 --usage 'input=10 actual_usd=0.01 actual_by=harness'")
	ta.ok("ask")
	for _, who := range []string{"reader-a", "reader-b"} {
		for _, rc := range ta.askedOf(who) {
			ta.ok("read --as " + who + " --ok " + rc + " --usage 'input=1 actual_usd=0.001 actual_by=harness'")
		}
	}
	ta.ok("accept --stream s1")
	ta.ok("merge --stream s1")
	ta.ok("tick")

	out = ta.ok("where")
	var s1Line, s2Line string
	summaryLine = ""
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "s1") && strings.Contains(trim, "|"):
			s1Line = trim
		case strings.HasPrefix(trim, "s2") && strings.Contains(trim, "|"):
			s2Line = trim
		case strings.HasPrefix(trim, "first time:"):
			summaryLine = trim
		}
	}
	assert.Contains(t, s1Line, "50% 1/2")
	assert.Contains(t, s2Line, "-")
	assert.Contains(t, summaryLine, "first time: 50% 1/2 target 90%")
	assert.Contains(t, summaryLine, "s1")

	jout := ta.ok("where --json")
	var doc struct {
		Tables          map[string]map[string]map[string]string `json:"tables"`
		FirstTime       string                                  `json:"first_time"`
		FirstTimeTarget int                                     `json:"first_time_target"`
		UnderTarget     []string                                `json:"first_time_under_target"`
	}
	require.NoError(t, json.Unmarshal([]byte(jout), &doc))
	assert.Equal(t, "50% 1/2", doc.Tables["work"]["s1"]["first time"])
	assert.Equal(t, "-", doc.Tables["work"]["s2"]["first time"])
	assert.Equal(t, "50% 1/2", doc.FirstTime)
	assert.Equal(t, 90, doc.FirstTimeTarget)
	assert.Equal(t, []string{"s1"}, doc.UnderTarget)
}

// TestWhereFirstTimeLandRateAtFiveHundredPlusCardsCountsExactAttemptOneOnly proves that on a
// sprint with >= 500 cards (where facts.SeriesCounted is set), nova-sprint where and
// where --json compute the exact sprint.FirstTimeOf rate and never treat a primary that
// required rework / multiple attempts as a first-time landing.
func TestWhereFirstTimeLandRateAtFiveHundredPlusCardsCountsExactAttemptOneOnly(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream s1 --count 2 --brief-file " + proBriefFile(t))
	ta.ok("add --stream s2 --sentinel gate2 --held")
	ta.ok("add --stream s2 --count 500 --brief-file " + proBriefFile(t))
	for _, id := range []string{"s1-1", "s1-2"} {
		ta.tierNow(id, "pro")
	}
	ta.ok("start")

	ta.ok("tick")
	ta.ok("take --as m1 s1-1.w1@1 s1-2.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1 --usage 'input=10 actual_usd=0.01 actual_by=harness'")
	ta.ok("finish --as m1 s1-2.w1@1 --failed --report 'verdict not-done; test failed' --usage 'input=10 actual_usd=0.01 actual_by=harness'")
	ta.ok("rework s1-2 --fix 'fix test'")
	ta.ok("tick")
	ta.ok("take --as m1 s1-2.w2@1")
	ta.ok("finish --as m1 s1-2.w2@1 --usage 'input=10 actual_usd=0.01 actual_by=harness'")
	ta.ok("ask")
	for _, who := range []string{"reader-a", "reader-b"} {
		for _, rc := range ta.askedOf(who) {
			ta.ok("read --as " + who + " --ok " + rc + " --usage 'input=1 actual_usd=0.001 actual_by=harness'")
		}
	}
	ta.ok("accept --stream s1")
	ta.ok("merge --stream s1")
	ta.ok("tick")

	checkExact := func(stage string) {
		jout := ta.ok("where --json")
		var doc struct {
			All             int64                                   `json:"all"`
			Tables          map[string]map[string]map[string]string `json:"tables"`
			FirstTime       string                                  `json:"first_time"`
			FirstTimeTarget int                                     `json:"first_time_target"`
			UnderTarget     []string                                `json:"first_time_under_target"`
		}
		require.NoError(t, json.Unmarshal([]byte(jout), &doc), stage)
		require.GreaterOrEqual(t, doc.All, int64(500), stage)
		assert.Equal(t, "50% 1/2", doc.Tables["work"]["s1"]["first time"], stage)
		assert.Equal(t, "-", doc.Tables["work"]["s2"]["first time"], stage)
		assert.Equal(t, "50% 1/2", doc.FirstTime, stage)
		assert.Equal(t, 90, doc.FirstTimeTarget, stage)
		assert.Equal(t, []string{"s1"}, doc.UnderTarget, stage)
	}

	checkExact("after tick with WhereRecord and SeriesCounted")
	ta.ok("add --stream s2 --count 1 --one --brief-file " + proBriefFile(t))
	checkExact("after work revision advance with SeriesCounted")
}

