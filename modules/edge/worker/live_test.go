package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

// The worker's live defects of 2026-11-07 (items 61, 66-73): passes apart
// from jobs, restart resumes, read outcomes, JOB.md, quarantine, one fetch
// per repo. Unit tier: fake sprint, fake wall, fake git, fake timer.

// liveWall runs fn for each harness run; nil fn writes {"result":"ok"}.
type liveWall struct {
	mu   sync.Mutex
	fn   func(ctx context.Context, j Job) (WallRunResult, error)
	runs []string
}

func (w *liveWall) RunHarness(ctx context.Context, j Job, _, _ []string, _ time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	w.runs = append(w.runs, j.Consumer)
	fn := w.fn
	w.mu.Unlock()
	if fn != nil {
		return fn(ctx, j)
	}
	return WallRunResult{PID: 4242, Nonce: "n-4242"}, os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
}
func (w *liveWall) RunStep(context.Context, Job, []string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}
func (w *liveWall) RunTest(context.Context, Job, string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}
func (w *liveWall) ran() []string { w.mu.Lock(); defer w.mu.Unlock(); return slices.Clone(w.runs) }

// liveSprint is cr2Sprint with a repo per card.
type liveSprint struct {
	cr2Sprint
	repos map[string]string
}

func (s *liveSprint) Take(ctx context.Context, row, id string, gen int) (TakeInfo, error) {
	t, err := s.cr2Sprint.Take(ctx, row, id, gen)
	t.Repo, t.Forge = s.repos[id], "github"
	return t, err
}

func liveWorkRunner_internal(t *testing.T, sp SprintClient, wall ReserveWall) (*Runner, *fakeGit) {
	t.Helper()
	r := cr2Runner_internal(t, sp, wall, &cr2Procs{}, time.Unix(1000, 0))
	g := newFakeGit_internal()
	r.Git = g
	return r, g
}

func workingNext_internal(kind string, ids ...string) NextResponse {
	n := NextResponse{Row: "vision.ds", State: card.Up, Width: len(ids)}
	for _, id := range ids {
		n.Working = append(n.Working, ConsumerSummary{ID: id, Kind: kind, Gen: 2, Attempt: 1})
	}
	return n
}

// 67: a read's report is ok, fix or fault; "fail" cannot be built, and a
// harness that wrote no verdict is a fault.
func TestLiveReadOutcomeIsOkFixOrFault(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   Outcome
		want ReadOutcome
		res  string
	}{
		{Outcome{Result: "ok"}, ReadOK, "ok"},
		{Outcome{Result: "fix", Reason: "a.go:1: x"}, ReadFix, "fix"},
		{Outcome{Result: "fail", Reason: "the head misses done-when"}, ReadFix, "fix"},
		{noVerdict_internal("the harness exited 0 without writing result.json", "tail"), ReadFault, "fault"},
		{Outcome{Result: "broken"}, ReadFault, "fault"},
		{Outcome{}, ReadFault, "fault"},
	} {
		got, out := ReadOutcomeOf(c.in)
		if got != c.want || out.Result != c.res || out.Result == "fail" {
			t.Errorf("ReadOutcomeOf(%+v) = %s %+v, want %s %s", c.in, got, out, c.want, c.res)
		}
	}
	if o := reportOutcome_internal(JobRecord{Kind: "work", Outcome: Outcome{Result: "fail", Reason: "r"}}); o.Result != "fail" {
		t.Errorf("a work job's fail stays fail: %+v", o)
	}
}

// 67 and the harness note: no result.json after exit 0 is a fault for a
// read and "fail: no verdict" with the harness's tail for a work job.
func TestLiveNoVerdictIsReadFaultAndWorkFail(t *testing.T) {
	t.Parallel()
	silent := &liveWall{fn: func(context.Context, Job) (WallRunResult, error) {
		return WallRunResult{Output: []byte("the harness's last words")}, nil
	}}
	for _, kind := range []string{"read", "work"} {
		sp := &cr2Sprint{kind: kind, next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Ready: []ConsumerSummary{{ID: "p1.c1", Kind: kind, Gen: 2}}}}
		r, _ := liveWorkRunner_internal(t, sp, silent)
		_ = r.RunConsumer(context.Background(), sp.next, sp.next.Ready[0])
		reps := sp.reported()
		want := map[string]string{"read": "fault", "work": "fail"}[kind]
		if len(reps) != 1 || reps[0].Result != want || !strings.Contains(reps[0].Reason, "no verdict") || !strings.Contains(reps[0].Reason, "last words") {
			t.Errorf("%s with no verdict: want %s naming no verdict and the tail, got %+v", kind, want, reps)
		}
	}
}

