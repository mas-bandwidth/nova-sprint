package worker

// Breaker findings against the runner (2026-11-06, at 6bb85564). The restart
// and epoch tests are unit (store.Fake, the package's fakes, clock from 1000;
// a "kill" is the first runner frozen at a step while a second runner starts on
// the same root, which is exactly the disk a SIGKILL leaves). The wall tests
// are functional (real processes through OSReserveWall).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestBreakRestartDuringHarnessReportsFaultAndRemovesLiveJobDir
//
// Sequence: runner 1 takes p1.w1 and starts its harness (pid 4242). While the
// harness runs, runner 1 is killed; runner 2 starts on the same root with a
// process table in which pid 4242 is alive, and runs RecoverRow.
//
// Broken: Guarantee 7 ("One live harness per job directory") and the
// design's restart rule ("A working consumer whose harness is alive is
// adopted by pid and nonce"). runStep_internal writes rec.PID only after
// Wall.RunHarness returns, so while the harness runs job.json has pid 0;
// runner 2 cannot find it alive, reports the consumer fault ("restart:
// harness exited while runner was down") and removes the job directory
// under the running harness, and the producer is dealt again while the old
// harness still works. (cmd/nova-worker also wires no ProcessTable at all,
// so Procs is nil and nothing is ever adopted in the shipped binary.)
func TestBreakRestartDuringHarnessReportsFaultAndRemovesLiveJobDir(t *testing.T) {
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	root := t.TempDir()
	inRun, release := make(chan Job, 1), make(chan struct{})
	wall := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		inRun <- j
		<-release
		return WallRunResult{ExitCode: 0, PID: 4242, Nonce: "n4242", Elapsed: time.Second}, nil
	}}
	r1, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: git, Wall: wall, Now: clock})
	done := make(chan error, 1)
	go func() { done <- r1.StepRow(context.Background(), row) }()
	j := <-inRun
	t.Cleanup(func() { close(release); <-done })

	rec, _ := ReadJobRecord(j.JobDir)
	procs := &fakeProcTable{alive: map[string]bool{"4242:n4242": true}}
	r2, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: git, Wall: &fakeWall{}, Procs: procs, Now: clock})
	if err := r2.RecoverRow(context.Background(), row); err != nil {
		t.Fatalf("RecoverRow: %v", err)
	}
	snap, _ := st.Snapshot(context.Background())
	_, statErr := os.Stat(j.JobDir)
	if rec.PID == 0 || snap.Cards[j.Consumer].State != card.Working || statErr != nil {
		t.Fatalf("harness pid 4242 is running in %s; job.json had pid=%d state=%s; after restart consumer %s is %s (result %q, reason %q), producer %s, job dir exists=%v",
			j.JobDir, rec.PID, rec.State, j.Consumer, snap.Cards[j.Consumer].State, snap.Cards[j.Consumer].Result,
			snap.Cards[j.Consumer].Reason, snap.Cards["p1"].State, statErr == nil)
	}
}

// blockingRebaseGit_internal freezes the runner inside step 6 (rebase).
type blockingRebaseGit_internal struct {
	*fakeGit
	entered chan struct{}
	release chan struct{}
}

func (g *blockingRebaseGit_internal) RebaseOntoStream(ctx context.Context, wt, ref string) (string, error) {
	g.entered <- struct{}{}
	<-g.release
	return g.fakeGit.RebaseOntoStream(ctx, wt, ref)
}

