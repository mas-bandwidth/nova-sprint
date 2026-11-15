package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

// A gate's red run that says the bench lacks a tool or the network names
// the route it lacks; one that says nothing of the bench names none.
func TestMissingRouteNamesWhatTheBenchLacks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, out, route string
		exit             int
	}{
		{"the shell's missing command", "/bin/sh: elixir: command not found\nmake: *** [Makefile:3: test] Error 127", "elixir", 2},
		{"dash's missing command", "make: *** [Makefile:3: test] Error 127\n/bin/sh: 1: cargo: not found", "cargo", 2},
		{"a missing command run by the shell itself", "sh: 1: luau: not found", "luau", 127},
		{"bash's missing command", "bash: line 1: sbcl: command not found", "sbcl", 127},
		{"zsh's missing command", "zsh: command not found: dart", "dart", 127},
		// make runs a plain recipe itself (measured: GNU make 3.81 and 4.4.1).
		{"make 3.81's missing command", "make: nosuchtoolxyz: No such file or directory\nmake: *** [a] Error 1", "nosuchtoolxyz", 2},
		{"gmake 4.4's missing command", "gmake: nosuchtoolxyz: No such file or directory\ngmake: *** [Makefile:2: a] Error 127", "nosuchtoolxyz", 2},
		{"a sub-make's missing command", "make[1]: Entering directory '/w'\nmake[1]: zig: Command not found\nmake[1]: *** [t] Error 127", "zig", 2},
		// The wall's own word for a gate's program on no PATH entry (measured).
		{"the gate's program on no PATH entry", "SANDBOX REFUSED reason=not_found: cargo is on no PATH entry or does not exist", "cargo", 127},
		{"a python launcher's missing tool", "make: *** [test] Error 1\nFileNotFoundError: [Errno 2] No such file or directory: 'dotnet'", "dotnet", 2},
		{"rustc's linker", "error: linker `cc` not found\n  |\n  = note: No such file or directory (os error 2)", "linker", 101},
		{"rustc's linker, older", "error: could not exec the linker `cc`: No such file or directory (os error 2)", "linker", 101},
		{"rustc's std target", "error[E0463]: can't find crate for `std`\n  |\n  = note: the `wasm32-unknown-unknown` target may not be installed", "rust-target", 101},
		{"rustup's target", "error: component 'rust-std' for target 'aarch64-linux-android': target not installed", "rust-target", 1},
		{"go's module lookup", "go: github.com/a/b@v1.2.3: module lookup disabled by GOPROXY=off", "network", 1},
		{"git's unresolved host", "fatal: unable to access 'https://github.com/a/b.git/': Could not resolve host: github.com", "network", 128},
		{"curl's unresolved host", "curl: (6) Could not resolve host: example.com", "network", 6},
		{"ssh's unresolved host", "ssh: Could not resolve hostname github.com: Name or service not known\nfatal: Could not read from remote repository.", "network", 128},
		{"the last such line names it", "/bin/sh: 1: jq: not found\nfatal: unable to access 'https://h/r.git/': Could not resolve host: h", "network", 1},
		// Refusals: each says nothing of the bench.
		{"a green run, whatever it printed", "/bin/sh: foo: command not found\ncurl: (6) Could not resolve host: x", "", 0},
		{"the wall's refusal", "SANDBOX REFUSED: --net-deny\nfatal: unable to access 'https://h/r.git/': Could not resolve host: h", "", 1},
		{"the wall's own exit", "fatal: unable to access 'https://h/r.git/': Could not resolve host: h", "", 126},
		{"a kill", "/bin/sh: 1: cargo: not found\nKilled", "", 137},
		{"a red test", "--- FAIL: TestX (0.00s)\n    x_test.go:9: want 2, got 1\nFAIL", "", 1},
		{"a red test whose error says not found", "--- FAIL: TestGet (0.00s)\n    x_test.go:9: get: not found\nFAIL\nmake: *** [test] Error 1", "", 2},
		{"a red test whose log quotes a resolver", "    fetch_test.go:30: got \"fatal: unable to access 'u': Could not resolve host: u\"\nFAIL", "", 1},
		{"a shell line naming no command", "/bin/sh: 1: : not found", "", 127},
		{"a file of the head's, by its path", "/bin/sh: 1: ./build.sh: not found\nmake: ./gen: No such file or directory", "", 2},
		{"a command of the wall's own", "/bin/sh: 1: nova-sandbox: not found", "", 127},
		{"a name no brief can declare", "/bin/sh: 1: g++: not found", "", 127},
	} {
		route, why := missingRoute_internal(WallRunResult{ExitCode: tc.exit, Output: []byte(tc.out)})
		if route != tc.route {
			t.Errorf("%s: route %q, want %q", tc.name, route, tc.route)
		}
		if route != "" && (!strings.HasPrefix(why, route+" missing in the wall") || !strings.Contains(why, strings.TrimSpace(tc.out[strings.LastIndexByte(tc.out, '\n')+1:]))) {
			t.Errorf("%s: why %q does not name the route and the output's end", tc.name, why)
		}
	}
}