// The harness note: ok with a dirty tree and HEAD where it started is
// committed by the worker and pushed; ok with nothing to commit is a fail.
func TestLiveOkWithALeftTreeIsCommittedByTheWorker(t *testing.T) {
	t.Parallel()
	for _, dirty := range []bool{true, false} {
		var g *fakeGit
		wall := &liveWall{fn: func(_ context.Context, j Job) (WallRunResult, error) {
			g.mu.Lock()
			g.dirtyByWT[j.WorktreeDir] = dirty
			g.mu.Unlock()
			return WallRunResult{}, os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		}}
		sp := &cr2Sprint{kind: "work", next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Ready: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 2}}}}
		var r *Runner
		r, g = liveWorkRunner_internal(t, sp, wall)
		_ = r.RunConsumer(context.Background(), sp.next, sp.next.Ready[0])
		reps := sp.reported()
		switch {
		case len(reps) != 1:
			t.Fatalf("dirty=%v: want one report, got %+v", dirty, reps)
		case dirty && (reps[0].Result != "ok" || reps[0].Head != testCommitSHA || len(g.commits) != 1 || g.pushed["sprint/s1/p1"] != testCommitSHA):
			t.Errorf("a left tree must be committed and pushed: report %+v commits %v pushed %v", reps[0], g.commits, g.pushed)
		case !dirty && (reps[0].Result != "fail" || !strings.Contains(reps[0].Reason, "nothing to commit")):
			t.Errorf("ok with nothing done must be a fail naming it, got %+v", reps[0])
		}
	}
}

// 68: JOB.md holds the card's facts and the finish rules, and the prompt
// carries the same rules word for word; neither lets the harness push; a
// work card's rules carry the sprint's commit identity (round 24).
func TestLiveJobMDAndPromptAgree(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"read", "work"} {
		dir := t.TempDir()
		j := Job{Consumer: "p1.c1", Producer: "p1", Kind: kind, Tier: "t1", Stream: "s1", Repo: "o/r", Base: "main", BaseSHA: strings.Repeat("b", 40),
			Branch: "sprint/s1/p1", Head: strings.Repeat("a", 40), JobDir: dir, WorktreeDir: filepath.Join(dir, "repo"),
			Paths: []string{"x/y.go"}, DoneWhen: []string{"go test ./x"}, Prose: "do the thing", CommitName: testCommitName, CommitEmail: testCommitEmail}
		if err := writeJobMD_internal(j); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, JobMDName))
		if err != nil {
			t.Fatal(err)
		}
		md, prompt, rules := string(b), buildPrompt_internal(j), finishRules_internal(j)
		for _, want := range []string{"card: p1.c1", "repository: o/r", "base: main", "checkout: " + j.WorktreeDir, "paths: x/y.go", "done-when: go test ./x", "do the thing", rules} {
			if !strings.Contains(md, want) {
				t.Errorf("%s JOB.md lacks %q:\n%s", kind, want, md)
			}
		}
		if !strings.Contains(prompt, rules) || !strings.Contains(prompt, JobMDName) {
			t.Errorf("%s prompt does not carry JOB.md's finish rules:\n%s", kind, prompt)
		}
		if kind == "work" && !strings.Contains(rules, "merge it into the branch, never rebase") {
			t.Errorf("work finish rules must say a newer base is merged in, never rebased:\n%s", rules)
		}
		if !strings.Contains(rules, "no git push and no gh") {
			t.Errorf("%s finish rules must forbid push and gh:\n%s", kind, rules)
		}
		identity := strings.Contains(rules, `git config user.email "`+testCommitEmail+`"`) && strings.Contains(rules, `"Co-Authored-By: <your name> <your email>" trailer`)
		if identity != (kind == "work") {
			t.Errorf("%s finish rules carry the commit identity: %v, want %v:\n%s", kind, identity, kind == "work", rules)
		}
	}
}

