//go:build functional

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// providerUsage is a take that ended on a provider or harness failure, spent tokens and
// time. The producer's cost record carries its end as the cause: `provider failure` (a
// provider or account limit) and `no result` (a harness crash before the first turn) are
// each coordination by construction (sprint.CoordinationCauseOfEnd).
const providerUsage = "wall=10s budget=1500/400000 input=1000 cache_read=2000 output=300 reasoning=200 requests=3 model=opencode/deepseek-v4-flash actual_usd=0.0005 actual_by=harness"

// coordinationEnds is each coordination class the card names and the report word a finish
// carries for it, in the order the functional test simulates them (the owner, verbatim:
// "Add a functional test that simulates coordinator fuckups and verify the counter
// increases and the friend, and fleet machines aren't blamed.").
var coordinationEnds = []struct {
	cause, report string
}{
	{"harness crash before first turn", "no result: the harness crashed before the first turn"},
	{"provider or account limit", "provider failure: class=usage limit: the account is at its limit"},
	{"provider content-filter or key refusal", "provider failure: content-filter refusal: the prompt was refused"},
	{"idle-reap with no run", "the lane was reaped at idle=0s with no run: ran=false"},
	{"missing bench tool or jar", "the lane failed: no rustc on PATH"},
	{"staging refused", "staging refused: no bench mirror"},
	{"tick or hold took the card back", "the tick took the card back: no beat carried her live set"},
	{"card dealt with unlanded needs", "the card was dealt before its needs landed: needs not landed"},
	{"hook-gate refusal", "the hook-gate refused the push"},
	{"server or store fault", "store fault: the server errored"},
	{"landing refused on a conflict", "landing refused: a conflict with another landing"},
}

// TestCoordinationFailuresAccumulateAndAreNotBlamedOnTheWorker is the owner's functional
// test: a twin server with a fleet member, two cards that end WORKER-ok, then every
// coordination class against it. After each one the coordination counter rose by exactly
// one under that cause and under the right member, and no brief-defect or bound judgment
// is raised. Then one real WORKER failure (a not-done verdict with a pushed head) does not
// move the counter, proving the test can tell the two apart.
func TestCoordinationFailuresAccumulateAndAreNotBlamedOnTheWorker(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream s1 --count 14")
	ta.deal(14)
	ta.ok("take --as m1 --limit 14")

	// two cards end WORKER-ok: finished and read ok by the reader, no coordination
	ta.ok("finish --as m1 s1-1.w1@1 s1-2.w1@1")
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 10 --usage '" + usageReadA + "'")
	assertCoordinationTotal(t, ta, 0)

	// every coordination class against m1, in turn, each on its own card
	for i, ec := range coordinationEnds {
		id := cardID(i + 3)
		// a card dealt with unlanded needs is decided on the table's facts (the primary's
		// needs), never on the report's words: pin an unlanded need on the primary, as the
		// race the owner saw did, so the finish routes to the needs-stop path
		if ec.cause == string(sprint.CauseUnlandedNeeds) {
			ta.setNeeds(strings.TrimSuffix(id, ".w1"), "n1")
		}
		ta.ok("finish --as m1 " + id + "@1 --failed --report '" + ec.report + "' --usage '" + providerUsage + "'")
		// the counter rose by exactly one under this cause and under m1
		assertCoordination(t, ta, i+1, ec.cause, "m1")
		// no brief-defect or bound judgment was raised for a coordination end
		assertNoBriefDefect(t, ta, "s1-3")
	}

	// then one real WORKER failure: a not-done verdict with a pushed head. It is the
	// worker's, never coordination, so the counter is unchanged.
	ta.ok("finish --as m1 s1-14.w1@1 --failed --head abcdef1234 --branch sprint/s1-14 --report 'verdict not-done; pushed=abcdef1234'")
	assertCoordinationTotal(t, ta, len(coordinationEnds))

	// each new coordination end is a HAPPENED note addressed to the coordinator
	// (notes.go, NCoordinationFailure), which the coordinator's hook streams: the push
	// reaches the seat that must fix the cause.
	assert.Contains(t, ta.ok("inbox"), "a coordination failure", "the coordination push reached the coordinator")
}

func cardID(n int) string { return "s1-" + strconv.Itoa(n) + ".w1" }

