package main

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// heldGateRig is a rig with a Go module on main, one merging card on s1, and vision and
// space the only up benches, every other member down by the machine (machineDown, not
// held), this machine studio.local. twoBenchesRing is not used: it takes the others down
// with fleet down, which is a coordinator's hold (hold --return), and a held member may gate.
func heldGateRig(t *testing.T) *landRig {
	t.Helper()
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the module", goModule)
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 1 --one")
	heads := map[string]string{"s1-1": r.card("s1-1", map[string]string{"ok.go": "package main\n\nfunc ok() {}\n"})}
	r.queued(heads, "s1-1")
	for _, m := range []string{"vision", "space"} {
		r.ok("fleet beat " + m + " --load 1 --cores 8")
		r.ok("fleet up " + m + " --probe passed")
	}
	r.downOthers("vision", "space")
	b := r.a.landState()
	b.mu.Lock()
	b.hostName = func() (string, error) { return "studio.local", nil }
	b.mu.Unlock()
	return r
}

// heldGateHosts is the gate's host choice (gateHosts), called directly on a lander of
// the rig's store: what the loop's gateBenches and the dry run both choose from.
func (r *landRig) heldGateHosts() (hosts []string, remote bool) {
	r.t.Helper()
	st, err := r.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(r.t, err)
	l := &lander{a: r.a, st: st}
	return l.gateHosts(context.Background())
}

// benchBack brings vision up again before the rig's closing check: the check's rule 11
// (internal/sprint/check.go) asks for a member up now, so a fleet left all held or all
// down with a card merging is named there.
func (r *landRig) benchBack(unhold ...string) {
	r.t.Helper()
	if len(unhold) > 0 {
		r.ok("fleet unhold " + strings.Join(unhold, " "))
	}
	r.ok("fleet beat vision --load 1 --cores 8")
	r.ok("fleet up vision --probe passed")
}

// machineDown takes a member down as the machine does when it stops beating (a fleet
// step down with no hold and no mark): status down, held unset, so it is no gate bench
// (Snapshot.GateMembers). fleet down is not that: it is hold --return, a coordinator's hold.
func (r *landRig) machineDown(member string) {
	r.t.Helper()
	st, err := r.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(r.t, err)
	_, err = st.Run(context.Background(), store.FleetStep(sprint.FleetReq{Op: "down", Member: member, Who: "tester"}))
	require.NoError(r.t, err)
}

// downOthers takes every up member other than keep down by the machine (not held).
func (r *landRig) downOthers(keep ...string) {
	r.t.Helper()
	var w whereView
	r.json("where", &w)
	for name, row := range w.Tables["fleet"] {
		if row["status"] == sprint.Up && !slices.Contains(keep, name) {
			r.machineDown(name)
		}
	}
}

// loopGateCycles and loopGateSpins bound loopGate by steps: the loop's pauses, and the
// yields one pause waits for the landing in flight.
const (
	loopGateCycles = 1000
	loopGateSpins  = 1 << 26
)

// loopGate runs the land loop once through the bench seam (no ssh, no socket): every host
// the gate was asked on, and the loop's output.
func (r *landRig) loopGate() (asked []string, text string) {
	r.t.Helper()
	var mu sync.Mutex
	b := r.a.landState()
	b.mu.Lock()
	b.landMore = []string{"--repo-dir", r.clone, "--base", "main"}
	b.gateBench = func(ctx context.Context, host, dir string, runs [][]string, withGit bool) (string, int, error) {
		mu.Lock()
		asked = append(asked, host)
		mu.Unlock()
		return "", 0, nil
	}
	b.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out beatBuf
	// bounded by steps, never by the wall clock: the loop's pause returns once the landing
	// in flight is done (or none is), and the loop is stopped at its first LAND line or
	// after loopGateCycles pauses
	cycles := 0
	r.a.sleep = func(time.Duration) {
		cycles++
		text := out.String()
		if strings.Contains(text, "LAND REFUSED") || strings.Contains(text, "LAND OK") || strings.Contains(text, "LAND FAILED") || cycles > loopGateCycles {
			cancel()
			return
		}
		for spin := 0; spin < loopGateSpins; spin++ {
			b.mu.Lock()
			f := b.flight
			b.mu.Unlock()
			if f == nil {
				return
			}
			f.mu.Lock()
			done := f.done
			f.mu.Unlock()
			if done {
				return
			}
			runtime.Gosched()
		}
	}
	r.a.landLoop(ctx, "mem:0", &out)
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), asked...), out.String()
}

