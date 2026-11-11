package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// A stream's gates (store.Stream.Gates, the take receipt's stream_gates),
// 2026-11-10: cards changed code emitters without regenerating the
// committed generated tree three times in one night, each caught only at
// landing by CI, although every brief said "regenerate in the same commit".
// A rule a model must remember fails; the worker runs the stream's gates
// after a card's DONE-WHENs, at the same head, and a gate red at the card's
// base as well is the base's, not the card's.

// streamGateWall_internal is a wall that knows which tree its gates run
// in: the head, after the apply step, or a base sha a step checked out
// (baseArgv_internal). red names "<where> <cmd>" runs that exit 2 with a
// FAIL line; a run of a cmd in notRun exits 127 (not run). No real time.
type streamGateWall_internal struct {
	cr2Wall
	mu        sync.Mutex
	at        string
	runs      []string
	red       map[string]bool
	notRun    map[string]bool
	baseFails bool
}

func (w *streamGateWall_internal) RunStep(_ context.Context, _ Job, argv []string, _ time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(argv) == 5 && argv[3] == "base" {
		if w.baseFails {
			return WallRunResult{ExitCode: 128, Output: []byte("fatal: reference is not a tree: " + argv[4])}, nil
		}
		w.at = argv[4]
		w.runs = append(w.runs, "checkout "+argv[4])
		return WallRunResult{}, nil
	}
	w.at = "head"
	return WallRunResult{}, nil
}

func (w *streamGateWall_internal) RunTest(_ context.Context, _ Job, cmd string, _ time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	where := w.at + " " + cmd
	w.runs = append(w.runs, where)
	switch {
	case w.notRun[cmd]:
		return WallRunResult{ExitCode: 127, Output: []byte("sh: 1: " + cmd + ": not found")}, nil
	case w.red[where]:
		return WallRunResult{ExitCode: 2, Output: []byte("generated tree differs\nFAIL: gen/lua/fixed128.lua is not current\nmake: *** [generated-current] Error 1\n")}, nil
	}
	return WallRunResult{}, nil
}

func (w *streamGateWall_internal) ran() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.runs)
}

