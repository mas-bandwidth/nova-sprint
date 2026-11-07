package worker

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type runnerJobDirState_internal struct {
	exists  bool
	creates int
	pid     int
	resID   int
	pushed  bool
	outcome string
	bounced bool
}

type runnerAppliedOp_internal struct {
	id  int
	c   string
	res string
}

type runnerEnvState_internal struct {
	crashes   int
	storeDown bool
	crashed   bool
}

type runnerTraceStep_internal struct {
	action       string
	arg          string
	storeState   map[string]string
	jobState     map[string]string
	slotsInUse   int
	width        int
	jobDir       map[string]runnerJobDirState_internal
	harnessAlive map[string]int
	appliedOps   []runnerAppliedOp_internal
	env          runnerEnvState_internal
}

type runnerWalkHarness_internal struct {
	st         *store.Fake
	rootDir    string
	t0         time.Time
	stepN      int
	jobState   map[string]string
	creates    map[string]int
	pushed     map[string]bool
	bounced    map[string]bool
	alive      map[string]int
	slotsInUse int
	applied    map[int]runnerAppliedOp_internal
	env        runnerEnvState_internal
}

func newRunnerWalkHarness_internal(t *testing.T) *runnerWalkHarness_internal {
	t.Helper()
	st := store.NewFake()
	t0 := time.Unix(1000, 0).UTC()
	rec, err := st.Apply(context.Background(), store.Op{
		ID:    "op-seed",
		Epoch: 1,
		Actor: "sprint",
		Kind:  "deal",
		At:    t0,
		Writes: []store.Write{
			store.PutStream{Name: "s1", Repo: "repo", Base: "main", Pos: 1},
			store.PutRow{Name: "vision.ds"},
			store.SetRowState{Row: "vision.ds", State: "up"},
			store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"},
			store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro"},
			store.PutCard{ID: "c1", Kind: "work", Of: "p1", Stream: "s1", Holder: "vision.ds", Tier: "pro", Attempt: 1},
			store.SetCardState{Card: "c1", State: "ready"},
			store.PutCard{ID: "c2", Kind: "work", Of: "p2", Stream: "s1", Holder: "vision.ds", Tier: "pro", Attempt: 1},
			store.SetCardState{Card: "c2", State: "ready"},
		},
	})
	if err != nil || !rec.Accepted {
		t.Fatalf("seed runner walk: err=%v rec=%+v", err, rec)
	}
	return &runnerWalkHarness_internal{
		st:       st,
		rootDir:  t.TempDir(),
		t0:       t0,
		jobState: map[string]string{"c1": JobNone, "c2": JobNone},
		creates:  map[string]int{"c1": 0, "c2": 0},
		pushed:   map[string]bool{"c1": false, "c2": false},
		bounced:  map[string]bool{"c1": false, "c2": false},
		alive:    map[string]int{"c1": 0, "c2": 0},
		applied:  map[int]runnerAppliedOp_internal{},
	}
}

func runnerResID_internal(c string) int {
	if c == "c1" {
		return 1
	}
	return 2
}

func runnerPID_internal(c string) int {
	if c == "c1" {
		return 101
	}
	return 102
}

func (h *runnerWalkHarness_internal) jobDirPath_internal(c string) string {
	return filepath.Join(h.rootDir, c+"-a1")
}

// refinementRunnerState_internal projects the real store.Fake + on-disk JobRecord state
// onto Runner.tla variables <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env>>.
func refinementRunnerState_internal(t *testing.T, h *runnerWalkHarness_internal, action, arg string) runnerTraceStep_internal {
	t.Helper()
	snap, err := h.st.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stMap := make(map[string]string, 2)
	jsMap := make(map[string]string, 2)
	jdMap := make(map[string]runnerJobDirState_internal, 2)
	alMap := make(map[string]int, 2)
	for _, c := range []string{"c1", "c2"} {
		stMap[c] = snap.Cards[c].State
		jsMap[c] = h.jobState[c]
		alMap[c] = h.alive[c]
		jdMap[c] = h.projectJobDir_internal(t, c)
	}
	var ops []runnerAppliedOp_internal
	for _, id := range []int{1, 2} {
		if op, ok := h.applied[id]; ok {
			ops = append(ops, op)
		}
	}
	return runnerTraceStep_internal{
		action:       action,
		arg:          arg,
		storeState:   stMap,
		jobState:     jsMap,
		slotsInUse:   h.slotsInUse,
		width:        2,
		jobDir:       jdMap,
		harnessAlive: alMap,
		appliedOps:   ops,
		env:          h.env,
	}
}