// 66: a pass never waits on a job: while one job blocks, a wake's pass
// takes the next dealt card and runs it.
func TestLivePassTakesWhileAJobBlocks(t *testing.T) {
	t.Parallel()
	release, started := make(chan struct{}), make(chan string, 4)
	wall := &liveWall{fn: func(ctx context.Context, j Job) (WallRunResult, error) {
		started <- j.Consumer
		if j.Consumer == "p1.r1" {
			<-release
		}
		return WallRunResult{}, os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	}}
	sp := &cr2Sprint{next: workingNext_internal("read", "p1.r1")}
	sp.next.Width = 2
	r := cr2Runner_internal(t, sp, wall, &cr2Procs{}, time.Unix(1000, 0))
	hook := newFakeWakeHook()
	r.BusHook = func(context.Context, string) (WakeHook, error) { return hook, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.RunWakeLoop(ctx, "vision.ds") }()
	if c := <-started; c != "p1.r1" {
		t.Fatalf("first job %s", c)
	}
	sp.mu.Lock()
	sp.next.Working = append(sp.next.Working, ConsumerSummary{ID: "p2.r1", Kind: "read", Gen: 2, Attempt: 1}) // dealt into the hand
	sp.mu.Unlock()
	hook.wake <- struct{}{}
	if c := <-started; c != "p2.r1" {
		t.Fatalf("a wake while p1.r1 runs must take p2.r1, got %s", c)
	}
	close(release)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("the loop ends clean at ctx end: %v", err)
	}
}

// 66: a periodic wake runs a pass beside the hook's wakes.
func TestLivePeriodicWakeRunsAPass(t *testing.T) {
	t.Parallel()
	tick := make(chan time.Time)
	sp := &loopSprint{next: NextResponse{Row: "vision.ds", State: card.Up}, nexts: make(chan struct{}, 16)}
	hook := newFakeWakeHook()
	r, _ := loopRunner_internal(t, sp, hook, &liveWall{})
	r.After = func(time.Duration) <-chan time.Time { return tick }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.RunWakeLoop(ctx, "vision.ds") }()
	<-sp.nexts // the first pass reads the row (recovery, then the deal)
	<-sp.nexts
	tick <- time.Time{}
	<-sp.nexts // a pass with no hook wake: the periodic one
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// 70, 73: a job taken but never run (no slot) is staged again and run by
// the next pass or the next worker, with no fault; one that was running
// when the worker died is run again; neither is released.
func TestLiveRestartResumesWithNoFault(t *testing.T) {
	t.Parallel()
	for _, state := range []string{JobStaged, JobTaken, JobRunning} {
		sp := &cr2Sprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Ready: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
		r1 := cr2Runner_internal(t, sp, &cr2Wall{err: ErrNoSlot}, &cr2Procs{}, time.Unix(1000, 0))
		if err := r1.RunConsumer(context.Background(), sp.next, sp.next.Ready[0]); !errors.Is(err, ErrNoSlot) {
			t.Fatalf("want no slot, got %v", err)
		}
		dir := filepath.Join(r1.RootDir, "jobs", "p1.r1")
		rec, err := ReadJobRecord(dir)
		if err != nil || rec.State != JobStaged {
			t.Fatalf("a job with no slot stays staged: %+v %v", rec, err)
		}
		rec.State, rec.PID, rec.Nonce = state, 77, "n-77" // the worker died here
		_ = WriteJobRecord(dir, rec)
		sp.next = workingNext_internal("read", "p1.r1")
		wall := &liveWall{}
		r2, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "vision", RootDir: r1.RootDir, Sprint: sp, Wall: wall, Procs: &cr2Procs{}, DefaultHarness: &ScriptHarness{}, Now: r1.Now})
		_ = r2.RecoverRow(context.Background(), "vision.ds")
		reps := sp.reported()
		if len(wall.ran()) != 1 || len(reps) != 1 || reps[0].Result != "ok" || len(sp.releases) != 0 {
			t.Errorf("from %s: want it run again and reported ok, ran %v reports %+v releases %v", state, wall.ran(), reps, sp.releases)
		}
	}
}