// streamGateRunner_internal is one worker on wall, its clock at 1600.
func streamGateRunner_internal(t *testing.T, wall ReserveWall) *Runner {
	t.Helper()
	clock := &gateClock_internal{sec: 1600}
	r, err := NewRunner(Runner{Now: clock.now, Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: &cr2Sprint{}, Wall: wall, Git: newFakeGit_internal(), DefaultHarness: &ScriptHarness{}, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// streamGateStep_internal runs the stream merge and gate step of consumer
// id, a harvested ok work job on base testPriorSHA with a 90-minute
// deadline, on r, and returns the record it left.
func streamGateStep_internal(t *testing.T, r *Runner, id string, doneWhen, gates []string) JobRecord {
	t.Helper()
	rec := JobRecord{Consumer: id, Kind: "work", Row: "vision.ds", Repo: "o/r", Forge: "github", Gen: 2, StreamRef: "sprint/s1",
		Branch: "sprint/s1/" + id, BaseSHA: testPriorSHA, PushedHead: testCommitSHA, StartedAt: 1000, DeadlineAt: 1000 + 5400,
		DoneWhen: doneWhen, StreamGates: gates, Outcome: Outcome{Result: "ok", Head: testCommitSHA}}
	jobDir := filepath.Join(r.RootDir, "jobs", id)
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.RootDir, RemotesDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, RemotesDir, id), []byte(recordRemote_internal(rec)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.rebaseAndTestStep_internal(context.Background(), jobDir, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

const genGate = "make generated-current"

// The stream's gates run after the card's DONE-WHENs, at the same head;
// all green, the attempt is ok and nothing runs at the base.
func TestStreamGatesRunAfterTheDoneWhens(t *testing.T) {
	t.Parallel()
	wall := &streamGateWall_internal{}
	rec := streamGateStep_internal(t, streamGateRunner_internal(t, wall), "p1.w1", []string{"make test"}, []string{genGate, "sh -c 'go vet ./...'"})
	if rec.Outcome.Result != "ok" || rec.State != JobRebased || rec.Outcome.Reason != "" {
		t.Fatalf("outcome %+v at %s, want ok with no reason", rec.Outcome, rec.State)
	}
	if want := []string{"head make test", "head " + genGate, "head sh -c 'go vet ./...'"}; !slices.Equal(wall.ran(), want) {
		t.Fatalf("ran %q, want %q", wall.ran(), want)
	}
}

// A card with stream gates and no DONE-WHEN of its own still has its
// merged head applied, and the gates run there.
func TestStreamGatesRunWithNoDoneWhen(t *testing.T) {
	t.Parallel()
	wall := &streamGateWall_internal{at: "staged"}
	rec := streamGateStep_internal(t, streamGateRunner_internal(t, wall), "p1.w1", nil, []string{genGate})
	if rec.Outcome.Result != "ok" {
		t.Fatalf("outcome %+v, want ok", rec.Outcome)
	}
	if want := []string{"head " + genGate}; !slices.Equal(wall.ran(), want) {
		t.Fatalf("ran %q, want %q: the merged head is applied before the stream's gates", wall.ran(), want)
	}
}

// A red DONE-WHEN fails the attempt before any stream gate runs.
func TestARedDoneWhenStopsBeforeTheStreamGates(t *testing.T) {
	t.Parallel()
	wall := &streamGateWall_internal{red: map[string]bool{"head make test": true}}
	rec := streamGateStep_internal(t, streamGateRunner_internal(t, wall), "p1.w1", []string{"make test"}, []string{genGate})
	if rec.Outcome.Result != "fail" || !strings.HasPrefix(rec.Outcome.Reason, "tests red (make test)") {
		t.Fatalf("outcome %+v, want the DONE-WHEN's fail", rec.Outcome)
	}
	if want := []string{"head make test"}; !slices.Equal(wall.ran(), want) {
		t.Fatalf("ran %q, want %q", wall.ran(), want)
	}
}

// A stream gate red at the head and green at the base is the card's: the
// attempt fails, its reason names the stream gate and keeps the lines that
// say what failed.
func TestAStreamGateRedOnlyAtTheHeadFailsTheAttempt(t *testing.T) {
	t.Parallel()
	wall := &streamGateWall_internal{red: map[string]bool{"head " + genGate: true}}
	rec := streamGateStep_internal(t, streamGateRunner_internal(t, wall), "p1.w1", []string{"make test"}, []string{genGate})
	if rec.Outcome.Result != "fail" || rec.State == JobRebased {
		t.Fatalf("outcome %+v at %s, want a fail: the card broke the stream's gate", rec.Outcome, rec.State)
	}
	for _, want := range []string{"stream gate red (" + genGate + ")", "FAIL: gen/lua/fixed128.lua is not current", "make: *** [generated-current] Error 1"} {
		if !strings.Contains(rec.Outcome.Reason, want) {
			t.Errorf("reason %q lacks %q", rec.Outcome.Reason, want)
		}
	}
	if want := []string{"head make test", "head " + genGate, "checkout " + testPriorSHA, testPriorSHA + " " + genGate}; !slices.Equal(wall.ran(), want) {
		t.Fatalf("ran %q, want %q", wall.ran(), want)
	}
}

// A stream gate red at the head and red at the base the card started from
// is the base's fault: the attempt is not failed for it, and its reason
// says so for the coordinator. The answer at the base is kept for the life
// of the worker: a second card on the same base runs the gate there no
// more, and a card on another base does.
func TestAStreamGateRedAtTheBaseIsNotTheCards(t *testing.T) {
	t.Parallel()
	wall := &streamGateWall_internal{red: map[string]bool{"head " + genGate: true, testPriorSHA + " " + genGate: true}}
	r := streamGateRunner_internal(t, wall)
	rec := streamGateStep_internal(t, r, "p1.w1", []string{"make test"}, []string{genGate})
	if rec.Outcome.Result != "ok" || rec.State != JobRebased {
		t.Fatalf("outcome %+v at %s, want ok: the gate is red at the base too", rec.Outcome, rec.State)
	}
	if want := "stream gate " + genGate + " is red at base " + testPriorSHA; rec.Outcome.Reason != want {
		t.Fatalf("reason %q, want %q", rec.Outcome.Reason, want)
	}
	again := streamGateStep_internal(t, r, "p2.w1", nil, []string{genGate})
	if again.Outcome.Result != "ok" || !strings.Contains(again.Outcome.Reason, "is red at base "+testPriorSHA) {
		t.Fatalf("second card %+v, want ok with the base's red named", again.Outcome)
	}
	if want := []string{"head make test", "head " + genGate, "checkout " + testPriorSHA, testPriorSHA + " " + genGate, "head " + genGate}; !slices.Equal(wall.ran(), want) {
		t.Fatalf("ran %q, want %q: the base's answer is kept, the second card runs the gate at its head only", wall.ran(), want)
	}
}

// Only a gate that ran and exited non-zero is judged at the base: one that
// did not run (exit 127) is a fault as a DONE-WHEN's is, with no base run.
func TestAStreamGateThatDoesNotRunIsAFault(t *testing.T) {
	t.Parallel()
	wall := &streamGateWall_internal{notRun: map[string]bool{genGate: true}}
	rec := streamGateStep_internal(t, streamGateRunner_internal(t, wall), "p1.w1", nil, []string{genGate})
	if rec.Outcome.Result != "fault" || !strings.HasPrefix(rec.Outcome.Reason, "stream gate ("+genGate+") did not run") {
		t.Fatalf("outcome %+v, want a fault that names the stream gate", rec.Outcome)
	}
	if want := []string{"head " + genGate}; !slices.Equal(wall.ran(), want) {
		t.Fatalf("ran %q, want %q", wall.ran(), want)
	}
}

// A base the job's tree cannot check out answers nothing: the gate red at
// the head fails the attempt as before, and nothing is kept.
func TestAStreamGateWhoseBaseCannotBeCheckedOutFails(t *testing.T) {
	t.Parallel()
	wall := &streamGateWall_internal{red: map[string]bool{"head " + genGate: true}, baseFails: true}
	r := streamGateRunner_internal(t, wall)
	rec := streamGateStep_internal(t, r, "p1.w1", nil, []string{genGate})
	if rec.Outcome.Result != "fail" || !strings.HasPrefix(rec.Outcome.Reason, "stream gate red ("+genGate+")") {
		t.Fatalf("outcome %+v, want the head's red", rec.Outcome)
	}
	if len(r.baseGates) != 0 {
		t.Fatalf("kept %v, want nothing: the base was never judged", r.baseGates)
	}
}

// JOB.md lists the stream's gates after the card's done-when lines, and
// the finish rules say what a gate red at the base too means; a stream
// with none adds nothing.
func TestJobMDCarriesTheStreamGates(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"work", "read"} {
		j := Job{Consumer: "p1.w1", Kind: kind, Stream: "s1", Repo: "o/r", Branch: "sprint/s1/p1", BaseSHA: testPriorSHA, Head: testCommitSHA,
			JobDir: t.TempDir(), WorktreeDir: "/w", DoneWhen: []string{"make test"}, StreamGates: []string{genGate}, CommitName: "Bot", CommitEmail: "bot@x"}
		if err := writeJobMD_internal(j); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(j.JobDir, JobMDName))
		if err != nil {
			t.Fatal(err)
		}
		md := string(b)
		for _, want := range []string{"done-when: make test\nstream gate: " + genGate + "\n", "Run every stream gate", "red at the base the card started from (" + testPriorSHA + ")"} {
			if !strings.Contains(md, want) {
				t.Errorf("%s JOB.md lacks %q:\n%s", kind, want, md)
			}
		}
		kin := KinFinishRules(j, KinFinish{Ref: "p1.w1@2", Row: "stella", Epoch: 1})
		if !strings.Contains(kin, "stream gate <command> is red at base "+testPriorSHA) {
			t.Errorf("%s kin rules lack how to name a base's red gate:\n%s", kind, kin)
		}
		j.StreamGates = nil
		if strings.Contains(finishRules_internal(j), "stream gate") || strings.Contains(KinFinishRules(j, KinFinish{Ref: "p1.w1@2", Row: "stella", Epoch: 1}), "stream gate") {
			t.Errorf("%s: a stream with no gates names stream gates in its rules", kind)
		}
	}
}