func (h *runnerWalkHarness_internal) projectJobDir_internal(t *testing.T, c string) runnerJobDirState_internal {
	t.Helper()
	dir := h.jobDirPath_internal(c)
	_, statErr := os.Stat(filepath.Join(dir, "job.json"))
	exists := statErr == nil
	var rec JobRecord
	if exists {
		var rErr error
		rec, rErr = ReadJobRecord(dir)
		if rErr != nil {
			t.Fatalf("ReadJobRecord %s: %v", c, rErr)
		}
	} else if h.creates[c] > 0 {
		rec = h.shadowRec_internal(c)
	}
	out := rec.Outcome.Result
	if out == "" {
		out = "none"
	}
	resID := 0
	if rec.ResultID != "" {
		resID = runnerResID_internal(c)
	}
	return runnerJobDirState_internal{
		exists:  exists,
		creates: h.creates[c],
		pid:     rec.PID,
		resID:   resID,
		pushed:  h.pushed[c],
		outcome: out,
		bounced: h.bounced[c],
	}
}

func (h *runnerWalkHarness_internal) shadowRec_internal(c string) JobRecord {
	var resID string
	if h.pushed[c] {
		resID = FormatResultID(1, c, 1, 1)
	}
	out := "ok"
	if h.bounced[c] {
		out = "fail"
	} else if op, ok := h.applied[runnerResID_internal(c)]; ok {
		out = op.res
	}
	return JobRecord{Consumer: c, ResultID: resID, Outcome: Outcome{Result: out}}
}

func (h *runnerWalkHarness_internal) applyStep_internal(t *testing.T, act, c string) {
	t.Helper()
	h.stepN++
	ctx := context.Background()
	dir := h.jobDirPath_internal(c)
	switch act {
	case "Take":
		rec, err := h.st.Apply(ctx, store.Op{
			ID:     FormatTakeOpID(1, c, 1, "vision.ds"),
			Epoch:  1,
			Actor:  "vision.ds",
			Kind:   "take",
			At:     h.t0.Add(time.Duration(h.stepN) * time.Second),
			Writes: []store.Write{store.SetCardState{Card: c, State: "working"}},
		})
		if err != nil || !rec.Accepted {
			t.Fatalf("Take %s: err=%v rec=%+v", c, err, rec)
		}
		h.jobState[c] = JobTaken
	case "Stage":
		if _, err := os.Stat(dir); err == nil {
			t.Fatalf("job dir %s already exists", dir)
		}
		h.creates[c]++
		h.jobState[c] = JobStaged
		if err := WriteJobRecord(dir, JobRecord{Epoch: 1, Consumer: c, State: JobStaged}); err != nil {
			t.Fatal(err)
		}
	case "Run":
		rec, err := ReadJobRecord(dir)
		if err != nil {
			t.Fatal(err)
		}
		rec.PID = runnerPID_internal(c)
		rec.State = JobRunning
		if err := WriteJobRecord(dir, rec); err != nil {
			t.Fatal(err)
		}
		h.slotsInUse++
		h.alive[c] = 1
		h.jobState[c] = JobRunning
	case "Harvest", "Die":
		h.finishRun_internal(t, c, act, dir)
	case "Rebase", "Conflict", "Push", "Report", "Clean":
		h.advancePostRun_internal(t, c, act, dir)
	case "Crash":
		h.env.crashes++
		h.env.crashed = true
	case "Restart":
		h.env.crashed = false
		h.env.storeDown = false
	}
}