func routeRunner_internal(t *testing.T, res WallRunResult, err error) (*Runner, Job) {
	t.Helper()
	dir := t.TempDir()
	wall := &fakeWall{testFn: func(Job, string) (WallRunResult, error) { return res, err }}
	r, nerr := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: dir, StateDir: dir, Wall: wall, Now: testClock_internal()})
	if nerr != nil {
		t.Fatal(nerr)
	}
	return r, Job{Consumer: "p1.w1", JobDir: dir, WorktreeDir: filepath.Join(dir, "repo")}
}

// A done-when, or a stream gate, the bench lacks the route for is a route
// fault, before the wall's exit codes (a shell's missing command exits
// 127): its reason starts with card.MissingRoute and the route, within the
// reason's bound however long the output; never a red.
func TestAGateTheBenchCannotRunIsARouteFault(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("compiling a crate\n", 400) + "fatal: unable to access 'https://forge/r.git/': Could not resolve host: forge"
	for _, tc := range []struct {
		name  string
		res   WallRunResult
		kind  gateKind
		route string
	}{
		{"a missing command, exit 127", WallRunResult{ExitCode: 127, Output: []byte("/bin/sh: 1: dart: not found")}, doneWhenGate, "dart"},
		{"the gate's program on no PATH entry", WallRunResult{ExitCode: 127, Output: []byte("SANDBOX REFUSED reason=not_found: cargo is on no PATH entry or does not exist")}, doneWhenGate, "cargo"},
		{"no network, a long output", WallRunResult{ExitCode: 128, Output: []byte(long)}, doneWhenGate, "network"},
		{"a stream gate's missing linker", WallRunResult{ExitCode: 101, Output: []byte("note: could not exec the linker `cc`")}, streamGate, "linker"},
	} {
		r, j := routeRunner_internal(t, tc.res, nil)
		rec := JobRecord{PushedHead: testWorkSHA}
		out, red, testsRed := r.doneWhen_internal(context.Background(), j, rec, tc.kind, "make test")
		if out.Result != "fault" || !red || testsRed {
			t.Errorf("%s: %+v red=%v testsRed=%v, want a fault that no stream gate's base can excuse", tc.name, out, red, testsRed)
		}
		if what, ok := card.RouteFault(out.Reason); !ok || card.RouteName(out.Reason) != tc.route || !strings.Contains(what, tc.kind.name+" (make test) did not run") {
			t.Errorf("%s: reason %q, want a route fault naming %s and the gate", tc.name, out.Reason, tc.route)
		}
		if len(out.Reason) > MaxReasonBytes || out.Head != testWorkSHA {
			t.Errorf("%s: reason of %d bytes (bound %d), head %q: want it bounded, at the pushed head", tc.name, len(out.Reason), MaxReasonBytes, out.Head)
		}
	}
}