// 73 under deal is take (defect 52): a card in the row's hand that the
// restarted worker has no job directory for is the pass's to run (its take
// is the acknowledgement), never released and never "runner lost the job".
func TestLiveRestartWithNoJobDirRunsTheCard(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{next: workingNext_internal("read", "p1.r1")}
	wall := &liveWall{}
	r := cr2Runner_internal(t, sp, wall, &cr2Procs{}, time.Unix(1000, 0))
	_ = r.RecoverRow(context.Background(), "vision.ds")
	if len(sp.reported()) != 0 || len(sp.releases) != 0 || len(wall.ran()) != 0 {
		t.Fatalf("recovery leaves a dealt card with no job alone: releases %v reports %+v ran %v", sp.releases, sp.reported(), wall.ran())
	}
	_ = r.StepRow(context.Background(), "vision.ds")
	if reps := sp.reported(); len(reps) != 1 || reps[0].Result != "ok" || len(sp.releases) != 0 || len(wall.ran()) != 1 {
		t.Fatalf("the pass runs it: releases %v reports %+v ran %v", sp.releases, reps, wall.ran())
	}
}

// 73: a job directory a restart cannot resume (no receipt) is released with
// no blame, never reported.
func TestLiveUnresumableJobIsReleasedWithNoBlame(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{next: workingNext_internal("read", "p1.r1")}
	r := cr2Runner_internal(t, sp, &liveWall{}, &cr2Procs{}, time.Unix(1000, 0))
	dir := filepath.Join(r.RootDir, "jobs", "p1.r1")
	if err := WriteJobRecord(dir, JobRecord{Consumer: "p1.r1", Producer: "p1", Kind: "read", Row: "vision.ds", Gen: 2, Attempt: 1, State: JobStaged}); err != nil {
		t.Fatal(err)
	}
	_ = r.RecoverRow(context.Background(), "vision.ds")
	if len(sp.reported()) != 0 || len(sp.releases) != 1 || !strings.HasPrefix(sp.releases[0], "p1.r1@2 runner restarted: ") {
		t.Fatalf("want one no-blame release and no report, got releases %v reports %+v", sp.releases, sp.reported())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the released job's directory must go: %v", err)
	}
}

// 73: a job the worker's own stop cancels is "runner stopped": no report,
// no timeout, its record kept to resume.
func TestLiveAStoppedJobIsKeptNotReported(t *testing.T) {
	t.Parallel()
	running := make(chan struct{})
	wall := &liveWall{fn: func(ctx context.Context, _ Job) (WallRunResult, error) {
		close(running)
		<-ctx.Done()
		return WallRunResult{ExitCode: -1}, fmt.Errorf("nova-sandbox: %w", ctx.Err())
	}}
	sp := &cr2Sprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Ready: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	r := cr2Runner_internal(t, sp, wall, &cr2Procs{}, time.Unix(1000, 0))
	r.Logger = &StepLogger{StateDir: t.TempDir(), Now: r.Now}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-running; cancel() }()
	_ = r.RunConsumer(ctx, sp.next, sp.next.Ready[0])
	rec, err := ReadJobRecord(filepath.Join(r.RootDir, "jobs", "p1.r1"))
	if len(sp.reported()) != 0 || err != nil || rec.Outcome.Result != "" || rec.PID != 0 {
		t.Fatalf("a stopped job is never reported and keeps its record: reports %+v record %+v %v", sp.reported(), rec, err)
	}
	b, _ := os.ReadFile(filepath.Join(r.Logger.StateDir, "nova-worker", "vision.ds.log"))
	if !strings.Contains(string(b), "runner stopped") || strings.Contains(string(b), "timed out") {
		t.Fatalf("the step log must say runner stopped, never a timeout:\n%s", b)
	}
}