// A fleet hold is a deal control, not a gate one: with vision and space both held by the
// coordinator and one card merging, the gate's host choice is both held members, and the
// loop's gate is asked on one of them, with no LAND REFUSED line.
// tla/DirtyTick.tla LandGate (MCDirtyTickGateHeld.cfg; MCDirtyTickBrokenGateUpOnly.cfg is
// the up-only choice this replaces).
func TestAHeldMemberRunsLandGates(t *testing.T) {
	t.Parallel()
	r := heldGateRig(t)
	r.ok("fleet hold vision space --reason red --force-last")
	hosts, remote := r.heldGateHosts()
	assert.True(t, remote, "another member is configured")
	assert.ElementsMatch(t, []string{"vision", "space"}, hosts, "both held members may gate")

	asked, text := r.loopGate()
	require.NotEmpty(t, asked, "the gate went to a bench:\n%s", text)
	for _, h := range asked {
		assert.Contains(t, []string{"vision", "space"}, h, "the gate is asked on a held member: %v", asked)
	}
	assert.NotContains(t, text, "LAND REFUSED", "a hold does not refuse the landing:\n%s", text)
	assert.Contains(t, text, "LAND OK stream=s1", "the card lands:\n%s", text)
	r.benchBack("vision", "space")
	r.clean()
}

// A member down and not held is no gate bench: with space down by the machine (not held)
// and vision held, the choice is vision alone, and the gate is never asked on space.
func TestADownMemberRunsNoLandGate(t *testing.T) {
	t.Parallel()
	r := heldGateRig(t)
	r.machineDown("space")
	r.ok("fleet hold vision --reason red --force-last")
	hosts, _ := r.heldGateHosts()
	assert.Equal(t, []string{"vision"}, hosts, "the down member is left out")

	asked, text := r.loopGate()
	require.NotEmpty(t, asked, "the gate went to a bench:\n%s", text)
	assert.NotContains(t, asked, "space", "a down member runs no gate:\n%s", text)
	r.benchBack("vision")
	r.clean()
}

// land --dry-run names the bench the loop would gate on, or the refusal it would hit: with
// every member held it names a held member; with every member down and not held it is
// refused in the loop's words and counted under refused=, exiting 1. Nothing runs.
func TestLandDryRunNamesTheGateBench(t *testing.T) {
	t.Parallel()
	r := heldGateRig(t)
	// the clone holds the module, as a landing clone holds its last landed tree: the dry
	// run reads the gates kind from the clones files (go.mod), running no git
	r.git(r.clone, "fetch", "-q", "origin", "+refs/heads/main:refs/remotes/origin/main")
	r.git(r.clone, "switch", "-q", "--no-track", "--force-create", "land/s1", "refs/remotes/origin/main")
	r.ok("fleet hold vision space --reason red --force-last")
	out := r.ok("land --repo-dir " + r.clone + " --base main --dry-run")
	assert.Regexp(t, `LAND OK stream=s1 .* bench=(vision|space)( |$|\n)`, out, "the dry run names a held bench")
	assert.NotContains(t, out, "wall=", "a dry run ran no gate")

	r.ok("fleet unhold vision space")
	r.downOthers()
	code, sout, serr := r.do("land --repo-dir " + r.clone + " --base main --dry-run")
	text := sout + serr
	assert.Equal(t, 1, code, "the refusing exit:\n%s", text)
	assert.Contains(t, text, "LAND REFUSED stream=s1", "the batch is refused:\n%s", text)
	assert.Contains(t, text, "reason=the configured remote bench did not run the tree gate", "in the loop's words:\n%s", text)
	assert.Contains(t, text, "refused=1", "counted under refused=:\n%s", text)
	r.benchBack()
	r.clean()
}
