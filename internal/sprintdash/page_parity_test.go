package sprintdash

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThePageShowsExactlyTheOwnersElements is the parity check (docs/SPEC-SPRINT-DASHBOARD.md,
// "the page is this list"): the Go page shows exactly the elements the owner asked for since
// 2026-10-11 (P1..P40 of the card the-go-dashboard-is-the-page-glenn-sees), and none of the
// things he removed by name.
//
// The page is rendered through its own path: the fixture where --json (ownerWhere: epoch 20,
// 0/84, STOPPED, 2 machines up, 11 held, 12 friends held or down, 13 streams with display names)
// is read by the Go server, which serves index.html (with its logo slot filled) and the
// /api/sprint envelope; the shipped app.js then renders that envelope on a stubbed DOM (no
// browser, no socket, no real time: the clock is the fixture's at, the zone is fixed). Two
// variants of the same fixture render the forms the DONE-WHEN fixture cannot show: the sprint
// running with spend and cards in flight (P9, P12-P15, P18), and the sprint done (P11). Each is
// rendered wide and on a phone (matchMedia (max-width: 1100px) true, a header too narrow for the
// chips). What only CSS decides (a hidden panel, a compact column, a colour) is asserted on the
// served page's rules, the rule the browser applies.
func TestThePageShowsExactlyTheOwnersElements(t *testing.T) {
	t.Parallel()
	served := servedPage(t, ownerWhere(ownerStopped))
	app := string(file("app.js"))
	envStopped := ownerEnvelope(t, ownerWhere(ownerStopped))
	envRunning := ownerEnvelope(t, ownerWhere(ownerRunning))
	envDone := ownerEnvelope(t, ownerWhere(ownerDone))
	out := renderOwner(t, app, []ownerCase{
		{"stopped", envStopped, false}, {"stopped-phone", envStopped, true},
		{"running", envRunning, false}, {"running-phone", envRunning, true},
		{"done", envDone, false},
	})
	st, stp, run, runp, done := out["stopped"], out["stopped-phone"], out["running"], out["running-phone"], out["done"]
	for name, r := range out {
		require.Empty(t, r.Errors, "%s: the page renders without an error", name)
	}

	// --- removed, each by the owner by name ---
	absent := func(hay, needle, msg string) {
		t.Helper()
		assert.False(t, strings.Contains(hay, needle), "%s: %q is on the page", msg, needle)
	}
	has := func(hay, needle string, msg ...any) {
		t.Helper()
		assert.True(t, strings.Contains(hay, needle), "%v: %q is not on the page", msg, needle)
	}
	matches := func(re, hay string, msg ...any) {
		t.Helper()
		assert.True(t, regexp.MustCompile(re).MatchString(hay), "%v: %s does not match the page", msg, re)
	}
	absent(served, "machine-chip", "the red machine STOPPED chip is gone (2026-10-12 ~00:58Z)")
	absent(served, `id="machine"`, "no machine chip element")
	absent(st.HTML["live-text"], "Updated", "the clock has no Updated (2026-10-12 ~00:32Z)")
	absent(app, `"Updated `, "the clock has no Updated")
	absent(served, `<span class="dot"></span><span id="live-text">`, "the clock has no green dot")
	absent(served, "All cards by state", "Cards by state, not All cards by state")
	absent(app, `"nothing in flight"`, "the in-flight tile is blank, not 'nothing in flight'")
	absent(st.Text["inflight-sub"], "nothing in flight", "the in-flight tile is blank")
	absent(app, "no stream has spent anything yet", "top streams with no spend draw blank ruled rows")
	absent(strings.Join(st.Top[len(st.Top)-1], ""), "spent", "top streams with no spend: no message")
	has(served, ".panel.friends { display: none; }", "the Friends panel is hidden (2026-10-11 ~21:55Z)")

	// P1 logo: the nova-sprint image, inline data PNG, top left
	matches(`<h1><img id="logo" class="logo-tile" src="data:image/png;base64,[A-Za-z0-9+/=]{100,}"`, served, "P1")
	// P2 wordmark "nova-sprint" (Nunito 800), hidden when it does not fit
	has(served, `<span class="wordmark" id="wordmark">nova-sprint</span>`, "P2 wordmark")
	has(served, `"Nunito", system-ui, sans-serif; font-weight: 800;`, "P2 Nunito 800")
	has(served, `.wordmark.nofit { display: none; }`, "P2 hidden when it does not fit")
	assert.False(t, st.Nofit["wordmark"], "P2 shown wide")
	assert.True(t, stp.Nofit["wordmark"], "P2 hidden when it does not fit")
	// P3 chip "version v1.2.16", hidden when unknown
	assert.Equal(t, "v1.2.16", st.Text["version"], "P3")
	assert.False(t, st.Hidden["version-chip"], "P3 shown when known")
	assert.False(t, stp.Hidden["version-chip"], "P3 the version stays on every width")
	has(served, `<span class="chip" id="version-chip" hidden>version <b id="version">-</b></span>`, "P3 hidden until known")
	unknown := renderOwner(t, app, []ownerCase{{"unknown", ownerEnvelopeVersion(t, ownerWhere(ownerStopped), ""), false}})["unknown"]
	assert.True(t, unknown.Hidden["version-chip"], "P3 hidden when unknown")
	// P4 chip "coordinator rowan"; on a phone just "rowan"; hidden when there is no room
	has(served, `<span class="chip opt-chip" id="coord-chip"><span class="coord-label">coordinator</span> <b id="coord">-</b></span>`, "P4")
	assert.Equal(t, "rowan", st.Text["coord"], "P4")
	has(served, `@media (max-width: 600px) { .coord-label { display: none; } }`, "P4 just rowan on a phone")
	assert.False(t, st.Nofit["coord-chip"], "P4 shown wide")
	assert.True(t, stp.Nofit["coord-chip"], "P4 hidden when there is no room")
	// P5 chip "epoch 20"; the first chip hidden when there is no room
	has(served, `<span class="chip opt-chip" id="epoch-chip">epoch <b id="epoch">-</b></span>`, "P5")
	assert.Equal(t, "20", st.Text["epoch"], "P5")
	assert.False(t, st.Nofit["epoch-chip"], "P5 shown wide")
	assert.True(t, stp.Nofit["epoch-chip"], "P5 hidden when there is no room")
	has(served, `.opt-chip.nofit { display: none; }`, "P4 P5")
	assert.Equal(t, []string{"wordmark", "epoch-chip", "coord-chip"}, stp.NofitOrder, "P2 P5 P4 the wordmark goes first, then the epoch, then the coordinator")
	// P6 release nav hidden (the envelope carries no releases)
	has(served, `<nav class="release" id="release" aria-label="release" hidden></nav>`, "P6")
	assert.Equal(t, 0, st.Children["release"], "P6 no release links")
	// P7 clock alone at the far right: "9:52:20 PM EDT", no Updated, no dot
	has(served, `.live { margin-left: auto; }`, "P7 at the far right")
	assert.Equal(t, "9:52:20 PM EDT", ownerTags.ReplaceAllString(st.HTML["live-text"], ""), "P7")
	// P8 Landed tile: the count, "of 84", "0.0% complete"; "not started" when the sprint is empty
	assert.Equal(t, "0", st.Text["landed"], "P8")
	assert.Equal(t, "84", st.Text["all"], "P8 of 84")
	has(served, `<small class="only-wide">of <span id="all">-</span></small>`, "P8 of 84")
	assert.Equal(t, "0.0%", st.Text["pct"], "P8")
	has(served, `<span class="only-wide" id="pct-complete"> complete</span>`, "P8 complete")
	assert.Equal(t, "", st.Display["pct-complete"], "P8 complete shown")
	empty := renderOwner(t, app, []ownerCase{{"empty", ownerEnvelope(t, ownerWhere(ownerEmpty)), false}})["empty"]
	assert.Equal(t, "not started", empty.Text["pct"], "P8 not started when the sprint is empty")
	assert.Equal(t, "none", empty.Display["pct-complete"], "P8 only not started")
	assert.Equal(t, "none", empty.Display["of-all2"], "P8 only not started, narrow")
	// P9 ETA "2h 20m" and "around 12:12 AM EDT", or "-" with "not in the summary"
	assert.Equal(t, `2h<span class="sp"> </span>20m`, run.Val["eta"], "P9")
	assert.Equal(t, "big", run.Class["eta"], "P9")
	assert.Equal(t, "around 12:12 AM EDT", strings.ReplaceAll(run.Text["eta-at"], " ", " "), "P9")
	none := renderOwner(t, app, []ownerCase{{"none", ownerEnvelope(t, ownerWhere(ownerNoETA)), false}})["none"]
	assert.Equal(t, "-", none.Text["eta"], "P9 -")
	assert.Equal(t, "not in the summary", none.Text["eta-at"], "P9")
	// P10 ETA "STOP" in dark red while the machine is STOPPED
	assert.Equal(t, "STOP", st.Text["eta"], "P10")
	assert.Equal(t, "STOP", stp.Text["eta"], "P10 at every width")
	assert.Equal(t, "big eta-stop", st.Class["eta"], "P10")
	has(served, `#eta.eta-stop { color: color-mix(in srgb, var(--critical) 72%, #000); }`, "P10 dark red")
	// P11 ETA "DONE" in green, the time the sprint took below it (done wins over STOPPED)
	assert.Equal(t, "DONE", done.Text["eta"], "P11")
	assert.Equal(t, "big eta-done", done.Class["eta"], "P11")
	assert.Equal(t, "2 hours 10 minutes", done.Text["eta-at"], "P11 the time it took")
	has(served, `#eta.eta-done { color: var(--good); }`, "P11 green")
	// P12 Cost tile: "$0.00" with cents; whole dollars on a phone; "$x per card"
	assert.Equal(t, "$0.00", st.Text["cost"], "P12")
	assert.Equal(t, "$0", st.Text["cost-whole"], "P12 whole dollars on a phone")
	assert.Equal(t, "$3.75", run.Text["cost"], "P12")
	assert.Equal(t, "$4", run.Text["cost-whole"], "P12 rounded up on a phone")
	has(served, `<div class="big"><span class="only-wide" id="cost">-</span><span class="only-narrow" id="cost-whole">-</span></div>`, "P12")
	assert.Equal(t, "$1.88 per card", done.Text["cost-per"], "P12 per card")
	// P13 In flight tile: the count; "n working · n verify"; blank when nothing is in flight
	assert.Equal(t, "0", st.Text["inflight"], "P13")
	assert.Equal(t, " ", st.Text["inflight-sub"], "P13 blank")
	assert.Equal(t, "3", run.Text["inflight"], "P13")
	assert.Equal(t, "2 working · 1 verify", run.Text["inflight-sub"], "P13")
	// P14 Throughput: the figure or "—"; "cards / hour"; a tooltip
	assert.Equal(t, "—", st.Text["tput"], "P14")
	assert.Equal(t, "needs ten minutes of samples", st.Title["tput"], "P14 tooltip")
	has(served, `<div class="sub">cards / hour</div>`, "P14")
	// P15 Cost breakdown head, the tier legend in tier colours, or "-"
	has(served, `<h2>Cost breakdown</h2>`, "P15")
	assert.Equal(t, "-", st.Text["tier-sub"], "P15 - with no spend")
	assert.Equal(t, "pro$2.50flash$1.25", run.Text["tier-sub"], "P15 the legend")
	assert.Equal(t, []string{"var(--tier-pro)", "var(--tier-flash)"}, run.TierSwatches, "P15 tier colours")
	has(served, `--tier-flash: #3b82f6; --tier-pro: #a855f7; --tier-heavy: #f59e0b; --tier-frontier: #ec4899`, "P15 blue purple amber pink")
	// P16 a fold chevron on every panel, state kept in localStorage
	has(served, `document.querySelectorAll(".panel").forEach(function(p){var h=p.querySelector(".panel-head")`, "P16 a chevron on every panel")
	has(served, `"sprint-fold-"`, "P16 fold state kept per panel")
	has(served, `localStorage`, "P16 kept in localStorage")
	has(served, `.panel-head .fold::before { content: "\2303";`, "P16 the chevron")
	// P17 tier pie; grey disc when nothing is spent; round wedge joins
	assert.Equal(t, []string{"circle var(--tier-other)"}, st.Pie, "P17 grey disc")
	assert.Equal(t, []string{"path var(--tier-pro)", "path var(--tier-flash)"}, run.Pie, "P17 wedges")
	has(served, `.pie path { stroke: var(--surface); stroke-width: 1; stroke-linejoin: round; }`, "P17 round joins")
	// P18 top streams: stream | each tier with spend | total; on a phone stream | "cost", the pie centred
	require.NotEmpty(t, run.Top, "P18")
	assert.Equal(t, []string{"stream", "pro", "flash", "total"}, run.Top[0], "P18 wide")
	assert.Equal(t, []string{"schema-c", "$2.50", "$1.25", "$3.75"}, run.Top[1], "P18 a stream's row")
	assert.Equal(t, []string{"stream", "pro", "flash", "cost"}, runp.Top[0], "P18 phone: cost")
	has(served, `.toprows .row > div:not(:first-child):not(:last-child) { display: none; }`, "P18 phone: stream | cost")
	has(served, `.pie { width: 100%; max-width: min(40rem, 100%); margin: 0 auto; }`, "P18 the pie centred")
	// P19 top streams with no spend: blank ruled rows, no message
	require.Greater(t, len(st.Top), 3, "P19 blank rows")
	for _, row := range st.Top[1:] {
		assert.Equal(t, []string{" ", " "}, row, "P19 a blank ruled row")
	}
	assert.Equal(t, "row blank", st.TopClass[1], "P19")
	// P20 label "Cards by state"
	has(served, `<div class="label">Cards by state</div>`, "P20")
	// P21 state legend: seven swatches with counts
	assert.Equal(t, []string{"waiting 84", "ready 0", "working 0", "review 0", "fix 0", "merging 0", "landed 0"}, ownerSorted(st.Legend, []string{"waiting", "ready", "working", "review", "fix", "merging", "landed"}), "P21")
	// P22 segmented bar, its colours, the pulse, still when stopped
	for _, c := range []string{"--s-landed", "--s-merging", "--s-fix", "--s-review", "--s-working", "--s-ready", "--s-waiting"} {
		has(served, c+":", "P22 "+c)
	}
	for _, c := range []string{"merging", "review", "working"} {
		assert.Regexp(t, `#overall \.cell\.`+c+` \{ opacity: 1; animation: pulse`, served, "P22 "+c+" pulses")
	}
	assert.Contains(t, st.Class["overall"], "stopped", "P22 still when stopped")
	assert.NotContains(t, run.Class["overall"], "stopped", "P22 pulses while running")
	// P23 Work head and "N streams · n landed · n held"
	has(served, `<h2>Work</h2>`, "P23")
	assert.Equal(t, "13 streams · 0 landed · 11 held", st.Text["streams-sub"], "P23")
	// P24 Work columns
	require.NotEmpty(t, st.Streams, "P24")
	assert.Equal(t, []string{"stream", "status", "waiting", "ready", "working", "review", "fix", "merging", "landed", "cost"}, st.Streams[0], "P24")
	// P25 the display name, falling back to the key, with the amber tag
	assert.Equal(t, "schema c", st.StreamName["schema-c"], "P25 the display name")
	assert.Equal(t, "s-13", st.StreamName["s-13"], "P25 the key when no display name")
	assert.Equal(t, "2 stuck · ci red", st.StreamTag["schema-c"], "P25 the tag")
	// P26 status pill tones
	assert.Equal(t, "pill critical", st.StreamPill["schema-c"], "P26 stopped red")
	assert.Equal(t, "pill warning", st.StreamPill["s-1"], "P26 held amber")
	assert.Equal(t, "pill active", run.StreamPill["schema-c"], "P26 working blue")
	assert.Equal(t, "pill done", done.StreamPill["schema-c"], "P26 landed green")
	// P27 grouped by status with a rule between groups; Total row
	assert.Equal(t, "Total", st.Streams[len(st.Streams)-1][0], "P27 Total row")
	assert.Equal(t, 1, st.GroupStarts, "P27 one rule between stopped and held")
	// P28 Work and Fleet compact: name | status | count, counts centred, status beside them
	has(served, `.streams.compact .row, .fleet.compact .row { --count-w: 10.5rem; grid-template-columns: minmax(0, 1fr) var(--status-w) var(--count-w); }`, "P28")
	has(served, `.streams.compact .row > :nth-child(9), .fleet.compact .row > :nth-child(5) { justify-self: center; margin-left: 0; }`, "P28 counts centred")
	has(served, `.streams.compact .row > :nth-child(2), .fleet.compact .row > :nth-child(2) { justify-self: end; margin-right: 1.5rem; }`, "P28 status beside them")
	// P29 Fleet head and "n up · n held · n down"
	has(served, `<h2>Fleet</h2>`, "P29")
	assert.Equal(t, "2 up · 11 held · 0 down", st.Text["fleet-head"], "P29")
	// P30 Fleet columns
	require.NotEmpty(t, st.Fleet, "P30")
	// the track column's head is "working", the n / width column's "working" on the compact form only (.alt)
	assert.Equal(t, []string{"machine", "status", "ready", "working", "working", "done", "ok%", "load"}, st.Fleet[0], "P30")
	assert.Equal(t, []string{"machine-a", "up", "1", "", "0 / 16", "0", "100.0%", "0.0%"}, st.Fleet[1], "P30 a machine's row")
	// P31 Fleet pill tones
	assert.Equal(t, "pill good", st.FleetPill["machine-a"], "P31 up green")
	assert.Equal(t, "pill warning", st.FleetPill["held-01"], "P31 held amber")
	// P32 track cells scale to the fleet's own widest member while Friends is hidden
	assert.True(t, strings.HasPrefix(st.FleetTrack["machine-a"], "repeat(16,"), "P32 the fleet's widest (16), never the friends' (32): %s", st.FleetTrack["machine-a"])
	for _, c := range []string{".p-blocker", ".p-critical", ".p-fix", ".p-reader"} {
		has(served, c, "P32 "+c)
	}
	// P33 Fleet Total row; dimmed panel when off
	assert.Equal(t, "Total", st.Fleet[len(st.Fleet)-1][0], "P33")
	has(served, `.panel.off { opacity: .45; }`, "P33")
	// P34 Fleet compact with no ok%
	has(served, `.fleet.compact .row > :nth-child(7) { display: none; }`, "P34")
	// P35 Friends hidden by CSS
	has(served, `<section class="panel friends">`, "P35 kept, hidden")
	// P36-P38
	has(served, `<section class="panel providers" id="providers-panel" hidden>`, "P36")
	assert.True(t, st.Hidden["providers-panel"], "P36 no tables.providers")
	has(served, `<section class="panel readers" id="readers-panel" hidden>`, "P37 only with ?all=1")
	assert.True(t, st.Hidden["readers-panel"], "P37")
	has(served, `<section class="panel landings" id="landings-panel" hidden>`, "P38")
	// P39 footer link
	has(served, `https://github.com/mas-bandwidth/nova-sprint`, "P39")
	// P40 title, favicon the robot tile, dark only
	has(served, `<title>nova-sprint</title>`, "P40")
	has(served, `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=`, "P40 the robot tile")
	has(served, `data-theme="dark"`, "P40 dark")
	// the coin and its speaker glyph stay (locked 2026-10-11)
	has(served, `id="coin"`)
}

var ownerTags = regexp.MustCompile(`<[^>]*>`)

// ownerSorted is items ordered by prefix.
func ownerSorted(items, order []string) []string {
	var out []string
	for _, p := range order {
		for _, it := range items {
			if strings.HasPrefix(it, p+" ") {
				out = append(out, it)
			}
		}
	}
	return out
}

// the fixture's variants: the DONE-WHEN state, the sprint running with spend, the sprint done,
// the sprint empty, and running with no ETA in the summary
const (
	ownerStopped = iota
	ownerRunning
	ownerDone
	ownerEmpty
	ownerNoETA
)

// ownerWhere is the fixture where --json: epoch 20, 0/84, STOPPED, 2 machines up and 11 held,
// 12 friends held or down (width 32, wider than any machine), 13 streams, 12 with display names
// (s-13 has none), 11 of them held (7 waiting each), schema-c stopped with 7 waiting and a stuck,
// ci-red merge row, s-13 stopped with no cards. The variant changes only what it names.
func ownerWhere(variant int) []byte {
	machine, summary, landed, all := "machine: STOPPED", "0/84 0.0% held=11 -> ETA -", 0, 84
	work := map[string]map[string]any{}
	var streams []map[string]string
	for i := 1; i <= 11; i++ {
		k := fmt.Sprintf("s-%d", i)
		work[k] = map[string]any{"waiting": "7", "ready": "0", "working": "0", "review": "0", "fix": "0", "merging": "0", "landed": "0", "cost": "$0.00"}
		streams = append(streams, map[string]string{"Stream": k, "Display": fmt.Sprintf("stream %d", i), "State": "running"})
	}
	work["schema-c"] = map[string]any{"waiting": "7", "ready": "0", "working": "0", "review": "0", "fix": "0", "merging": "0", "landed": "0", "cost": "$0.00"}
	work["s-13"] = map[string]any{"waiting": "0", "ready": "0", "working": "0", "review": "0", "fix": "0", "merging": "0", "landed": "0", "cost": "$0.00"}
	streams = append(streams, map[string]string{"Stream": "schema-c", "Display": "schema c", "State": "stopped"},
		map[string]string{"Stream": "s-13", "State": "stopped"})
	extra := map[string]any{}
	switch variant {
	case ownerRunning:
		machine, summary = "machine: running", "0/84 0.0% held=10 -> ETA 2h20m"
		work["schema-c"] = map[string]any{"waiting": "4", "ready": "0", "working": "2", "review": "1", "fix": "0", "merging": "0", "landed": "0",
			"cost": "$3.75", "cost_by_tier": map[string]string{"flash": "1.25", "pro": "2.50"}}
		streams[11]["State"] = "running"
		extra["stream_costs"] = map[string]any{"schema-c": map[string]string{"total_cost": "3.75"}}
	case ownerDone:
		landed = 84
		summary = "84/84 100.0% held=0 -> ETA -"
		for k := range work {
			if k != "s-13" {
				work[k]["waiting"], work[k]["landed"] = "0", "7"
			}
		}
		work["schema-c"]["cost"] = "$157.50"
		extra["stream_costs"] = map[string]any{"schema-c": map[string]string{"total_cost": "157.50"}}
		extra["cleared"] = "2026-10-12T23:40:00Z"
		start := time.Date(2026, 10, 12, 23, 40, 0, 0, time.UTC).Unix()
		series := make([]int, 13)
		series[12] = 4
		extra["landedSeries"] = map[string]any{"start": start, "bucketSeconds": 600, "buckets": 13, "fleet": series}
	case ownerEmpty:
		all, summary = 0, "0/0 0.0% held=0 -> ETA -"
		work = map[string]map[string]any{}
		streams = nil
	case ownerNoETA:
		machine, summary = "machine: running", "0/84 0.0% held=11"
	}
	fleet := map[string]any{
		"machine-a": map[string]string{"status": "up", "ready": "1", "working": "0", "width": "16", "done": "0", "okpct": "100.0", "load": "0.0"},
		"machine-b": map[string]string{"status": "up", "ready": "2", "working": "0", "width": "8", "done": "0", "okpct": "100.0", "load": "0.0"},
	}
	for i := 1; i <= 11; i++ {
		fleet[fmt.Sprintf("held-%02d", i)] = map[string]string{"status": "held", "ready": "0", "working": "0", "width": "4", "done": "0"}
	}
	friends := map[string]any{}
	for i := 1; i <= 12; i++ {
		st := "held"
		if i > 6 {
			st = "down"
		}
		friends[fmt.Sprintf("friend-%02d", i)] = map[string]string{"status": st, "working": "0", "width": "32"}
	}
	top := map[string]any{
		"at": "2026-10-13T01:52:20Z", "landed": landed, "all": all, "summary": summary, "coordinator": "rowan", "epoch": "20",
		"machine": machine, "streams": streams,
		"tables": map[string]any{"work": work, "merge": map[string]any{"schema-c": map[string]string{"stuck": "2", "ci": "red"}}, "fleet": fleet, "friends": friends},
	}
	for k, v := range extra {
		top[k] = v
	}
	return mustJSON(top)
}

// servedPage is the page / serves over body, with no --logo.
func servedPage(t *testing.T, body []byte) string {
	r := newRig(t)
	r.s.Version = "v1.2.16"
	r.next = func() ([]byte, error) { return body, nil }
	return get(t, r.s, "/").Body.String()
}

// ownerEnvelope is /api/sprint's answer over body, the server's version v1.2.16.
func ownerEnvelope(t *testing.T, body []byte) json.RawMessage {
	return ownerEnvelopeVersion(t, body, "v1.2.16")
}

func ownerEnvelopeVersion(t *testing.T, body []byte, version string) json.RawMessage {
	r := newRig(t)
	r.s.Version = version
	r.next = func() ([]byte, error) { return body, nil }
	return json.RawMessage(get(t, r.s, "/api/sprint").Body.Bytes())
}

type ownerCase struct {
	Name   string          `json:"name"`
	Env    json.RawMessage `json:"env"`
	Narrow bool            `json:"narrow"`
}

// ownerRender is what one stubbed-DOM render reads back.
type ownerRender struct {
	Errors       []string            `json:"errors"`
	Text         map[string]string   `json:"text"`
	Val          map[string]string   `json:"val"`
	HTML         map[string]string   `json:"html"`
	Class        map[string]string   `json:"class"`
	Title        map[string]string   `json:"title"`
	Hidden       map[string]bool     `json:"hidden"`
	Display      map[string]string   `json:"display"`
	Nofit        map[string]bool     `json:"nofit"`
	NofitOrder   []string            `json:"nofitOrder"`
	Children     map[string]int      `json:"children"`
	Legend       []string            `json:"legend"`
	Pie          []string            `json:"pie"`
	TierSwatches []string            `json:"tierSwatches"`
	Top          [][]string          `json:"top"`
	TopClass     []string            `json:"topClass"`
	Streams      [][]string          `json:"streams"`
	StreamName   map[string]string   `json:"streamName"`
	StreamTag    map[string]string   `json:"streamTag"`
	StreamPill   map[string]string   `json:"streamPill"`
	GroupStarts  int                 `json:"groupStarts"`
	Fleet        [][]string          `json:"fleet"`
	FleetPill    map[string]string   `json:"fleetPill"`
	FleetTrack   map[string]string   `json:"fleetTrack"`
	Extra        map[string][]string `json:"extra"`
}

// renderOwner runs the shipped app.js over each case's envelope, each on a fresh stubbed DOM,
// and reads the rendered elements back.
func renderOwner(t *testing.T, appJS string, cases []ownerCase) map[string]ownerRender {
	t.Helper()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NOVA_CI") == "1" {
			require.NoError(t, err, "node is required for the dashboard JS parity test")
		}
		t.Skip("node is not installed on this machine; the dashboard JS parity test needs it")
	}
	cmd := exec.Command(nodePath, "-e", ownerDOMStub)
	cmd.Stdin = bytes.NewReader(mustJSON(map[string]any{"appJS": appJS, "cases": cases}))
	cmd.Env = append(os.Environ(), "TZ=America/New_York")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	require.NoError(t, cmd.Run(), "node runner failed: %s", errBuf.String())
	res := map[string]ownerRender{}
	require.NoError(t, json.Unmarshal(out.Bytes(), &res), "node output: %s", out.String())
	return res
}