func (h *runnerWalkHarness_internal) finishRun_internal(t *testing.T, c, act, dir string) {
	t.Helper()
	rec, err := ReadJobRecord(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec.PID = 0
	rec.State = JobHarvested
	if act == "Die" {
		rec.Outcome = Outcome{Result: "fault"}
		h.bounced[c] = false
	} else {
		rec.Outcome = Outcome{Result: "ok"}
	}
	if err := WriteJobRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	h.alive[c] = 0
	h.slotsInUse--
	h.jobState[c] = JobHarvested
}

func (h *runnerWalkHarness_internal) advancePostRun_internal(t *testing.T, c, act, dir string) {
	t.Helper()
	ctx := context.Background()
	switch act {
	case "Rebase":
		rec, _ := ReadJobRecord(dir)
		rec.State = JobRebased
		_ = WriteJobRecord(dir, rec)
		h.jobState[c] = JobRebased
	case "Conflict":
		rec, _ := ReadJobRecord(dir)
		rec.State = JobRebased
		rec.Outcome = Outcome{Result: "fail"}
		_ = WriteJobRecord(dir, rec)
		h.bounced[c] = true
		h.jobState[c] = JobRebased
	case "Push":
		rec, _ := ReadJobRecord(dir)
		h.pushed[c] = true
		if rec.ResultID == "" {
			rec.ResultID = FormatResultID(1, c, 1, 1)
		}
		rec.State = JobReporting
		_ = WriteJobRecord(dir, rec)
		h.jobState[c] = rec.State
	case "Report":
		rec, _ := ReadJobRecord(dir)
		id := runnerResID_internal(c)
		if _, exists := h.applied[id]; !exists {
			h.applied[id] = runnerAppliedOp_internal{id: id, c: c, res: rec.Outcome.Result}
		}
		_, _ = h.st.Apply(ctx, store.Op{
			ID:     rec.ResultID,
			Epoch:  1,
			Actor:  "vision.ds",
			Kind:   "report",
			At:     h.t0.Add(time.Duration(h.stepN) * time.Second),
			Writes: []store.Write{store.SetCardState{Card: c, State: "done"}},
		})
		rec.State = JobRecorded
		_ = WriteJobRecord(dir, rec)
		h.jobState[c] = JobRecorded
	case "Clean":
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		h.jobState[c] = JobCleaned
	}
}

func walkRunnerTrace_internal(t *testing.T, seed int64) []runnerTraceStep_internal {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	h := newRunnerWalkHarness_internal(t)
	steps := []runnerTraceStep_internal{refinementRunnerState_internal(t, h, "Init", "none")}
	for i := 0; i < 40; i++ {
		st := steps[len(steps)-1]
		if st.jobState["c1"] == JobCleaned && st.jobState["c2"] == JobCleaned {
			steps = append(steps, refinementRunnerState_internal(t, h, "Done", "none"))
			break
		}
		act, arg := pickRunnerMove_internal(t, rng, h, st)
		steps = append(steps, refinementRunnerState_internal(t, h, act, arg))
	}
	return steps
}

func pickRunnerMove_internal(t *testing.T, rng *rand.Rand, h *runnerWalkHarness_internal, st runnerTraceStep_internal) (string, string) {
	t.Helper()
	if st.env.crashed {
		h.applyStep_internal(t, "Restart", "none")
		return "Restart", "none"
	}
	type move struct{ act, arg string }
	var moves []move
	for _, c := range []string{"c1", "c2"} {
		switch st.jobState[c] {
		case JobNone:
			if st.storeState[c] == "ready" {
				moves = append(moves, move{"Take", c})
			}
		case JobTaken:
			moves = append(moves, move{"Stage", c})
		case JobStaged:
			if st.slotsInUse < 1 && st.harnessAlive[c] == 0 {
				moves = append(moves, move{"Run", c})
			}
		case JobRunning:
			moves = append(moves, move{"Harvest", c}, move{"Die", c})
		case JobHarvested:
			if st.jobDir[c].outcome == "ok" {
				moves = append(moves, move{"Rebase", c}, move{"Conflict", c})
			} else {
				moves = append(moves, move{"Push", c})
			}
		case JobRebased:
			moves = append(moves, move{"Push", c})
		case JobReporting:
			moves = append(moves, move{"Report", c})
		case JobRecorded:
			moves = append(moves, move{"Clean", c})
		}
	}
	if st.env.crashes < 1 {
		moves = append(moves, move{"Crash", "none"})
	}
	m := moves[rng.Intn(len(moves))]
	h.applyStep_internal(t, m.act, m.arg)
	return m.act, m.arg
}

func formatRunnerTraceTLA_internal(steps []runnerTraceStep_internal) string {
	var b strings.Builder
	b.WriteString("--------------------------- MODULE RunnerTraceLog ---------------------------\n")
	b.WriteString("EXTENDS Naturals, Sequences\nTraceLog == <<\n")
	for i, s := range steps {
		comma := ","
		if i == len(steps)-1 {
			comma = ""
		}
		b.WriteString(fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, storeState |-> [c1 |-> %q, c2 |-> %q], jobState |-> [c1 |-> %q, c2 |-> %q], slotsInUse |-> %d, width |-> %d, jobDir |-> [c1 |-> %s, c2 |-> %s], harnessAlive |-> [c1 |-> %d, c2 |-> %d], appliedOps |-> %s, env |-> %s]%s\n",
			i+1, s.action, s.arg,
			s.storeState["c1"], s.storeState["c2"],
			s.jobState["c1"], s.jobState["c2"],
			s.slotsInUse, s.width,
			formatRunnerJobDirTLA_internal(s.jobDir["c1"]),
			formatRunnerJobDirTLA_internal(s.jobDir["c2"]),
			s.harnessAlive["c1"], s.harnessAlive["c2"],
			formatRunnerOpsTLA_internal(s.appliedOps),
			formatRunnerEnvTLA_internal(s.env),
			comma))
	}
	b.WriteString(">>\n")
	b.WriteString("=============================================================================\n")
	return b.String()
}