// setNeeds pins the named needs on a primary, the way the sprint's own test helper does
// (internal/sprint/needs_take_twin_test.go setNeeds): the finish's needs-stop is decided on
// the table's facts alone, so the functional test writes the needs field directly, as the
// race the owner saw did (a card dealt before its needs landed).
func (ta *testApp) setNeeds(primary, needs string) {
	ta.t.Helper()
	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	require.NoError(ta.t, err)
	c := snap.Work.Card(primary)
	require.NotNil(ta.t, c, primary)
	_, err = ta.m.Apply(ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(snap.Work.Revision), OperationID: "set-needs-" + primary,
		Members: []ntable.BatchMemberEntry{{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: map[string]string{"needs": needs}}}})
	require.NoError(ta.t, err)
}

func assertCoordinationTotal(t *testing.T, ta *testApp, want int) {
	t.Helper()
	var st struct {
		Coordination *sprint.CoordinationCounter `json:"coordination"`
	}
	ta.json("stats --json", &st)
	require.NotNil(t, st.Coordination, "stats coordination")
	assert.Equal(t, want, st.Coordination.Total, "coordination total")
}

func assertCoordination(t *testing.T, ta *testApp, wantTotal int, wantCause, wantWho string) {
	t.Helper()
	var st struct {
		Coordination *sprint.CoordinationCounter `json:"coordination"`
	}
	ta.json("stats --json", &st)
	require.NotNil(t, st.Coordination, "stats coordination")
	assert.Equal(t, wantTotal, st.Coordination.Total, "coordination total")
	ca := sprint.CoordinationCause(wantCause)
	assert.Equal(t, 1, st.Coordination.ByCause[ca], "cause %q", wantCause)
	assert.Equal(t, wantTotal, st.Coordination.ByWho[wantWho], "who %q", wantWho)
}

// assertNoBriefDefect fails if the card raised a brief-defect or bound judgment: the inbox
// (a judgment) must not carry one for the card after a coordination end.
func assertNoBriefDefect(t *testing.T, ta *testApp, card string) {
	t.Helper()
	out := ta.ok("inbox")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "JUDGMENT ") {
			continue
		}
		for _, word := range []string{"brief", "bound", "defect"} {
			if strings.Contains(line, word) {
				t.Fatalf("a coordination end raised a %s judgment: %s", word, line)
			}
		}
	}
}

// TestACoordinationEndLeavesOkPctAloneAndAWorkerEndMovesIt is the ok% half of the rule: a
// coordination end never hits the worker's ok% (the card is withdrawn, never failed work),
// while a worker's own end moves it (its failed work is read broken and lowers the row's
// ok%). On the started machine, where the readers' verdicts run.
func TestACoordinationEndLeavesOkPctAloneAndAWorkerEndMovesIt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream s1 --count 4")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 4")

	// two cards end WORKER-ok and are read ok: 100.0%
	ta.ok("finish --as m1 s1-1.w1@1 s1-2.w1@1")
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 10 --usage '" + usageReadA + "'")
	ta.ok("tick")
	assertRowOkPct(t, ta, "m1", "100.0%")

	// a coordination end on a third card leaves the ok% alone: it is withdrawn, never
	// failed work waiting on a reader's verdict, so it never enters the ok or failed cells
	// the row's ok% (pct(ok/ok+failed)) is computed over.
	ta.ok("finish --as m1 s1-3.w1@1 --failed --report 'the lane failed: no rustc on PATH' --usage '" + providerUsage + "'")
	var c3 cardView
	ta.json("card s1-3", &c3)
	require.Len(t, c3.Work, 1, "one work card")
	require.Equal(t, sprint.Withdrawn, c3.Work[0].Col, "a coordination end is withdrawn, never failed work")
	assertRowOkPct(t, ta, "m1", "100.0%")

	// a worker's own end moves the ok%: its ok work read broken lowers the row's ok%
	ta.ok("finish --as m1 s1-4.w1@1")
	ta.ok("ask")
	ta.ok("read --as reader-a --broken s1-4.r1.reader-a --finding 'internal/x.go:1: off by one' --usage '" + usageReadA + "'")
	ta.ok("tick")
	assertRowOkPct(t, ta, "m1", "66.7%")
}

