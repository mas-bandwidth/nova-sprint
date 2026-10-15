package sprint

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The route table tests drive RouteTable (internal/sprint/routetable.go:25), the
// table `nova-sprint stats --routes` prints (docs/SPEC-SPRINT.md, the verb
// stats), from inside its own package. They build the log lines the test needs
// and assert on the printed rows and the header line only: no unexported helper
// of routetable.go is named.

// rtSince is the window every test here runs since.
var rtSince = time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)

// rtStamp renders an instant the way a log line's stamp is written.
func rtStamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// rtUsageLine is a usage line in the shape parseTake and cardcost.ParseUsage
// read (routetable.go:13-24): a route, a wall and run, and an actual_usd when
// one is named.
func rtUsageLine(route string, wall int, usd string) string {
	s := "wall=" + strconv.Itoa(wall) + "s run=" + strconv.Itoa(wall) + "s"
	if route != "" {
		s = "price_route=" + route + " " + s
	}
	if usd != "" {
		s += " actual_usd=" + usd
	}
	return s + " input=10 output=1"
}

// rtFinish is a fleet finish line for card at a generation.
func rtFinish(at time.Time, card string, gen int, set map[string]string) Line {
	return Line{At: at, Table: Fleet, Verb: "finish", Card: card, Gen: gen, Set: set}
}

// rtRows is the table's rows by their first field: a route's name, or FLASH and
// PRO for the tier rows.
func rtRows(out string) map[string][]string {
	m := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		m[f[0]] = f
	}
	return m
}

// TestAFinishBeforeTheWindowIsLeftOutButItsDealTakeAndAcceptStillCount pins the
// window (routetable.go:33-40): a finish before since is left out with the
// provider takes on its line, while a deal, take or accept before since still
// gives the route and wall of a finish inside the window.
func TestAFinishBeforeTheWindowIsLeftOutButItsDealTakeAndAcceptStillCount(t *testing.T) {
	t.Parallel()
	early := rtSince.Add(-2 * time.Hour)
	inside := rtSince.Add(time.Hour)
	lines := []Line{
		rtFinish(rtSince.Add(-time.Hour), "old.w1", 1, map[string]string{
			"ok":    "yes",
			"usage": rtUsageLine("flash-a", 999, "99.00"),
			FieldProviderTake + "1": ProviderTake{
				Route: "flash-a", Error: "server_error", Usage: "actual_usd=9.00 price_route=flash-a",
			}.String(),
		}),
		{At: early, Table: Fleet, Verb: "tick deal", Card: "bare.w1", Set: map[string]string{"route": "flash-b", "gen": "1"}},
		{At: early, Table: Fleet, Verb: "take", Card: "bare.w1", Gen: 1, Set: map[string]string{"taken": rtStamp(early)}},
		rtFinish(inside, "bare.w1", 1, map[string]string{"ok": "no", "finished": rtStamp(inside)}),
	}
	out := RouteTable(lines, rtSince)
	assert.Contains(t, out, "ROUTE TABLE since 2026-10-04T00:00:00Z")
	assert.NotContains(t, out, "99.00", "a finish before since is outside the window")
	assert.NotContains(t, out, "9.00", "a provider take on a finish before since is outside the window")
	assert.NotContains(t, out, "flash-a", "the excluded finish's route is not in the table")
	rows := rtRows(out)
	require.Contains(t, rows, "flash-b", out)
	assert.Equal(t, "1", rows["flash-b"][1], "the in-window finish counts on the pre-window deal's route")
	assert.Equal(t, "10800s", rows["flash-b"][11], "its wall is the pre-window take to the finish")
}

