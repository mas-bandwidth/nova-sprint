package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-15 ~13:29Z: the lander's remote tree gate on bench amd-1 refused v1.3/comms as red
// on its class build and reworked a card, on "go build ./...: exit status 1 on the bench
// amd-1: GATE RUN: go build ./..." with nothing after the mark: amd-1's per-user /tmp quota
// was full, go could not make its work dir, the file holding the run's output could not be
// written either, and the shell's own line was printed before the mark, where benchFault
// clears what it found. A bench that cannot run the gate is the bench's fault (tla/GateRing.tla
// AskRefused, tla/LandBisect.tla Fault): the ring steps past it, and only when every slot
// is full is the gate unavailable, which blames no base, names no fix-red card and reworks
// no head.

// incidentShellLine is the line the shell printed when the run's output file could not be
// written (dash's wording, with the mark printed after it).
const incidentShellLine = "sh: 1: cannot create /tmp/tmp.X9fK2q/r_0_0: Disk quota exceeded"

// benchFullRig is a land rig with two benches up and the lander in the server's loop (the
// bench seam answers for them), the gate seam answering per host by answer.
type benchFullRig struct {
	r      *landRig
	l      *lander
	hosts  []string
	mu     sync.Mutex
	asked  []string
	answer func(host string) (string, int, error)
}