// TestAFriendLanesCoordinationEndIsClassedCoordinationAndPushedToTheHook is the friend's
// half of the rule: a friend lane's coordination end is classed coordination (withdrawn as
// a coordination failure, never failed work, her ok% untouched) and pushed to the
// coordinator's hook. Every class from (1) runs against her in turn, as against a member.
func TestAFriendLanesCoordinationEndIsClassedCoordinationAndPushedToTheHook(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "friend amy", "amy")
	ta.ok("friend up amy --width 16")
	// eleven cards, one per class: s1-1 (from friendCardApp) plus s1-2 .. s1-11
	dir := t.TempDir()
	for i := 2; i <= len(coordinationEnds); i++ {
		id := "s1-" + strconv.Itoa(i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".md"),
			[]byte(passingBrief(id+": a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy")), 0o644))
	}
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("tick")
	ta.startFriend("amy", len(coordinationEnds))

	for i, ec := range coordinationEnds {
		id := cardID(i + 1)
		// a card dealt with unlanded needs is decided on the table's facts, as for a member
		if ec.cause == string(sprint.CauseUnlandedNeeds) {
			ta.setNeeds(strings.TrimSuffix(id, ".w1"), "n1")
		}
		ta.ok("finish --as friend.amy " + id + "@1 --failed --report '" + ec.report + "' --usage '" + providerUsage + "'")
		// the counter rose by exactly one under this cause and under amy's row
		assertCoordination(t, ta, i+1, ec.cause, string(sprint.FriendRow("amy")))
		// classed coordination: withdrawn, never failed work
		var c cardView
		ta.json("card "+strings.TrimSuffix(id, ".w1"), &c)
		require.Equal(t, sprint.Withdrawn, c.Work[0].Col, "a friend lane's coordination end is withdrawn, never failed")
	}
	// her ok% is untouched throughout (no card of hers was failed work)
	assertFriendsOkPct(t, ta, "amy", "0.0%")
	// pushed to the hook: the coordinator is told, naming the friend and the cause
	out := ta.ok("inbox")
	assert.Contains(t, out, "a coordination failure", "the coordination push reached the coordinator")
	assert.Contains(t, out, "for=coordinator", "the push is addressed to the coordinator")
	assert.Contains(t, out, "missing bench tool or jar", "the push names the cause")
}

// TestACoordinationEndDoesNotCountTowardTheAttemptBound is the bound's half of the rule: a
// card whose ends are coordination is never judged a brief defect for them. Four
// coordination ends in a row leave the card dealt, never "the brief is wrong, not the
// worker" and never "the lane is broken, not the brief".
func TestACoordinationEndDoesNotCountTowardTheAttemptBound(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("take --as m1 --limit 1")

	for n := 0; n < sprint.LaneBrokenBound; n++ {
		gen := "1"
		if n > 0 {
			gen = ta.workGen("m1", "s1-1.w1")
			ta.ok("take --as m1 s1-1.w1@" + gen)
		}
		ta.ok("finish --as m1 s1-1.w1@" + gen + " --failed --report '" + coordinationEnds[n].report + "' --usage '" + providerUsage + "'")
		if n < sprint.LaneBrokenBound-1 {
			ta.deal(1)
		}
	}

	// no brief-defect, brief-wrong or lane-broken judgment was raised: the card is still
	// dealt, not blamed on the worker or the brief
	assertNoBriefDefect(t, ta, "s1-1")
	var c cardView
	ta.json("card s1-1", &c)
	require.Equal(t, sprint.Ready, c.Primary.Col, "the card is still dealt after %d coordination ends", sprint.LaneBrokenBound)
}

// assertRowOkPct is a fleet row's ok% cell, the table's formula over the ok and failed
// cells (docs/SPEC-SPRINT.md, done and ok%).
func assertRowOkPct(t *testing.T, ta *testApp, row, want string) {
	t.Helper()
	var w whereView
	ta.json("where", &w)
	require.Equal(t, want, w.Tables[sprint.Fleet][row]["okpct"], "fleet %s ok%%", row)
}

// assertFriendsOkPct is a friend's ok% cell.
func assertFriendsOkPct(t *testing.T, ta *testApp, friend, want string) {
	t.Helper()
	var w whereView
	ta.json("where", &w)
	require.Equal(t, want, w.Tables[sprint.Friends][friend]["okpct"], "friend %s ok%%", friend)
}