// TestAFinishCountsOkYesAsOkAndEveryOtherOkAsFailed pins the ok/failed split
// (routetable.go:53-57) and the takes line (routetable.go:13-24): ok=yes is ok,
// any other ok is failed, and takes is ok+failed+noRes, never a provider
// failure.
func TestAFinishCountsOkYesAsOkAndEveryOtherOkAsFailed(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	lines := []Line{
		rtFinish(at, "a.w1", 1, map[string]string{
			"ok": "yes", "usage": rtUsageLine("flash-a", 10, ""),
			FieldProviderTake + "1": ProviderTake{Route: "flash-a", Error: "server_error"}.String(),
		}),
		rtFinish(at, "a.w2", 2, map[string]string{"ok": "no", "usage": rtUsageLine("flash-a", 20, "")}),
		rtFinish(at, "a.w3", 3, map[string]string{"ok": "maybe", "usage": rtUsageLine("flash-a", 30, "")}),
	}
	rows := rtRows(RouteTable(lines, rtSince))
	require.Contains(t, rows, "flash-a")
	assert.Equal(t, "3", rows["flash-a"][1], "takes is the finishes that ran, not the provider take")
	assert.Equal(t, "1", rows["flash-a"][2], "ok=yes is the only ok")
	assert.Equal(t, "2", rows["flash-a"][3], "ok=no and ok=maybe are both failed")
	assert.Equal(t, "0", rows["flash-a"][4])
	assert.Equal(t, "1", rows["flash-a"][5], "the provider take is the prov column")
}