func newBenchFullRig(t *testing.T) *benchFullRig {
	t.Helper()
	r := newLandRig(t)
	for file, content := range goModule {
		path := filepath.Join(r.clone, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	hosts, _ := r.twoBenchesRing()
	f := &benchFullRig{r: r, hosts: hosts}
	b := r.a.landState()
	b.mu.Lock()
	b.flight = &landFlight{}
	b.hostName = func() (string, error) { return "coordinator.local", nil }
	b.gateBench = func(_ context.Context, host, _ string, _ [][]string, _ bool) (string, int, error) {
		f.mu.Lock()
		f.asked = append(f.asked, host)
		f.mu.Unlock()
		return f.answer(host)
	}
	b.mu.Unlock()
	st, err := r.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	f.l = &lander{a: r.a, st: st, gateKey: "s1", base: "main"}
	return f
}

// said is the batch's NOTE lines of the tree gate.
func (f *benchFullRig) said() string { return strings.Join(f.l.ledgerLog, "\n") }

func TestAFullBenchIsNotARedBase(t *testing.T) {
	t.Parallel()

	// (a) go could not make its work dir on either bench: every slot is full, so the gate
	// is unavailable. The base is not red (no retry spent, nothing cached), no fix-red card
	// is named, and the batch says which benches and the stderr line.
	t.Run("a base gate on full benches is unavailable, not red", func(t *testing.T) {
		t.Parallel()
		f := newBenchFullRig(t)
		const out = "GATE RUN: go build ./...\ngo: creating work dir: mkdir /tmp/go-build123: disk quota exceeded"
		f.answer = func(string) (string, int, error) { return out, 1, nil }
		why, stop := f.l.treeGateBase(t.Context(), f.r.clone, "base-1")
		assert.Equal(t, benchGateUnavailableWhy, why)
		assert.False(t, stop)
		assert.Empty(t, f.l.baseGateFails, "no red-base retry is spent")
		assert.Empty(t, f.l.baseGateCache, "nothing is cached for the base")
		assert.NotContains(t, why, "fix-red")
		assert.NotContains(t, why, "fails the tree gate")
		said := f.said()
		for _, h := range f.hosts {
			assert.Contains(t, said, h, "each full bench is named: %s", said)
		}
		assert.Contains(t, said, "the bench's disk", said)
		assert.Contains(t, said, "go: creating work dir: mkdir /tmp/go-build123: disk quota exceeded", "the stderr line is carried: %s", said)
		assert.ElementsMatch(t, f.hosts, f.asked, "each bench was asked once")
		f.r.clean()
	})

	// (b) the script's preflight said the first bench full and exited with its own
	// status: the ring steps to the second, where the gate runs green.
	t.Run("a bench that says it is full is stepped past", func(t *testing.T) {
		t.Parallel()
		f := newBenchFullRig(t)
		ring := benchRing("s1", f.hosts)
		full, next := ring[0], ring[1]
		f.answer = func(host string) (string, int, error) {
			if host == full {
				return benchFullMark + "/home/gate/.cache/nova-gate/run.AbC123 1024K free, 2097152K needed\n", benchFullExit, nil
			}
			return "", 0, nil
		}
		runs := gateCmds(gateRuns(false, nil, nil))
		l := &lander{a: f.r.a, st: f.l.st, gateKey: "s1"}
		why, ran := l.benchGate(t.Context(), f.hosts, f.r.clone, runs, false)
		assert.Empty(t, why)
		assert.True(t, ran, "the gate ran on the second bench")
		assert.Contains(t, l.gateHost, next, "the bench the gate ran on")
		assert.Equal(t, []string{full, next}, f.asked, "the full bench is asked first (the ring's order), then the next")
		assert.Contains(t, f.say(l), full+" after", "the full bench is named")
		assert.Contains(t, f.say(l), "1024K free, 2097152K needed")
		f.r.clean()
	})

	// (c) the incident's exact output: the mark and nothing after it, the shell's own
	// line printed before it. A bench fault, not a red tree.
	t.Run("the incident's output is a bench fault", func(t *testing.T) {
		t.Parallel()
		f := newBenchFullRig(t)
		f.answer = func(string) (string, int, error) {
			return incidentShellLine + "\nGATE RUN: go build ./...\n", 1, nil
		}
		why, stop := f.l.treeGateBase(t.Context(), f.r.clone, "base-1")
		assert.Equal(t, benchGateUnavailableWhy, why, "the old verdict was: go build ./...: exit status 1 on the bench: GATE RUN: go build ./...")
		assert.False(t, stop)
		assert.Empty(t, f.l.baseGateFails)
		assert.Contains(t, f.said(), "Disk quota exceeded", "the shell's line is kept with the run: %s", f.said())
		f.r.clean()
	})

	// (d) a real compile error is still a red tree, and its refusal carries the line.
	t.Run("a syntax error is still a red tree with its line", func(t *testing.T) {
		t.Parallel()
		f := newBenchFullRig(t)
		f.answer = func(string) (string, int, error) {
			return "GATE RUN: go build ./...\n# example.com/m\n./bad.go:3:14: syntax error: unexpected newline\n", 1, nil
		}
		why, _ := f.l.treeGateBase(t.Context(), f.r.clone, "base-1")
		assert.NotEqual(t, benchGateUnavailableWhy, why)
		assert.Contains(t, why, "./bad.go:3:14: syntax error: unexpected newline")
		require.NotNil(t, f.l.baseGateFails["base-1"], "a red base spends a retry")
		assert.Equal(t, 1, f.l.baseGateFails["base-1"].n)
		assert.Len(t, f.asked, 1, "a red tree is not stepped past")
		f.r.clean()
	})
}

// say is the NOTE lines of l.
func (f *benchFullRig) say(l *lander) string { return strings.Join(l.ledgerLog, "\n") }

// A red run's refusal keeps the end of its output, not only the front, and a run that
// printed nothing says so with the script's own lines.
func TestARedGateKeepsTheTailOfItsOutput(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("GATE RUN: go test ./...\n")
	for i := range 200 {
		b.WriteString("noise line " + strings.Repeat("x", 20) + " " + string(rune('a'+i%26)) + "\n")
	}
	b.WriteString("./late.go:9:1: the last stderr line\n")
	why := gateWhy([]string{"go", "test", "./..."}, assert.AnError, b.String())
	assert.Contains(t, why, "the last stderr line", "the end of a long output is kept")
	assert.LessOrEqual(t, len(why), 1700)

	none := gateWhy([]string{"go", "build", "./..."}, assert.AnError, "sh: 1: some shell line\nGATE RUN: go build ./...\n")
	assert.Contains(t, none, "no output")
	assert.Contains(t, none, "sh: 1: some shell line", "the script's own lines are kept")
}

// The gate script runs in a directory of its own under the bench's home disk, never /tmp,
// checked before the first run, and removes it when it ends.
func TestTheGateScriptRunsOffTmpAndChecksItsDiskFirst(t *testing.T) {
	t.Parallel()
	script := gateScript([][]string{{"go", "build", "./..."}, {"go", "vet", "./..."}})
	for _, want := range []string{
		`"$HOME"/.cache/nova-gate`,
		"mktemp -d",
		"GOTMPDIR",
		"TMPDIR",
		"df -Pk",
		`trap 'rm -rf "$gate_dir"' EXIT`,
		benchFullMark,
	} {
		assert.Contains(t, script, want)
	}
	assert.NotContains(t, strings.ReplaceAll(script, `"$HOME"/.cache`, ""), "mktemp -d\n", "no bare mktemp -d")
	first := strings.Index(script, "echo 'GATE RUN: ")
	require.GreaterOrEqual(t, first, 0)
	assert.Less(t, strings.Index(script, "df -Pk"), first, "the free-space check runs before any run")
	assert.Less(t, strings.Index(script, "dd if=/dev/zero"), first, "the write probe runs before any run")
	assert.Less(t, strings.Index(script, "export GOTMPDIR"), strings.Index(script, "('go' 'build'"), "go's work dir is moved before any run")
	assert.NotContains(t, script, "/tmp/", "nothing is written to /tmp")
}