// What the route does not cover keeps its own kind: the wall's refusal and
// its execution fault are plain faults, dealt again; a kill is the killed
// fail, whatever ran before it; a red test is a red.
func TestAGateRouteFaultRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		res    WallRunResult
		err    error
		result string
	}{
		{"the wall's refusal names the network", WallRunResult{ExitCode: 126, Output: []byte("SANDBOX REFUSED\nCould not resolve host: forge")}, nil, "fault"},
		{"the wall could not run it", WallRunResult{ExitCode: 1, Output: []byte("/bin/sh: elixir: command not found")}, errors.New("exec: nova-reserve: not found"), "fault"},
		{"a missing binary with no shell's line", WallRunResult{ExitCode: 127, Output: []byte("exec failed")}, nil, "fault"},
		{"a kill after a missing command", WallRunResult{ExitCode: 137, Output: []byte("/bin/sh: 1: cargo: not found\nKilled")}, nil, "fail"},
		{"a red test", WallRunResult{ExitCode: 1, Output: []byte("--- FAIL: TestX")}, nil, "fail"},
	} {
		r, j := routeRunner_internal(t, tc.res, tc.err)
		out, red, _ := r.doneWhen_internal(context.Background(), j, JobRecord{}, doneWhenGate, "make test")
		if _, route := card.RouteFault(out.Reason); !red || out.Result != tc.result || route {
			t.Errorf("%s: %+v red=%v, want a %s that is no route fault", tc.name, out, red, tc.result)
		}
	}
}

// Through the whole step: a done-when the bench has no network for is
// reported a route fault, so the producer waits for the coordinator with
// that reason; the head is not pushed, and no bounce is spent.
func TestNoNetworkForADoneWhenLeavesTheCardWaiting(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	git.rebasedSHA = testRebSHA
	wall := &fakeWall{
		runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
			git.mu.Lock()
			git.headByWT[j.WorktreeDir] = testWorkSHA
			git.mu.Unlock()
			_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
			return WallRunResult{ExitCode: 0, Elapsed: time.Second}, nil
		},
		testFn: func(Job, string) (WallRunResult, error) {
			return WallRunResult{ExitCode: 1, Output: []byte("go: github.com/a/b@v1.2.3: module lookup disabled by GOPROXY=off")}, nil
		},
	}
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	root := t.TempDir()
	r, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: git, Wall: wall, Now: clock})
	if err := r.StepRow(context.Background(), row); err != nil {
		t.Fatalf("StepRow: %v", err)
	}
	snap, _ := st.Snapshot(context.Background())
	w, p := snap.Cards["p1.w1"], snap.Cards["p1"]
	if w.Result != "fault" || card.RouteName(w.Reason) != "network" {
		t.Fatalf("the work card: %+v, want a route fault naming network", w)
	}
	if p.State != card.Waiting || card.RouteName(p.Reason) != "network" || p.Bounces != 0 {
		t.Fatalf("the producer: %+v, want it waiting on the route, no bounce spent", p)
	}
	if p.Head == testRebSHA {
		t.Fatalf("the rebased head was pushed although its gate did not run: %+v", p)
	}
}

// A stream gate red at the head whose run at the base lacked a route is
// that route fault, not the plain fault dealt again for ever: a bench of
// the kind that took it cannot judge it. Nothing is kept for the base.
func TestAStreamGateBaseWithNoRouteIsARouteFault(t *testing.T) {
	t.Parallel()
	at := testPriorSHA + " " + genGate
	wall := &streamGateWall_internal{red: map[string]bool{"head " + genGate: true},
		res: map[string]WallRunResult{at: {ExitCode: 128, Output: []byte("fatal: unable to access 'https://forge/r.git/': Could not resolve host: forge")}}}
	r := streamGateRunner_internal(t, wall)
	rec := streamGateStep_internal(t, r, "p1.w1", nil, []string{genGate})
	if rec.Outcome.Result != "fault" || card.RouteName(rec.Outcome.Reason) != "network" || !strings.Contains(rec.Outcome.Reason, "stream gate ("+genGate+") is red at the head and could not be judged at the base "+testPriorSHA) {
		t.Fatalf("outcome %+v, want the network's route fault naming the gate and the base", rec.Outcome)
	}
	if len(r.baseGates) != 0 {
		t.Fatalf("kept %v, want nothing: the base was never judged", r.baseGates)
	}
}
