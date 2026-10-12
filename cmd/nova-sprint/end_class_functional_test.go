//go:build functional

package main

import (
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

// TestCoordinationFailuresAccumulateAndAreNotBlamedOnTheWorker is the owner's functional
// test, verbatim: "Add a functional test that simulates coordinator fuckups and verify the
// counter increases and the friend, and fleet machines aren't blamed." A twin server with a
// fleet member gets a pair of cards that end WORKER-ok, then a coordinator failure class
// against it: the coordination counter rose by exactly one under that cause and under the
// right member, and no brief-defect or bound judgment was raised. Then one real WORKER
// failure (a not-done verdict with a pushed head) does not move the counter, proving the
// test can tell the two apart. The counter is the epoch's, read from the producers' cost
// records (sprint.CoordinationOf, shown in stats --json): the provider and no-result ends
// are coordination by construction, each at its cause, and a failed finish of the worker's
// own is the worker's, never coordination.
func TestCoordinationFailuresAccumulateAndAreNotBlamedOnTheWorker(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream s1 --count 5")
	ta.deal(5)
	ta.ok("take --as m1 --limit 5")

	// two cards end WORKER-ok: finished and read ok by the reader, no coordination
	ta.ok("finish --as m1 s1-1.w1@1 s1-2.w1@1")
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 10")
	assertCoordinationTotal(t, ta, 0)

	// a coordinator fuckup: a provider or account limit. The counter rises by exactly one
	// under that cause and under m1, and no brief-defect or bound judgment is raised.
	ta.ok("finish --as m1 s1-3.w1@1 --failed --report 'provider failure: class=usage limit: the account is at its limit' --usage '" + providerUsage + "'")
	assertCoordination(t, ta, 1, "provider or account limit", "m1")
	assertNoBriefDefect(t, ta, "s1-3")
	// a second coordinator fuckup: a harness crash before the first turn (no result)
	ta.ok("finish --as m1 s1-4.w1@1 --failed --report 'no result: the harness crashed before the first turn' --usage '" + providerUsage + "'")
	assertCoordinationTotal(t, ta, 2)

	// then one real WORKER failure: a not-done verdict with a pushed head. It is the
	// worker's, never coordination, so the counter is unchanged.
	ta.ok("finish --as m1 s1-5.w1@1 --failed --head abcdef1234 --branch sprint/s1-5 --report 'verdict not-done; pushed=abcdef1234'")
	assertCoordinationTotal(t, ta, 2)
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
	assert.Equal(t, 1, st.Coordination.ByWho[wantWho], "who %q", wantWho)
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