// TestAFinishWithNoRouteUsesTheDealtRouteWhenItRanAMinuteOrFailed pins the "-"
// rule (routetable.go:66-72): a usage naming no route counts on the dealt route
// when it ran at least 60 s or failed, otherwise on "-", which prints only the
// "finishes ran no model" line and no row.
func TestAFinishWithNoRouteUsesTheDealtRouteWhenItRanAMinuteOrFailed(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	deal := func(card string) Line {
		return Line{At: rtSince.Add(30 * time.Second), Table: Fleet, Verb: "tick deal", Card: card, Set: map[string]string{"route": "flash-d", "gen": "1"}}
	}
	lines := []Line{
		deal("d1.w1"),
		rtFinish(at, "d1.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("", 60, "")}),
		deal("d2.w1"),
		rtFinish(at, "d2.w1", 1, map[string]string{"ok": "no", "usage": rtUsageLine("", 1, "")}),
		deal("d3.w1"),
		rtFinish(at, "d3.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("", 1, "")}),
		rtFinish(at, "d4.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("", 1, "")}),
	}
	out := RouteTable(lines, rtSince)
	rows := rtRows(out)
	require.Contains(t, rows, "flash-d", out)
	assert.Equal(t, "2", rows["flash-d"][1], "the 60 s finish and the failed finish count on the deal")
	assert.Equal(t, "1", rows["flash-d"][2])
	assert.Equal(t, "1", rows["flash-d"][3])
	assert.Contains(t, out, "(2 finishes ran no model", "the two short ok finishes went to the \"-\" route")
	assert.NotRegexp(t, "(?m)^- ", out, "the \"-\" route prints no row")
}

// TestAFinishWithNoUsageTakesItsRunFromTakeToFinish pins the no-usage wall
// (routetable.go:58-65): its run is its take's taken stamp to its finished
// stamp.
func TestAFinishWithNoUsageTakesItsRunFromTakeToFinish(t *testing.T) {
	t.Parallel()
	taken := rtSince.Add(time.Minute)
	finished := taken.Add(90 * time.Second)
	lines := []Line{
		{At: rtSince.Add(30 * time.Second), Table: Fleet, Verb: "tick deal", Card: "e1.w1", Set: map[string]string{"route": "flash-e", "gen": "1"}},
		{At: taken, Table: Fleet, Verb: "take", Card: "e1.w1", Gen: 1, Set: map[string]string{"taken": rtStamp(taken)}},
		rtFinish(finished, "e1.w1", 1, map[string]string{"ok": "yes", "finished": rtStamp(finished)}),
	}
	rows := rtRows(RouteTable(lines, rtSince))
	require.Contains(t, rows, "flash-e")
	assert.Equal(t, "1", rows["flash-e"][1])
	assert.Equal(t, "90s", rows["flash-e"][11], "run and wall are taken to finished")
}

// TestANoResultProviderTakeCountsInNoResAndTakes pins the provider take's kind
// (routetable.go:78-92): a no-result counts in noRes and takes, any other
// provider take counts in prov only.
func TestANoResultProviderTakeCountsInNoResAndTakes(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	noResult := ProviderTake{
		Route: "flash-f", Model: "p/m", Member: "m1", Finished: rtStamp(at),
		Usage: "wall=30s run=30s price_route=flash-f input=0 output=0",
		Error: cardhdr.EndNoResult + ": the child wrote nothing",
	}.String()
	lines := []Line{
		rtFinish(at, "g1.w1", 1, map[string]string{
			"ok": "yes", "usage": rtUsageLine("flash-f", 5, ""),
			FieldProviderTake + "1": noResult,
		}),
		rtFinish(at, "g2.w1", 1, map[string]string{
			"ok": "yes", "usage": rtUsageLine("flash-f", 5, ""),
			FieldProviderTake + "1": ProviderTake{Route: "flash-f", Error: "server_error"}.String(),
		}),
	}
	rows := rtRows(RouteTable(lines, rtSince))
	require.Contains(t, rows, "flash-f")
	assert.Equal(t, "3", rows["flash-f"][1], "two ok finishes and the no-result are takes")
	assert.Equal(t, "2", rows["flash-f"][2])
	assert.Equal(t, "1", rows["flash-f"][4], "the no-result is a noRes")
	assert.Equal(t, "1", rows["flash-f"][5], "the other provider take is a prov")
}

// TestMoreThanFiftyProviderFailuresOnAPrimaryIsAStormCountedOnce pins the storm
// (routetable.go:159-176): more than fifty provider failures on one primary is
// counted once per route, and the header names it and the storm total.
func TestMoreThanFiftyProviderFailuresOnAPrimaryIsAStormCountedOnce(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	var lines []Line
	for i := 0; i < 51; i++ {
		lines = append(lines, rtFinish(at, "stormy.w"+strconv.Itoa(i+1), 1, map[string]string{
			"ok": "yes", "usage": rtUsageLine("flash-g", 10, ""),
			FieldProviderTake + "1": ProviderTake{Route: "flash-g", Error: "server_error"}.String(),
		}))
	}
	out := RouteTable(lines, rtSince)
	assert.Contains(t, out, "with the 51 on the storm cards stormy counted once per route")
	rows := rtRows(out)
	require.Contains(t, rows, "flash-g", out)
	assert.Equal(t, "1", rows["flash-g"][5], "a storm's provider failures count once per route")
	assert.Equal(t, "51", rows["flash-g"][1], "the ok finishes still count")
}

// TestExactlyFiftyProviderFailuresIsNotAStorm pins the storm's boundary
// (routetable.go:161-166): exactly fifty provider failures is not a storm.
func TestExactlyFiftyProviderFailuresIsNotAStorm(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	var lines []Line
	for i := 0; i < 50; i++ {
		lines = append(lines, rtFinish(at, "fifty.w"+strconv.Itoa(i+1), 1, map[string]string{
			"ok": "yes", "usage": rtUsageLine("flash-h", 10, ""),
			FieldProviderTake + "1": ProviderTake{Route: "flash-h", Error: "server_error"}.String(),
		}))
	}
	out := RouteTable(lines, rtSince)
	assert.Contains(t, out, "with the 0 on the storm cards", "no primary is a storm")
	assert.NotContains(t, out, "storm cards fifty")
	rows := rtRows(out)
	require.Contains(t, rows, "flash-h", out)
	assert.Equal(t, "50", rows["flash-h"][5], "exactly fifty provider failures count each")
}

// TestALandingCreditsTheLatestOkTakeAtOrBeforeTheAccept pins landed and 1stT
// (routetable.go:103-133, 211-226): a landing credits the latest ok take at or
// before the accept (a take up to 2 s after still counts), and 1stT is the
// share that were the primary's first non-provider take on the tier.
func TestALandingCreditsTheLatestOkTakeAtOrBeforeTheAccept(t *testing.T) {
	t.Parallel()
	lines := []Line{
		rtFinish(rtSince.Add(time.Minute), "p1.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-i", 10, "")}),
		{At: rtSince.Add(2 * time.Minute), Table: Work, Verb: "accept", Primary: "p1"},
		rtFinish(rtSince.Add(10*time.Minute), "p1.w2", 2, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-i", 20, "")}),
		{At: rtSince.Add(30 * time.Minute), Table: Work, To: "s1:landed", Primary: "p1"},
		rtFinish(rtSince.Add(2*time.Minute+time.Second), "p2.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-j", 10, "")}),
		{At: rtSince.Add(2 * time.Minute), Table: Work, Verb: "tick accept", Primary: "p2"},
		{At: rtSince.Add(30 * time.Minute), Table: Work, To: "s1:landed", Primary: "p2"},
	}
	rows := rtRows(RouteTable(lines, rtSince))
	require.Contains(t, rows, "flash-i", "the latest ok take at or before the accept lands")
	assert.Equal(t, "1", rows["flash-i"][6])
	assert.Equal(t, "100%", rows["flash-i"][7])
	require.Contains(t, rows, "flash-j", "a take up to 2 s after a tick accept still lands")
	assert.Equal(t, "1", rows["flash-j"][6])
	assert.Equal(t, "100%", rows["flash-j"][7])
}