// TestBreakRestartAfterHarvestReportsOkWithoutRebaseOrTests
//
// Sequence: runner 1 runs p1.w1; the harness says ok and advances HEAD;
// harvest pushes it and writes job.json (state harvested, outcome ok). Runner
// 1 is killed inside step 6, before the rebase onto the stream head and the
// card's done-when (`go test ./edge/worker/...`). Runner 2 starts on the same
// root and runs RecoverRow.
//
// Broken: the design's lifecycle step 6 ("Rebase onto the stream head as it
// is now and re-run the card's test command ... push") before step 8's
// report: recoverWorkingJob_internal treats any record with a result id and
// an outcome as "reported again under that id" and sends the harvest's ok,
// so the producer goes to review at an un-rebased head whose tests never ran.
// The same record exists after a kill anywhere in steps 6 and 7.
func TestBreakRestartAfterHarvestReportsOkWithoutRebaseOrTests(t *testing.T) {
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	base := newFakeGit_internal()
	base.rebasedSHA = testRebSHA
	bg := &blockingRebaseGit_internal{fakeGit: base, entered: make(chan struct{}, 1), release: make(chan struct{})}
	sc := &StoreSprintClient{Store: st, Origin: base, Now: clock}
	root := t.TempDir()
	wall1 := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		base.mu.Lock()
		base.headByWT[j.WorktreeDir] = testWorkSHA
		base.mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{ExitCode: 0, PID: 501, Nonce: "n501", Elapsed: time.Second}, nil
	}}
	r1, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: bg, Wall: wall1, Now: clock})
	done := make(chan error, 1)
	go func() { done <- r1.StepRow(context.Background(), row) }()
	<-bg.entered
	t.Cleanup(func() { close(bg.release); <-done })

	wall2 := &fakeWall{}
	r2, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: base, Wall: wall2, Procs: &fakeProcTable{alive: map[string]bool{}}, Now: clock})
	if err := r2.RecoverRow(context.Background(), row); err != nil {
		t.Fatalf("RecoverRow: %v", err)
	}
	snap, _ := st.Snapshot(context.Background())
	p := snap.Cards["p1"]
	wall2.mu.Lock()
	tests := len(wall2.testCalls)
	wall2.mu.Unlock()
	if p.State == "review" && tests == 0 {
		t.Fatalf("after a kill before rebase, the restarted runner reported ok: p1 is in review at head %s (the stream rebase would give %s) and 0 of the card's done-when commands ran", p.Head, testRebSHA)
	}
}

// TestBreakStaleEpochJobReplaysOldReceipts
//
// Sequence: epoch 1: p1 is dealt to amd-1.ds and the runner takes, runs and
// reports p1.w1 (p1 to review). The coordinator clears (epoch 2), adds
// stream s1 and producer p1 again and starts it on the same row, so the new
// consumer is p1.w1 at the same gen. The runner steps the row.
//
// Broken: Built seam 3 (facts keyed by (epoch, kind, card, sha)) and
// Guarantee 10 ("A report is recorded at most once per result id, retried
// until recorded"): the take op id take-<consumer>-g<gen>-<row> and the
// result id res-<consumer>-g<gen> carry no epoch, op ids are global in the
// store, so epoch 2's take and report are answered by epoch 1's stored
// receipts: the runner runs the job and believes it reported, while epoch 2's
// p1.w1 is never taken or reported and its producer never leaves working.
func TestBreakStaleEpochJobReplaysOldReceipts(t *testing.T) {
	ctx := context.Background()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	root := t.TempDir()
	wall := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		git.mu.Lock()
		git.headByWT[j.WorktreeDir] = testWorkSHA
		git.mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{ExitCode: 0, PID: 600, Nonce: "n600", Elapsed: time.Second}, nil
	}}
	r, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: git, Wall: wall, Now: clock})
	if err := r.StepRow(ctx, row); err != nil {
		t.Fatalf("epoch 1 StepRow: %v", err)
	}
	snap, _ := st.Snapshot(ctx)
	if snap.Cards["p1"].State != "review" {
		t.Fatalf("epoch 1: p1 should be in review, is %s", snap.Cards["p1"].State)
	}

	if rec, err := st.Clear(ctx, store.Op{ID: "clear-1", Epoch: snap.Epoch, Actor: "friendF", Kind: "clear", At: clock()}); err != nil || !rec.Accepted {
		t.Fatalf("clear: err=%v rec=%+v", err, rec)
	}
	snap, _ = st.Snapshot(ctx)
	e2 := snap.Epoch
	rec, err := st.Apply(ctx, store.Op{ID: "setup-e2", Epoch: e2, Actor: "friendF", Kind: "setup", At: clock(), Writes: []store.Write{
		store.PutStream{Name: "s1", Repo: "mas-bandwidth/nova-sprint", Base: "main", Pos: 1},
		store.SetRowState{Row: row, State: "up"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "normal", Brief: "brief text", Prose: "implement feature",
			Paths: []string{"edge/worker"}, DoneWhen: []string{"go test ./edge/worker/..."}},
		store.SetCardState{Card: "p1", State: "ready"},
	}})
	if err != nil || !rec.Accepted {
		t.Fatalf("epoch 2 setup: err=%v reason=%s", err, rec.Reason)
	}
	snap, _ = st.Snapshot(ctx)
	plan, ref := card.Move(card.FromStore(snap.Cards["p1"]), nil, map[string]card.Row{row: card.RowFromStore(snap.Rows[row], 1, []string{"flash", "pro"})},
		card.StreamFromStore(snap.Streams["s1"]), nil, card.Start{Row: row, Branch: "sprint/s1/p1", Base: testBaseSHA})
	if ref != nil {
		t.Fatalf("epoch 2 Start refused: %s", ref.Reason)
	}
	if rec, err = st.Apply(ctx, store.Op{ID: "start-p1-e2", Epoch: e2, Actor: "sprint", Kind: "start", At: clock(), Expect: plan.Expect, Writes: plan.Writes}); err != nil || !rec.Accepted {
		t.Fatalf("epoch 2 start: err=%v reason=%s", err, rec.Reason)
	}

	if err := r.StepRow(ctx, row); err != nil {
		t.Fatalf("epoch 2 StepRow: %v", err)
	}
	snap, _ = st.Snapshot(ctx)
	if snap.Cards["p1"].State != "review" || snap.Cards["p1.w1"].State != "done" {
		wall.mu.Lock()
		calls := wall.calls
		wall.mu.Unlock()
		t.Fatalf("epoch %d: the runner ran the harness %d time(s) in all, yet p1 is %s and p1.w1 is %s (gen %d): take and report were answered by epoch 1's receipts",
			e2, calls, snap.Cards["p1"].State, snap.Cards["p1.w1"].State, snap.Cards["p1.w1"].Gen)
	}
}