// 69: a record the sprint keeps refusing never wedges a pass: each pass
// sends it once and goes on; at RefusalLimit identical refusals it moves
// to quarantine/ and is raised, and the pass still takes new cards.
func TestLiveRefusedRecordIsQuarantinedAndThePassGoesOn(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{reportErr: fmt.Errorf("%w: consumer p1.r1 is held by another row", ErrRefused),
		next: NextResponse{Row: "vision.ds", State: card.Up, Width: 2, Ready: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	r := cr2Runner_internal(t, sp, &cr2Wall{}, &cr2Procs{}, time.Unix(1000, 0))
	var mu sync.Mutex
	var warns []string
	r.Warn = func(s string) { mu.Lock(); warns = append(warns, s); mu.Unlock() }
	_ = r.RunConsumer(context.Background(), sp.next, sp.next.Ready[0])
	sp.mu.Lock()
	sp.next.Ready, sp.next.Working = nil, []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2, Attempt: 1}}
	sp.mu.Unlock()
	for range RefusalLimit {
		_ = r.RecoverRow(context.Background(), "vision.ds")
	}
	if n := len(sp.reported()); n != RefusalLimit {
		t.Fatalf("sends %d, want %d (then quarantined)", n, RefusalLimit)
	}
	if _, err := os.Stat(filepath.Join(r.RootDir, "quarantine", "p1.r1@2")); err != nil {
		t.Fatalf("the record was not quarantined: %v", err)
	}
	mu.Lock()
	raised := slices.ContainsFunc(warns, func(s string) bool { return strings.Contains(s, "quarantine") })
	mu.Unlock()
	if !raised {
		t.Fatalf("the quarantine was not raised: %q", warns)
	}
	sp.mu.Lock()
	sp.reportErr = nil
	sp.next.Working = append(sp.next.Working, ConsumerSummary{ID: "p2.r1", Kind: "read", Gen: 2, Attempt: 1}) // p1.r1 is still in hand
	sp.mu.Unlock()
	_ = r.StepRow(context.Background(), "vision.ds")
	reps := sp.reported()
	if len(reps) != RefusalLimit+1 || reps[len(reps)-1].Consumer != "p2.r1" || len(sp.releases) != 0 {
		t.Fatalf("after the quarantine the pass takes p2.r1 and leaves p1.r1 alone: reports %+v releases %v", reps, sp.releases)
	}
}

// 72: one fetch per repo per pass, before any job stages from it, for all
// the pass's jobs of that repo.
func TestLiveOneFetchPerRepoPerPass(t *testing.T) {
	t.Parallel()
	sp := &liveSprint{cr2Sprint: cr2Sprint{kind: "work", next: NextResponse{Row: "vision.ds", State: card.Up, Width: 3, Working: []ConsumerSummary{
		{ID: "p1.w1", Kind: "work", Gen: 2, Attempt: 1}, {ID: "p2.w1", Kind: "work", Gen: 2, Attempt: 1}, {ID: "p3.w1", Kind: "work", Gen: 2, Attempt: 1}}}},
		repos: map[string]string{"p1.w1": "o/x", "p2.w1": "o/x", "p3.w1": "o/y"}}
	r, g := liveWorkRunner_internal(t, sp, &liveWall{})
	_ = r.StepRow(context.Background(), "vision.ds")
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fetches["o/x"] != 1 || g.fetches["o/y"] != 1 || len(g.stagedJobs) != 3 {
		t.Fatalf("fetches %v staged %d: want one fetch per repo and three stages", g.fetches, len(g.stagedJobs))
	}
	if g.fetchedJobs["o/x"] != 2 || g.fetchedJobs["o/y"] != 1 { // a repo's one fetch is for every job of it (shallow: it fetches only theirs)
		t.Fatalf("fetched for jobs %v: want o/x's fetch for both its jobs", g.fetchedJobs)
	}
}