// TestABrokenVerdictShowsAsWrongReviewedAndWinsOverAnEarlierOne pins wrong
// (routetable.go:95-102, 202-209, 350-354): a reader's broken verdict on a
// reviewed ok take shows as wrong/reviewed, a broken verdict wins over an
// earlier non-broken one for the same attempt, and with nothing reviewed the
// column is "-".
func TestABrokenVerdictShowsAsWrongReviewedAndWinsOverAnEarlierOne(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	read := func(reader, verdict string, when time.Time) Line {
		return Line{At: when, Table: Readers, Verb: "read", Card: reader, Set: map[string]string{"verdict": verdict}}
	}
	lines := []Line{
		rtFinish(at, "q1.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-i1", 10, "")}),
		read("q1.r1.reader-a", "good", at.Add(time.Minute)),
		read("q1.r1.reader-a", "broken", at.Add(2*time.Minute)),
		rtFinish(at, "q2.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-i2", 10, "")}),
		read("q2.r1.reader-a", "good", at.Add(time.Minute)),
		rtFinish(at, "q3.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-i3", 10, "")}),
	}
	rows := rtRows(RouteTable(lines, rtSince))
	require.Contains(t, rows, "flash-i1")
	assert.Equal(t, "1/1", rows["flash-i1"][8], "the broken verdict wins over the earlier good one")
	assert.Equal(t, "0/1", rows["flash-i2"][8], "a good verdict is reviewed but not wrong")
	assert.Equal(t, "-", rows["flash-i3"][8], "nothing reviewed prints -")
}

// TestAPricedTakeCountsItsUsdAndAnUnpricedLongTakeIsImputedAtTheRouteMean pins
// the money columns (routetable.go:144-152, 190-198, 355-368): a priced take
// adds its actual_usd, an unpriced take over a minute is imputed at the route's
// mean priced take and counted in imp, $/land is "-" with no landing, and the
// median wall is the upper median.
func TestAPricedTakeCountsItsUsdAndAnUnpricedLongTakeIsImputedAtTheRouteMean(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	lines := []Line{
		rtFinish(at, "m1.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-k", 10, "2.00")}),
		rtFinish(at, "m2.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-k", 70, "")}),
	}
	rows := rtRows(RouteTable(lines, rtSince))
	require.Contains(t, rows, "flash-k")
	assert.Equal(t, "2", rows["flash-k"][1])
	assert.Equal(t, "2.00", rows["flash-k"][9], "the priced take and its imputed twin")
	assert.Equal(t, "-", rows["flash-k"][10], "$/land is - with no landing")
	assert.Equal(t, "70s", rows["flash-k"][11], "the upper median of 10s and 70s")
	assert.Equal(t, "1", rows["flash-k"][12], "the over-a-minute unpriced take is imputed")
}

// TestTheFlashAndProRowsSumTheirRoutes pins the tier rows
// (routetable.go:244-269): FLASH (all) and PRO (all) sum every route whose name
// starts flash- or pro-, and a route of another tier is in neither.
func TestTheFlashAndProRowsSumTheirRoutes(t *testing.T) {
	t.Parallel()
	at := rtSince.Add(time.Minute)
	lines := []Line{
		rtFinish(at, "n1.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-a", 10, "")}),
		rtFinish(at, "n2.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("flash-b", 20, "")}),
		rtFinish(at, "n3.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("pro-a", 30, "")}),
		rtFinish(at, "n4.w1", 1, map[string]string{"ok": "yes", "usage": rtUsageLine("heavy-a", 40, "")}),
	}
	rows := rtRows(RouteTable(lines, rtSince))
	require.Contains(t, rows, "FLASH", "the FLASH (all) row is printed")
	require.Contains(t, rows, "PRO", "the PRO (all) row is printed")
	assert.Equal(t, "FLASH", rows["FLASH"][0])
	assert.Equal(t, "2", rows["FLASH"][2], "both flash- routes are summed")
	assert.Equal(t, "1", rows["PRO"][2], "the pro- route is summed")
	assert.Equal(t, "1", rows["heavy-a"][1], "the heavy- route prints its own row")
}