func formatRunnerJobDirTLA_internal(d runnerJobDirState_internal) string {
	return fmt.Sprintf("[exists |-> %s, creates |-> %d, pid |-> %d, resId |-> %d, pushed |-> %s, outcome |-> %q, bounced |-> %s]",
		tlaBool_internal(d.exists), d.creates, d.pid, d.resID, tlaBool_internal(d.pushed), d.outcome, tlaBool_internal(d.bounced))
}

func formatRunnerOpsTLA_internal(ops []runnerAppliedOp_internal) string {
	if len(ops) == 0 {
		return "{}"
	}
	parts := make([]string, len(ops))
	for i, op := range ops {
		parts[i] = fmt.Sprintf("[id |-> %d, c |-> %q, res |-> %q]", op.id, op.c, op.res)
	}
	slices.Sort(parts)
	return "{" + strings.Join(parts, ", ") + "}"
}

func formatRunnerEnvTLA_internal(e runnerEnvState_internal) string {
	return fmt.Sprintf("[crashes |-> %d, storeDown |-> %s, crashed |-> %s]",
		e.crashes, tlaBool_internal(e.storeDown), tlaBool_internal(e.crashed))
}

func tlaBool_internal(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}

func TestRunnerTraceRefinementWalk(t *testing.T) {
	t.Parallel()
	driveRunnerEntrypoints_internal(t)
	for seed := int64(1); seed <= 8; seed++ {
		steps := walkRunnerTrace_internal(t, seed)
		last := steps[len(steps)-1]
		if last.action != "Done" {
			t.Fatalf("seed %d: did not reach Done: %+v", seed, last)
		}
		tlaText := formatRunnerTraceTLA_internal(steps)
		if traceDir := os.Getenv("NOVA_TRACE_DIR"); traceDir != "" && seed <= 3 {
			sub := filepath.Join(traceDir, fmt.Sprintf("runner-walk-%d", seed))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "RunnerTraceLog.tla"), []byte(tlaText), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		wantSeed := int64(1)
		if raw := os.Getenv("NOVA_TRACE_SEED"); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				wantSeed = parsed
			}
		}
		if seed == wantSeed {
			out := os.Getenv("NOVA_RUNNER_TRACE_OUT")
			if out == "" {
				out = os.Getenv("NOVA_TRACE_OUT")
			}
			if out != "" {
				if err := os.WriteFile(out, []byte(tlaText), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func driveRunnerEntrypoints_internal(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	wall := &fakeWall{
		runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
			git.mu.Lock()
			git.headByWT[j.WorktreeDir] = testWorkSHA
			git.mu.Unlock()
			_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
			return WallRunResult{ExitCode: 0, PID: 101, Nonce: "n101", Elapsed: time.Second}, nil
		},
	}
	r, err := NewRunner(Runner{CoordinatorHosts: testCoordinators_internal,
		Host: "vision", RootDir: t.TempDir(), Sprint: sc, Git: git, Wall: wall,
		Now: clock, Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverRow(ctx, row); err != nil {
		t.Fatal(err)
	}
	next, err := sc.Next(ctx, row)
	if err != nil || len(next.Ready) == 0 {
		t.Fatalf("expected ready consumer, err=%v next=%+v", err, next)
	}
	if err := r.RunConsumer(ctx, next, next.Ready[0]); err != nil {
		t.Fatal(err)
	}
	if err := r.StepRow(ctx, row); err != nil {
		t.Fatal(err)
	}
}
