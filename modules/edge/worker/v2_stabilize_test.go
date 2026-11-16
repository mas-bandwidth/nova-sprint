package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

func TestHarnessSignalKillsClassifyAsFault(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"claude", "codex", "opencode"} {
		h, err := NewHarness(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, exit := range []int{-1, 129, 130, 134, 137, 143} {
			out := ClassifyJob(h, Job{Consumer: "p1.w1", Kind: "work", Model: "m"}, exit, 5*time.Second, 15*time.Second, []byte("terminated"))
			if out.Result != "fault" || !strings.Contains(out.Reason, fmt.Sprintf("harness killed with exit %d", exit)) {
				t.Errorf("%s exit %d: got %+v, want fault naming harness killed", name, exit, out)
			}
		}
	}

	// A harness killed by SIGTERM (143) after writing a partial commit pushes
	// that head for the next attempt and still reports fault (never ok or fail).
	root := t.TempDir()
	sha := strings.Repeat("c", 40)
	fx := edge.NewFakeExec()
	sealedFake_internal(fx, sha)
	r, err := NewRunner(Runner{Host: "b1", RootDir: root, Git: &OSGitOps{WorkRoot: filepath.Join(root, "work"), Exec: fx}, Wall: &fakeWall{}, Now: testClock_internal(), Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	rec := JobRecord{Epoch: 1, Consumer: "p1.w1", Kind: "work", Repo: "o/r", Forge: "github", Branch: "sprint/s/p1", BaseSHA: strings.Repeat("b", 40), Gen: 1, StreamRef: "sprint/s/group", DoneWhen: []string{"go test ./..."}}
	writeStagedRecord_internal(t, jobDir, rec)
	if err := os.WriteFile(filepath.Join(jobDir, SealBundle), []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := Job{Consumer: rec.Consumer, Kind: "work", JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), BaseSHA: rec.BaseSHA, CommitName: testCommitName, CommitEmail: testCommitEmail}
	out := ClassifyJob(&ClaudeHarness{}, job, 143, 0, 0, []byte("SIGTERM"))
	if out = r.harvestWorkHead_internal(context.Background(), job, &rec, out); out.Result != "fault" || out.Head != sha {
		t.Fatalf("SIGTERM after partial commit: got %+v, want fault with pushed head %s", out, sha)
	}
}

func TestGateSignalAndReserveInfraFaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		res    edge.Result
		want   string
		noSlot bool
	}{
		{
			name: "reserve capacity mismatch exit 2",
			res:  edge.Result{ExitCode: 2, Stderr: "nova-reserve: capacity is 8, not 1 (in /var/tmp/nova-reserve/capacity)\n"},
			want: "fault",
		},
		{
			name: "reserve lock dir error exit 1 before HELD",
			res:  edge.Result{ExitCode: 1, Stderr: "nova-reserve: open /var/tmp/nova-reserve: permission denied\n"},
			want: "fault",
		},
		{
			name: "gate killed by SIGTERM 143",
			res:  edge.Result{ExitCode: 143, Stderr: "nova-reserve: HELD mode=shared slot=1/4 pid=9\nterminated\n"},
			want: "fault",
		},
		{
			name: "gate killed by SIGHUP 129",
			res:  edge.Result{ExitCode: 129, Stderr: "nova-reserve: HELD mode=shared slot=1/4 pid=9\nhangup\n"},
			want: "fault",
		},
		{
			name: "wrapper killed directly by signal exit -1",
			res:  edge.Result{ExitCode: -1, Stderr: "nova-reserve: HELD mode=shared slot=1/4 pid=9\n"},
			want: "fault",
		},
		{
			name: "in-wall test OOM kill 137 after HELD stays fail",
			res:  edge.Result{ExitCode: 137, Stderr: "nova-reserve: HELD mode=shared slot=1/4 pid=9\nKilled\n"},
			want: "fail",
		},
	}
	for _, tc := range cases {
		w := &OSReserveWall{Exec: &wallExec{res: tc.res}, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox"}}
		_, err := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: t.TempDir()}, "go test ./...", time.Minute)
		if errors.Is(err, ErrNoSlot) != tc.noSlot {
			t.Errorf("%s: ErrNoSlot = %v, want %v (err=%v)", tc.name, errors.Is(err, ErrNoSlot), tc.noSlot, err)
		}
		rec := gateStep_internal(t, w, &gateClock_internal{sec: 1600}, JobRecord{DeadlineAt: 6400, DoneWhen: []string{"go test ./..."}})
		if rec.Outcome.Result != tc.want {
			t.Errorf("%s: gate outcome = %+v, want %s", tc.name, rec.Outcome, tc.want)
		}
	}
}

