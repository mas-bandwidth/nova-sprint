package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The carry from nova-tools (PR 5305, the seat fence and the friend health API;
// PR 5308, the auto sentinel) is in nova-sprint main: friend health refuses a
// proof dated after the server's clock (a2aff60e99d3), and add --sentinel --auto
// admits a sentinel the tick releases when its needs land (21578186310b).
func TestSeatHealthAndAutoSentinelAreInNovaSprintMain(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync --root " + t.TempDir())
	require.Equal(t, "SEAT holder=coordinator epoch=0 generation=1\n", ta.ok("seat"), "the seat read")
	seen := ta.now.UTC().Format(time.RFC3339)
	ta.ok("friend health amy --state up --seen " + seen + " --generation 1")
	code, _, errs := ta.do("friend health amy --state up --seen " + ta.now.Add(time.Hour).UTC().Format(time.RFC3339) + " --generation 1")
	require.Equal(t, 1, code, errs)
	require.Contains(t, errs, "after the server's clock", "a proof dated after the server's clock is refused")

	sa := newTestApp(t)
	sa.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	sa.ok("add --stream s1 --count 2 --actor lead")
	out := sa.ok("add --stream s1 --sentinel stop --auto --actor lead")
	require.Contains(t, out, "auto: the tick releases it when its needs land", "add --sentinel --auto")
	var w map[string]any
	sa.json("where", &w)
	require.Equal(t, float64(1), w["auto"], "where --json counts the auto sentinel: %v", w)
}
