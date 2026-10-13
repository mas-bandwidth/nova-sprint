package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Held ordinary cards keep explicit release. A new held sentinel is refused,
// while an ordinary empty sentinel releases automatically without a manual gate.
func TestHeldSentinelAdmissionRefusedAndOrdinaryHeldCardReleases(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	code, _, errs := ta.do("add --stream w --sentinel gate --held --actor lead")
	require.Equal(t, 1, code)
	require.Contains(t, errs, "sentinels release automatically")
	ta.ok("add --stream w --sentinel gate --actor lead")
	ta.ok("add --stream w a b --actor lead")
	ta.ok("add --stream v c --one --held --actor lead")
	ta.ok("start --actor lead")
	ta.ok("tick")
	require.Equal(t, sprint.Landed, ta.primary("gate").Col)
	for _, id := range []string{"a", "b", "c"} {
		require.Equal(t, sprint.Waiting, ta.primary(id).Col)
	}
	for _, g := range ta.inboxGroups() {
		require.NotEqual(t, sprint.NSentinelReached, g.Type)
		require.NotEqual(t, sprint.NStalled, g.Type)
	}
	ta.clean()
	out := ta.ok("release c --reason 'ordinary card allowed' --actor lead")
	require.Contains(t, out, "c waiting -> ready (released by lead)")
	ta.ok("tick")
	require.NotEqual(t, sprint.Waiting, ta.primary("c").Col)
	require.Equal(t, sprint.Waiting, ta.primary("a").Col, "consumer remains blocked without an actual trunk merge")
	require.Equal(t, sprint.Waiting, ta.primary("b").Col, "consumer remains blocked without an actual trunk merge")
	ta.clean()
}