func TestGateParallelismFollowsNovaReserveCPUs(t *testing.T) {
	t.Parallel()
	cc := func(name string) (string, error) { return "/fixture/bin/" + name, nil }
	for _, tc := range []struct {
		name        string
		env         []string
		wantJobs    int
		wantThreads string
	}{
		{
			name:        "NOVA_RESERVE_CPUS=8 scales jobserver and rust test threads",
			env:         []string{"PATH=/usr/bin", "NOVA_RESERVE_CPUS=8"},
			wantJobs:    8,
			wantThreads: "RUST_TEST_THREADS=8",
		},
		{
			name:        "NOVA_RESERVE_CPUS=1 sets zero extra FIFO tokens",
			env:         []string{"PATH=/usr/bin", "NOVA_RESERVE_CPUS=1"},
			wantJobs:    1,
			wantThreads: "RUST_TEST_THREADS=1",
		},
		{
			name:        "explicit RUST_TEST_THREADS is preserved under NOVA_RESERVE_CPUS",
			env:         []string{"PATH=/usr/bin", "NOVA_RESERVE_CPUS=8", "RUST_TEST_THREADS=2"},
			wantJobs:    8,
			wantThreads: "RUST_TEST_THREADS=2",
		},
		{
			name:     "CARGO_BUILD_JOBS=6 used when NOVA_RESERVE_CPUS is unset",
			env:      []string{"PATH=/usr/bin", "CARGO_BUILD_JOBS=6"},
			wantJobs: 6,
		},
		{
			name:     "invalid NOVA_RESERVE_CPUS is stripped and falls back cleanly",
			env:      []string{"PATH=/usr/bin", "NOVA_RESERVE_CPUS=not-a-number"},
			wantJobs: gateJobs_internal([]string{"PATH=/usr/bin"}),
		},
	} {
		ex := &rawFifoExec_internal{}
		w := &OSReserveWall{
			UseSandbox: true,
			Exec:       ex,
			LookPath:   cc,
			Env:        func() []string { return slices.Clone(tc.env) },
			Config:     WallConfig{ReserveBin: "/fixture/nova-reserve", SandboxBin: "/fixture/nova-sandbox", NetDeny: true},
		}
		dir := t.TempDir()
		res, err := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, "cargo test", time.Minute)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		wantMake := fmt.Sprintf("CARGO_MAKEFLAGS=-j%d --jobserver-auth=fifo:%s", tc.wantJobs, ex.fifo)
		if !slices.Contains(ex.env, wantMake) || !ex.isPipe || ex.tokens != tc.wantJobs-1 {
			t.Errorf("%s: env=%q pipe=%v tokens=%d; want %q and %d tokens", tc.name, ex.env, ex.isPipe, ex.tokens, wantMake, tc.wantJobs-1)
		}
		argv := strings.Join(res.Argv, " ")
		if tc.wantThreads != "" {
			if !slices.Contains(ex.env, tc.wantThreads) || !strings.Contains(argv, "--env RUST_TEST_THREADS") {
				t.Errorf("%s: env=%q argv=%q; want %s and --env RUST_TEST_THREADS", tc.name, ex.env, argv, tc.wantThreads)
			}
		} else if slices.ContainsFunc(ex.env, func(kv string) bool { return strings.HasPrefix(kv, "RUST_TEST_THREADS=") }) {
			t.Errorf("%s: unexpected RUST_TEST_THREADS in %q", tc.name, ex.env)
		}
	}
}

// rawFifoExec_internal reads the FIFO via raw syscall.Open/syscall.Read with
// O_NONBLOCK so 0-token FIFOs (-j1) return EAGAIN immediately rather than
// parking in os.File's epoll poller while the worker holds the write end open.
type rawFifoExec_internal struct {
	env    []string
	fifo   string
	isPipe bool
	tokens int
}

func (e *rawFifoExec_internal) Run(_ context.Context, c edge.Cmd) (edge.Result, error) {
	e.env = slices.Clone(c.Env)
	for _, kv := range c.Env {
		if v, ok := strings.CutPrefix(kv, "CARGO_MAKEFLAGS="); ok {
			_, e.fifo, _ = strings.Cut(v, "--jobserver-auth=fifo:")
		}
	}
	if st, err := os.Lstat(e.fifo); err == nil && st.Mode()&os.ModeNamedPipe != 0 {
		e.isPipe = true
		if fd, err := syscall.Open(e.fifo, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0); err == nil {
			buf := make([]byte, 64)
			if n, rerr := syscall.Read(fd, buf); rerr == nil && n > 0 {
				e.tokens = n
			}
			_ = syscall.Close(fd)
		}
	}
	return edge.Result{}, nil
}
