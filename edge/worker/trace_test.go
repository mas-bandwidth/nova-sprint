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

	"github.com/mas-bandwidth/nova-sprint/steps"
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
	stopped bool
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
	released  bool
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
	pass         string
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
	pass       string                // the wake loop's pass: idle or planning
	stopped    map[string]bool       // kept by the worker's stop, to run again
	released   map[string]*JobRecord // the record a release gave back, its directory gone
	takes      map[string]int
	runner     *Runner // its stopped_internal writes the stop's record
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
			store.SetCardState{Card: "c1", State: "working"}, // the deal is the take (defect 52)
			store.PutCard{ID: "c2", Kind: "work", Of: "p2", Stream: "s1", Holder: "vision.ds", Tier: "pro", Attempt: 1},
			store.SetCardState{Card: "c2", State: "working"},
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
		pass:     "idle",
		stopped:  map[string]bool{"c1": false, "c2": false},
		released: map[string]*JobRecord{},
		takes:    map[string]int{"c1": 0, "c2": 0},
		runner:   runnerForWalk_internal(t),
	}
}

func runnerForWalk_internal(t *testing.T) *Runner {
	t.Helper()
	r, err := NewRunner(Runner{Host: "vision", RootDir: t.TempDir(), Now: func() time.Time { return time.Unix(1000, 0) }, Sleep: func(time.Duration) {}})
	if err != nil {
		t.Fatal(err)
	}
	return r
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
		pass:         h.pass,
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
		stopped: h.stopped[c],
	}
}