// ownerDOMStub loads app.js into a minimal DOM per case and renders the case's envelope through
// the page's own accept(). A narrow case answers matchMedia("(max-width: 1100px)") true and puts
// the clock where no chip fits beside it; a wide one leaves room. It writes the read-back as JSON.
const ownerDOMStub = `
const fs = require('fs');
const vm = require('vm');
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
function renderCase(c) {
  const errors = [];
  const nofitOrder = [];
  function createElement(tag, id) {
    const _classes = new Set();
    const el = {
      tagName: String(tag).toUpperCase(), id: id || '', _classes, hidden: false, title: '',
      get className() { return Array.from(_classes).join(' '); },
      set className(v) { _classes.clear(); if (v) String(v).split(/\s+/).filter(Boolean).forEach(x => _classes.add(x)); },
      get classList() { return {
        add(...x){ x.forEach(y => { if (y === 'nofit' && !_classes.has(y)) nofitOrder.push(el.id); _classes.add(y); }); },
        remove(...x){ x.forEach(y => _classes.delete(y)); },
        toggle(x, f){ if (f === undefined) f = !_classes.has(x); if (f) _classes.add(x); else _classes.delete(x); return f; },
        contains(x){ return _classes.has(x); } }; },
      children: [], dataset: {}, attributes: {},
      style: { setProperty(k, v){ this[k] = v; }, getPropertyValue(k){ return this[k] || ''; }, removeProperty(k){ delete this[k]; } },
      addEventListener(){}, setAttribute(k, v){ this.attributes[k] = String(v); }, getAttribute(k){ return this.attributes[k] || null; },
      appendChild(ch){ if (ch.parentNode) ch.parentNode.removeChild(ch); this.children.push(ch); ch.parentNode = this; return ch; },
      insertBefore(ch, ref){ if (ch.parentNode) ch.parentNode.removeChild(ch); const i = ref ? this.children.indexOf(ref) : -1; if (i >= 0) this.children.splice(i, 0, ch); else this.children.push(ch); ch.parentNode = this; return ch; },
      removeChild(ch){ const i = this.children.indexOf(ch); if (i >= 0) this.children.splice(i, 1); ch.parentNode = null; return ch; },
      get nextSibling(){ if (!this.parentNode) return null; const s = this.parentNode.children; return s[s.indexOf(this) + 1] || null; },
      get firstChild(){ return this.children[0] || null; }, get lastChild(){ return this.children[this.children.length - 1] || null; },
      get innerHTML(){ return this._html || ''; }, set innerHTML(v){ this.children = []; this._html = String(v); this._text = ''; },
      get textContent(){ if (this.children.length > 0) return this.children.map(x => x.textContent).join(''); return this._text || ''; },
      set textContent(v){ this._text = String(v); this._html = ''; this.children = []; },
      querySelector(){ return null; }, querySelectorAll(){ return []; },
      closest(){ return null; },
      remove(){ if (this.parentNode) this.parentNode.removeChild(this); }, cloneNode(){ return createElement(tag); },
      getBoundingClientRect(){ return rects[el.id] || { top: 0, bottom: 10, left: 0, right: 10 }; },
      scrollWidth: 50, clientWidth: 100, offsetWidth: 50, offsetHeight: 20, offsetParent: {}
    };
    return el;
  }
  // the header: the logo at the left; a wide header has the clock far to the right of every chip,
  // a narrow one has the clock where no chip fits
  const rects = { logo: { top: 0, bottom: 10, left: 0, right: 10 }, live: { top: 0, bottom: 10, left: c.narrow ? 5 : 100000, right: 100100 } };
  const elements = new Map();
  function getEl(id) { if (!elements.has(id)) elements.set(id, createElement('div', id)); return elements.get(id); }
  // the panels index.html serves hidden; the version chip starts shown here, so its hiding is app.js's
  ['readers-panel', 'providers-panel', 'landings-panel'].forEach(id => { getEl(id).hidden = true; });
  getEl('release').hidden = true;
  const context = {
    window: { addEventListener(){}, matchMedia: (q) => ({ matches: c.narrow && q === '(max-width: 1100px)' }) },
    document: { getElementById: getEl, createElement: (t) => createElement(t), createElementNS: (ns, t) => createElement(t),
      createTextNode: (t) => ({ textContent: t, children: [] }), documentElement: {}, querySelector(){ return null; }, querySelectorAll(){ return []; },
      body: createElement('body') },
    getComputedStyle: () => ({ fontSize: '16px', display: 'block' }), location: { search: '', reload(){} },
    localStorage: { getItem(){ return null; }, setItem(){} },
    fetch: () => new Promise(() => {}), EventSource: undefined, setInterval: () => 0, setTimeout: () => 0, clearTimeout(){}, requestAnimationFrame: () => 0,
    console: { log(){}, warn(){}, error(...a){ errors.push(a.map(x => x && x.stack ? x.stack : String(x)).join(' ')); } },
    Intl, Math, Date, String, Number, parseInt, parseFloat, isNaN, Array, Object, RegExp, JSON, Map, Set, Promise
  };
  context.window.document = context.document;
  vm.createContext(context);
  try { vm.runInContext(input.appJS, context); } catch (e) { errors.push('load: ' + (e.stack || e)); }
  try { vm.runInContext('accept', context)(c.env); } catch (e) { errors.push('accept: ' + (e.stack || e)); }
  const ids = ['landed', 'all', 'all2', 'pct', 'pct-complete', 'of-all2', 'eta', 'eta-at', 'cost', 'cost-whole', 'cost-per', 'inflight',
    'inflight-sub', 'tput', 'coord', 'epoch', 'version', 'version-chip', 'wordmark', 'coord-chip', 'epoch-chip', 'live-text', 'tier-sub',
    'streams-sub', 'fleet-head', 'overall', 'providers-panel', 'readers-panel', 'release'];
  const r = { errors, text: {}, val: {}, html: {}, class: {}, title: {}, hidden: {}, display: {}, nofit: {}, nofitOrder, children: {},
    streamName: {}, streamTag: {}, streamPill: {}, fleetPill: {}, fleetTrack: {}, groupStarts: 0 };
  // the text a viewer reads: a node's children's, else its markup's with the tags taken out, else its own
  const textOf = (x) => x.children && x.children.length ? x.children.map(textOf).join('') : (x._html ? x._html.replace(/<[^>]*>/g, '') : (x._text || x.textContent || ''));
  const cellText = textOf;
  ids.forEach(id => { const e = getEl(id);
    r.text[id] = textOf(e); r.val[id] = e._val == null ? '' : String(e._val); r.html[id] = e.innerHTML; r.class[id] = e.className;
    r.title[id] = e.title || ''; r.hidden[id] = !!e.hidden; r.display[id] = e.style.display || ''; r.nofit[id] = e.classList.contains('nofit');
    r.children[id] = e.children.length; });
  const lg = getEl('legend'); r.legend = lg.children.map(textOf);
  r.pie = getEl('pie').children.map(x => x.tagName.toLowerCase() + ' ' + x.attributes.fill);
  r.tierSwatches = getEl('tier-sub').children.map(x => x.children[0] && x.children[0].style.background);
  const top = getEl('top-streams'); r.top = top.children.map(row => row.children.map(cellText)); r.topClass = top.children.map(row => row.className);
  const streams = getEl('streams');
  r.streams = streams.children.map(row => row.children.map(cellText));
  if (streams._map) streams._map.forEach((row, k) => {
    r.streamName[k] = textOf(row.nameT); r.streamTag[k] = textOf(row.tag); r.streamPill[k] = row.pill.className;
    if (row.node.classList.contains('group-start')) r.groupStarts++;
  });
  const fleet = getEl('fleet');
  r.fleet = fleet.children.map(row => row.children.map(cellText));
  if (fleet._map) fleet._map.forEach((row, k) => { r.fleetPill[k] = row.pill.className; r.fleetTrack[k] = row.track.style.gridTemplateColumns || ''; });
  return r;
}
const out = {};
input.cases.forEach(c => { out[c.name] = renderCase(c); });
process.stdout.write(JSON.stringify(out));
`

