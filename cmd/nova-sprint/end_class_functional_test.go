//go:build functional

package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
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
	ta.ok("add --stream s1 --count 13")
	ta.deal(13)
	ta.ok("take --as m1 --limit 13")

	// two cards end WORKER-ok: finished and read ok by the reader, no coordination
	ta.ok("finish --as m1 s1-1.w1@1 s1-2.w1@1")
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 10 --usage '" + usageReadA + "'")
	assertCoordinationTotal(t, ta, 0)

	// every coordination class against m1, in turn, each on its own card
	for i, ec := range coordinationEnds {
		id := cardID(i + 3)
		ta.ok("finish --as m1 " + id + "@1 --failed --report '" + ec.report + "' --usage '" + providerUsage + "'")
		// the counter rose by exactly one under this cause and under m1
		assertCoordination(t, ta, i+1, ec.cause, "m1")
		// no brief-defect or bound judgment was raised for a coordination end
		assertNoBriefDefect(t, ta, "s1-3")
	}

	// then one real WORKER failure: a not-done verdict with a pushed head. It is the
	// worker's, never coordination, so the counter is unchanged.
	ta.ok("finish --as m1 s1-13.w1@1 --failed --head abcdef1234 --branch sprint/s1-13 --report 'verdict not-done; pushed=abcdef1234'")
	assertCoordinationTotal(t, ta, len(coordinationEnds))

	// each new coordination end is a HAPPENED note addressed to the coordinator
	// (notes.go, NCoordinationFailure), which the coordinator's hook streams: the push
	// reaches the seat that must fix the cause.
	assert.Contains(t, ta.ok("inbox"), "a coordination failure", "the coordination push reached the coordinator")
}

func cardID(n int) string { return "s1-" + strconv.Itoa(n) + ".w1" }

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