func (h *runnerWalkHarness_internal) shadowRec_internal(c string) JobRecord {
	if rec := h.released[c]; rec != nil {
		return JobRecord{Consumer: c, Outcome: rec.Outcome}
	}
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
		h.takes[c]++
		rec, err := h.st.Apply(ctx, store.Op{
			ID:     FormatTakeOpID(1, c, h.takes[c], "vision.ds"),
			Epoch:  1,
			Actor:  "vision.ds",
			Kind:   "take",
			At:     h.t0.Add(time.Duration(h.stepN) * time.Second),
			Writes: []store.Write{store.SetCursor{Name: steps.AckCursor(1, c), Seq: 1}}, // the acknowledgement
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
		h.stopped[c] = false
		h.jobState[c] = JobRunning
	case "Harvest", "Die":
		h.finishRun_internal(t, c, act, dir)
	case "Rebase", "Conflict", "Push", "Report", "Clean":
		h.advancePostRun_internal(t, c, act, dir)
	default:
		h.processStep_internal(t, act, c)
	}
}

// processStep_internal is a step of the worker process, not of one job: a
// crash, the stop, a pass, a restart, a release.
func (h *runnerWalkHarness_internal) processStep_internal(t *testing.T, act, c string) {
	t.Helper()
	switch act {
	case "Crash":
		h.env.crashes++
		h.env.crashed = true
		h.pass = "idle"
	case "Stop":
		h.stop_internal(t)
	case "PassStart":
		h.pass = "planning"
	case "PassEnd":
		h.pass = "idle"
	case "Release":
		h.release_internal(t, c)
	case "Restart":
		h.env.crashed = false
		h.env.storeDown = false
	}
}

// stop_internal is the worker's stop: each running job's record is kept by
// the worker's own stopped_internal (no pid, no outcome), its harness gone
// and its slot freed; the walk's job is at staged, as a resume treats a
// running record with no live harness (prepareResume_internal).
func (h *runnerWalkHarness_internal) stop_internal(t *testing.T) {
	t.Helper()
	for _, c := range []string{"c1", "c2"} {
		if h.jobState[c] != JobRunning {
			continue
		}
		dir := h.jobDirPath_internal(c)
		rec, err := ReadJobRecord(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.runner.stopped_internal(&prepared_internal{job: Job{JobDir: dir, Row: "vision.ds", Consumer: c}, rec: rec}); err != nil {
			t.Fatal(err)
		}
		h.alive[c], h.stopped[c], h.jobState[c] = 0, true, JobStaged
	}
	h.slotsInUse = 0
	h.env.crashes++
	h.env.crashed = true
	h.pass = "idle"
}

// release_internal is the restart's no-blame give-back of a job it cannot
// resume: its commits pushed, the consumer withdrawn with no result and no
// report, and its directory gone (releaseNoBlame_internal).
func (h *runnerWalkHarness_internal) release_internal(t *testing.T, c string) {
	t.Helper()
	dir := h.jobDirPath_internal(c)
	kept, err := ReadJobRecord(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.st.Apply(context.Background(), store.Op{
		ID: fmt.Sprintf("release-%s-%d", c, h.takes[c]), Epoch: 1, Actor: "vision.ds", Kind: "release",
		At: h.t0.Add(time.Duration(h.stepN) * time.Second), Writes: []store.Write{store.SetCardState{Card: c, State: "fail"}},
	})
	if err != nil || !rec.Accepted {
		t.Fatalf("Release %s: err=%v rec=%+v", c, err, rec)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	h.pushed[c], h.released[c], h.jobState[c] = true, &kept, JobCleaned
	h.env.released = true
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
	for i := 0; i < 160; i++ {
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

type runnerMove_internal struct{ act, arg string }

func pickRunnerMove_internal(t *testing.T, rng *rand.Rand, h *runnerWalkHarness_internal, st runnerTraceStep_internal) (string, string) {
	t.Helper()
	if st.env.crashed {
		h.applyStep_internal(t, "Restart", "none")
		return "Restart", "none"
	}
	moves := []runnerMove_internal{{"PassEnd", "none"}}
	if st.pass == "idle" {
		moves = []runnerMove_internal{{"PassStart", "none"}}
	}
	for _, c := range []string{"c1", "c2"} {
		moves = append(moves, consumerMoves_internal(st, c)...)
	}
	if st.env.crashes < 1 {
		moves = append(moves, runnerMove_internal{"Crash", "none"}, runnerMove_internal{"Stop", "none"})
	}
	m := moves[rng.Intn(len(moves))]
	h.applyStep_internal(t, m.act, m.arg)
	return m.act, m.arg
}

// consumerMoves_internal are c's enabled job moves in st: a take (the
// acknowledgement of a card dealt working) only in a pass; a release only
// after a restart, of a job left staged or harvested.
func consumerMoves_internal(st runnerTraceStep_internal, c string) []runnerMove_internal {
	var moves []runnerMove_internal
	if (st.jobState[c] == JobStaged || st.jobState[c] == JobHarvested) && st.env.crashes > 0 && !st.env.released &&
		st.storeState[c] == "working" && st.jobDir[c].exists && st.harnessAlive[c] == 0 {
		moves = append(moves, runnerMove_internal{"Release", c})
	}
	switch st.jobState[c] {
	case JobNone:
		if st.storeState[c] == "working" && st.pass == "planning" {
			moves = append(moves, runnerMove_internal{"Take", c})
		}
	case JobTaken:
		moves = append(moves, runnerMove_internal{"Stage", c})
	case JobStaged:
		if st.slotsInUse < 1 && st.harnessAlive[c] == 0 {
			moves = append(moves, runnerMove_internal{"Run", c})
		}
	case JobRunning:
		moves = append(moves, runnerMove_internal{"Harvest", c}, runnerMove_internal{"Die", c})
	case JobHarvested:
		if st.jobDir[c].outcome == "ok" {
			moves = append(moves, runnerMove_internal{"Rebase", c}, runnerMove_internal{"Conflict", c})
		} else {
			moves = append(moves, runnerMove_internal{"Push", c})
		}
	case JobRebased:
		moves = append(moves, runnerMove_internal{"Push", c})
	case JobReporting:
		moves = append(moves, runnerMove_internal{"Report", c})
	case JobRecorded:
		moves = append(moves, runnerMove_internal{"Clean", c})
	}
	return moves
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
		b.WriteString(fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, storeState |-> [c1 |-> %q, c2 |-> %q], jobState |-> [c1 |-> %q, c2 |-> %q], slotsInUse |-> %d, width |-> %d, jobDir |-> [c1 |-> %s, c2 |-> %s], harnessAlive |-> [c1 |-> %d, c2 |-> %d], appliedOps |-> %s, env |-> %s, pass |-> %q]%s\n",
			i+1, s.action, s.arg,
			s.storeState["c1"], s.storeState["c2"],
			s.jobState["c1"], s.jobState["c2"],
			s.slotsInUse, s.width,
			formatRunnerJobDirTLA_internal(s.jobDir["c1"]),
			formatRunnerJobDirTLA_internal(s.jobDir["c2"]),
			s.harnessAlive["c1"], s.harnessAlive["c2"],
			formatRunnerOpsTLA_internal(s.appliedOps),
			formatRunnerEnvTLA_internal(s.env),
			s.pass,
			comma))
	}
	b.WriteString(">>\n")
	b.WriteString("=============================================================================\n")
	return b.String()
}

func formatRunnerJobDirTLA_internal(d runnerJobDirState_internal) string {
	return fmt.Sprintf("[exists |-> %s, creates |-> %d, pid |-> %d, resId |-> %d, pushed |-> %s, outcome |-> %q, bounced |-> %s, stopped |-> %s]",
		tlaBool_internal(d.exists), d.creates, d.pid, d.resID, tlaBool_internal(d.pushed), d.outcome, tlaBool_internal(d.bounced), tlaBool_internal(d.stopped))
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
	return fmt.Sprintf("[crashes |-> %d, storeDown |-> %s, crashed |-> %s, released |-> %s]",
		e.crashes, tlaBool_internal(e.storeDown), tlaBool_internal(e.crashed), tlaBool_internal(e.released))
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
	r, err := NewRunner(Runner{
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
	if err != nil || len(next.Working) == 0 {
		t.Fatalf("expected a consumer in hand, err=%v next=%+v", err, next)
	}
	if err := r.RunConsumer(ctx, next, next.Working[0]); err != nil {
		t.Fatal(err)
	}
	if err := r.StepRow(ctx, row); err != nil {
		t.Fatal(err)
	}
}
