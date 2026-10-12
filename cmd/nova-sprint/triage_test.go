package main

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestProducerPlanningCLIAndLegacyCheck(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --coordinator coordinator")
	ta.ok("add --stream plans a --one")
	require.Equal(t, "plan", ta.primary("a").Col)
	require.Contains(t, ta.ok("view coordinator --json"), `"plan":1`)
	code, _, _ := ta.do("promote a")
	require.NotEqual(t, 0, code)
	out := ta.ok("check a --json")
	require.Contains(t, out, `"triage"`)
	require.Equal(t, "check", ta.primary("a").Col)
	require.Contains(t, ta.ok("view coordinator --json"), `"check":1`)
	require.Contains(t, ta.ok("inbox --triage"), `"id":"a"`)
	ta.ok("promote --stream plans")
	require.Equal(t, "waiting", ta.primary("a").Col)
	ta.ok("check")
	require.Empty(t, readVerb([]string{"promote", "a"}).unserved())
	require.Contains(t, readVerb([]string{"promote", "--once"}).unserved(), "not run by the server")
}

func TestPlanningReviewRejectsMixedSelectorAndNonCoordinator(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --coordinator coordinator")
	ta.ok("add --stream plans a --one")
	for _, line := range []string{"check a --all-plan", "check --all-plan --actor stranger", "promote a --actor stranger"} {
		code, _, errs := ta.do(line)
		require.NotEqual(t, 0, code)
		require.NotEmpty(t, strings.TrimSpace(errs))
	}
	require.Equal(t, "plan", ta.primary("a").Col)
}

func TestPlanningDispatchWritesOnTheControlLaneAndReplays(t *testing.T) {
	require.False(t, onReadLane(readVerb([]string{"check", "a"})))
	require.False(t, onReadLane(readVerb([]string{"check", "--all-plan"})))
	require.True(t, onReadLane(readVerb([]string{"check"})))
	ta := newTestApp(t)
	ta.ok("init --coordinator coordinator")
	ta.ok("add --stream plans a --one")
	first := ta.ok("check a --json --op plan-review")
	second := ta.ok("check a --json --op plan-review")
	require.Contains(t, first, `"triage":[`)
	require.Contains(t, second, `"triage":[`)
	require.Contains(t, second, `"replay":true`)
	code, _, _ := ta.do("promote a --dry-run")
	require.NotEqual(t, 0, code)
	require.Equal(t, "check", ta.primary("a").Col)
}