// The version chip (P3) reads data.server_version: where's own when it carries one, else the
// listening process's, as the live page's server sets it; nothing when neither is known; and the
// envelope gains no key of its own for it (the live server's keys, and no other).
func TestThePageDataCarriesTheServersVersion(t *testing.T) {
	t.Parallel()
	version := func(body []byte, v string) (string, bool) {
		var env map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(ownerEnvelopeVersion(t, body, v), &env))
		_, top := env["server_version"]
		var data struct {
			ServerVersion *string `json:"server_version"`
		}
		require.NoError(t, json.Unmarshal(env["data"], &data))
		assert.False(t, top, "the envelope has no server_version key of its own")
		if data.ServerVersion == nil {
			return "", false
		}
		return *data.ServerVersion, true
	}
	got, ok := version(ownerWhere(ownerStopped), "v1.2.16")
	assert.True(t, ok)
	assert.Equal(t, "v1.2.16", got, "where gives none: the listening process's")
	withOwn := bytes.Replace(ownerWhere(ownerStopped), []byte(`"at":`), []byte(`"server_version":"v1.2.17","at":`), 1)
	got, _ = version(withOwn, "v1.2.16")
	assert.Equal(t, "v1.2.17", got, "where's own wins")
	_, ok = version(ownerWhere(ownerStopped), "")
	assert.False(t, ok, "neither known: none, and the chip hides")
}