// 61: a failed probe holds the row (unhooked, raised) and is retried after
// a backoff; it never exits the worker, and a passing probe hooks it in.
func TestLiveFailedProbeHoldsAndRetries(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{Row: "b1", TierModels: map[string][]string{"flash": {}}}}}
	wall := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		return WallRunResult{Elapsed: time.Second}, os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	}}
	r := probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	r.BusHook = func(context.Context, string) (WakeHook, error) { return newFakeWakeHook(), nil }
	waits := make(chan time.Duration, 4)
	tick := make(chan time.Time)
	r.After = func(d time.Duration) <-chan time.Time {
		if d != WakeEvery {
			waits <- d
		}
		return tick
	}
	held := make(chan string, 4)
	r.Warn = func(s string) {
		if strings.Contains(s, "held") {
			held <- s
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.ServeRow(ctx, "b1", "opencode") }()
	if d := <-waits; d != ProbeBackoff {
		t.Fatalf("first retry after %s, want %s", d, ProbeBackoff)
	}
	if s := <-held; !strings.Contains(s, "no model for tier flash") {
		t.Fatalf("held without the probe's reason: %s", s)
	}
	tick <- time.Time{} // the probe runs again and fails again: the wait doubles
	if d := <-waits; d != 2*ProbeBackoff {
		t.Fatalf("second retry after %s, want %s", d, 2*ProbeBackoff)
	}
	sp.mu.Lock()
	sp.next.TierModels["flash"] = []string{"deepseek/deepseek-flash"}
	sp.mu.Unlock()
	tick <- time.Time{}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("a served worker never exits on a probe: %v", err)
	}
	if sp.hooks > 1 {
		t.Fatalf("hooked %d times", sp.hooks)
	}
}

// 74: at hook the worker beats once before its first pass, advertising the
// tiers its probe proved (a tier whose models the harness cannot reach is
// left out) with its nova-reserve slots.
func TestLiveWorkerAdvertisesProvedTiersAtHook(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{Row: "b1", TierModels: map[string][]string{
		"flash": {"deepseek/deepseek-flash"}, "heavy": {"anthropic/opus"}}}}}
	wall := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		return WallRunResult{Elapsed: time.Second}, os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	}}
	r := probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	r.BusHook = func(context.Context, string) (WakeHook, error) { return newFakeWakeHook(), nil }
	r.Warn = func(string) {}
	rec := &beatRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	r.Meter, r.Beater = meterFake{hb: Heartbeat{Load1: 1, Cores: 16, SlotsTotal: 3}}, cancelBeater{rec, cancel}
	if err := r.ServeRow(ctx, "b1", "opencode"); err != nil {
		t.Fatal(err)
	}
	if len(rec.beats) != 1 || rec.beats[0].Tiers != "flash" || rec.beats[0].SlotsTotal != 3 || sp.hooks != 1 {
		t.Fatalf("want one beat advertising flash and 3 slots after the hook, got %+v (hooks %d)", rec.beats, sp.hooks)
	}
}

// 66 under deal is take (defect 52): the deal holds the width, so a pass
// starts a job only for a card dealt into its hand with none yet: one whose
// job is in flight is not started twice, and a card the sprint lists ready
// (not dealt to this row) is never taken.
func TestLivePassRunsOnlyTheHand(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{next: workingNext_internal("read", "p1.r1", "p2.r1")}
	sp.next.Ready = []ConsumerSummary{{ID: "p3.r1", Kind: "read", Gen: 2}}
	r := cr2Runner_internal(t, sp, &liveWall{}, &cr2Procs{}, time.Unix(1000, 0))
	held, ok := r.claim_internal(context.Background(), sp.next.Working[0], nil) // p1.r1's job is in flight
	if !ok {
		t.Fatal("claim")
	}
	defer held.done()
	_, launches, _, err := r.planPass_internal(context.Background(), "vision.ds")
	if err != nil || len(launches) != 1 || launches[0].c.ID != "p2.r1" {
		t.Fatalf("want one launch, p2.r1, got %+v (%v)", launches, err)
	}
	launches[0].done()
}

// cancelBeater records a beat and ends the serve: the advertisement is the
// first beat, so a served row stops right after it.
type cancelBeater struct {
	rec    *beatRecorder
	cancel context.CancelFunc
}

func (b cancelBeater) Beat(ctx context.Context, row string, hb Heartbeat) error {
	defer b.cancel()
	return b.rec.Beat(ctx, row, hb)
}