// TestBreakHarnessOutputUnbounded
//
// Sequence: OSReserveWall.RunHarness runs a harness that prints 100 MB.
//
// Broken: Built, "Every child process runs with stdin closed (nil), bounded
// stdout/stderr ring buffers": execBounded_internal collects the harness's
// stdout and stderr in an unbounded bytes.Buffer, so the runner holds the
// whole output in memory (and writes all of it to harness.log); a harness
// that prints without end exhausts the runner's memory.
func TestBreakHarnessOutputUnbounded(t *testing.T) {
	dir := t.TempDir()
	w := &OSReserveWall{}
	res, err := w.RunHarness(context.Background(), Job{Consumer: "p1.w1", JobDir: dir, WorktreeDir: dir},
		[]string{"head", "-c", "100000000", "/dev/zero"}, nil, 60*time.Second)
	if err != nil {
		t.Fatalf("RunHarness: %v", err)
	}
	if len(res.Output) > 1<<20 {
		t.Fatalf("harness printed 100 MB and the runner kept %d bytes of it in memory", len(res.Output))
	}
}

// TestBreakHarnessTimeoutHangsOnBackgroundChild
//
// Sequence: OSReserveWall.RunHarness with a 300 ms timeout runs a harness
// that starts a background process (`sleep 20 &`, as an agent starting a dev
// server or a watcher does) and then never exits.
//
// Broken: Guarantee 13 and the Built claim of "mandatory policy timeouts":
// the deadline kills only the direct child; the background process keeps the
// output pipe open, and execBounded_internal sets no WaitDelay and kills no
// process group, so cmd.Run blocks until the grandchild exits (20 s here,
// forever for a server), and the runner is stuck past every deadline.
func TestBreakHarnessTimeoutHangsOnBackgroundChild(t *testing.T) {
	dir := t.TempDir()
	w := &OSReserveWall{}
	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		_, _ = w.RunHarness(context.Background(), Job{Consumer: "p1.w1", JobDir: dir, WorktreeDir: dir},
			[]string{"sh", "-c", "sleep 20 & sleep 20"}, nil, 300*time.Millisecond)
		done <- time.Since(start)
	}()
	select {
	case d := <-done:
		if d > 5*time.Second {
			t.Fatalf("RunHarness with a 300 ms timeout returned after %s", d)
		}
	case <-time.After(5 * time.Second):
		<-done
		t.Fatalf("RunHarness with a 300 ms timeout had not returned after 5 s (the background child holds the output pipe)")
	}
}
