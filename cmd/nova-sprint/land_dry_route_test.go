package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestADirectDryRunNamesTheBenchADirectLandUses pins the dry run's gate route to the
// route a direct land uses (gateBenches, runTreeGate): a land command on its own keeps
// the gate in this process, so the dry run names no bench and refuses nothing; only the
// server's loop, with a flight in progress and its bench seam, sends the gate out, and
// then the dry run names the ring's first host, the one the loop's gate asks first.
func TestADirectDryRunNamesTheBenchADirectLandUses(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.live = nil
	ta.ok("init --readers reader-a --members m1")
	for _, m := range []string{"vision", "space"} {
		ta.ok("fleet beat " + m + " --load 1 --cores 8")
		ta.ok("fleet up " + m + " --probe passed")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/gate\n"), 0o600))
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	l := &lander{a: ta.a, st: st, gateKey: "s1"}
	b := ta.a.landState()
	b.mu.Lock()
	b.hostName = func() (string, error) { return "studio.local", nil }
	b.mu.Unlock()

	host, why := l.dryGate(dir)
	assert.Empty(t, host, "a direct land runs the gate here, so the dry run names no bench")
	assert.Empty(t, why, "a direct land runs the gate here, so the dry run refuses nothing")

	s, err := st.Load(context.Background(), []string{sprint.Fleet}, nil)
	require.NoError(t, err)
	hosts := s.GateMembers()
	require.ElementsMatch(t, []string{"vision", "space"}, hosts, "the fleet's two up members may gate")
	want := benchRing(l.gateKey, hosts)[0]

	b.mu.Lock()
	b.flight = &landFlight{}
	b.gateBench = func(context.Context, string, string, [][]string, bool) (string, int, error) {
		return "", 0, nil
	}
	b.mu.Unlock()

	host, why = l.dryGate(dir)
	assert.Empty(t, why, "a bench is up, so the loop's dry run does not refuse")
	assert.Equal(t, want, host, "the loop's gate asks the ring's first host first")
}
