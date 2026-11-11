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

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
	sealed  bool
	// keepsPrior: the job's head still descends from the head it
	// started from; a Rebase step sets it from the git the stream step runs.
	keepsPrior bool
	// The deal's prior head (none, unfinished, finished) and whether it is
	// a fix; atPrior: the harvest found no new head; nothingNew: failed as
	// "committed nothing over the prior head" (Runner.tla, 2026-11-11).
	prior      string
	fix        bool
	atPrior    bool
	nothingNew bool
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
	restarted bool
}

// runnerBranch_internal is Runner.tla's branch[c]: who wrote the card
// branch's tip on origin (none, job, holder), whether the next holder did,
// and the job's pushes after its consumer was final (defect 78); the head
// the store learned (known) and the one the next holder's take started it
// from (from: unset before that take), each by its writer.
type runnerBranch_internal struct {
	tip         string
	holderWrote bool
	finalPushes int
	known, from string
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
	branch       map[string]runnerBranch_internal
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
	gone       map[string]*JobRecord // the record a release or a retire left, its directory gone
	takes      map[string]int
	runner     *Runner  // its stopped_internal, pushBranch_internal and finalJob_internal are the walk's
	git        *fakeGit // origin: the card branches, and every push the runner made
	finalAt    map[string]int
	holder     map[string]bool
	wall       *stepWall_internal // the runner's wall: every seal step it ran
	sealedAt   map[string]int     // the seal steps the wall had run on c's tree at c's last Run
	dropped    map[string]bool    // a Rebase step whose git did not keep c's history
	known      map[string]string  // the head a re-deal's store prior carries: a release reports its own
	from       map[string]string  // the next holder's start, once it took c's card: StartHead is the code bound here; take's origin read is TestTakeHandsOverTheBranchTip's
	took       map[string]bool
	prior      map[string]string // the deal: each consumer's prior head by kind, chosen by the walk's seed
	fix        map[string]bool   // the deal carries a finding
	atPrior    map[string]bool   // the harvest found no new head
	nothingNew map[string]bool
	rng        *rand.Rand // the walk's: a harvest at the prior head, red done-whens there
	// atPriorAlways: every harvest of a job with a prior head finds no new
	// head (the scripted walk's).
	atPriorAlways bool
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
	root, git := t.TempDir(), newFakeGit_internal()
	wall := &stepWall_internal{fakeWall: &fakeWall{}, steps: &[][]string{}}
	return &runnerWalkHarness_internal{
		st:       st,
		rootDir:  root,
		t0:       t0,
		jobState: map[string]string{"c1": JobNone, "c2": JobNone},
		creates:  map[string]int{"c1": 0, "c2": 0},
		pushed:   map[string]bool{"c1": false, "c2": false},
		bounced:  map[string]bool{"c1": false, "c2": false},
		alive:    map[string]int{"c1": 0, "c2": 0},
		applied:  map[int]runnerAppliedOp_internal{},
		pass:     "idle",
		stopped:  map[string]bool{"c1": false, "c2": false},
		gone:     map[string]*JobRecord{},
		takes:    map[string]int{"c1": 0, "c2": 0},
		runner:   runnerForWalk_internal(t, root, git, wall),
		wall:     wall,
		git:      git,
		finalAt:  map[string]int{},
		holder:   map[string]bool{"c1": false, "c2": false},
		sealedAt: map[string]int{"c1": 0, "c2": 0},
		dropped:  map[string]bool{},
		known:    map[string]string{},
		from:     map[string]string{},
		took:     map[string]bool{},
		prior:    map[string]string{"c1": "none", "c2": "none"},
		fix:      map[string]bool{}, atPrior: map[string]bool{}, nothingNew: map[string]bool{},
	}
}

func runnerForWalk_internal(t *testing.T, root string, git *fakeGit, wall ReserveWall) *Runner {
	t.Helper()
	r, err := NewRunner(Runner{Host: "vision", RootDir: root, Git: git, Wall: wall, Procs: &fakeProcTable{alive: map[string]bool{}},
		Now: func() time.Time { return time.Unix(1000, 0) }, Sleep: func(time.Duration) {}})
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
	return filepath.Join(h.rootDir, "jobs", c)
}

func runnerBranchName_internal(c string) string { return "sprint/s1/" + c }

// projectBranch_internal reads branch[c] off the fake origin: the tip's
// writer by its sha, and the pushes of c's branch since c went final.
func (h *runnerWalkHarness_internal) projectBranch_internal(c string) runnerBranch_internal {
	b := runnerBranch_internal{holderWrote: h.holder[c], known: runnerWriter_internal(h.known[c]), from: "unset"}
	h.git.mu.Lock()
	b.tip = runnerWriter_internal(h.git.originHeads[runnerBranchName_internal(c)])
	h.git.mu.Unlock()
	if h.took[c] {
		b.from = runnerWriter_internal(h.from[c])
	}
	if at, final := h.finalAt[c]; final {
		for _, p := range h.git.pushes()[at:] {
			if p.branch == runnerBranchName_internal(c) {
				b.finalPushes++
			}
		}
	}
	return b
}

// runnerWriter_internal names a head of the walk's branch by who wrote it:
// none (no branch, or the base: nothing past it), the next holder, or the
// job.
func runnerWriter_internal(sha string) string {
	switch sha {
	case "", testBaseSHA, testPriorSHA: // the prior head is where the job started: nothing past it is the job's
		return "none"
	case testHolderSHA:
		return "holder"
	}
	return "job"
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
	brMap := make(map[string]runnerBranch_internal, 2)
	for _, c := range []string{"c1", "c2"} {
		brMap[c] = h.projectBranch_internal(c)
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
		branch:       brMap,
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
		sealed:  h.seals_internal(c) > h.sealedAt[c],

		keepsPrior: !h.dropped[c],
		prior:      h.prior[c], fix: h.fix[c], atPrior: h.atPrior[c], nothingNew: h.nothingNew[c],
	}
}

func (h *runnerWalkHarness_internal) shadowRec_internal(c string) JobRecord {
	if rec := h.gone[c]; rec != nil {
		return *rec
	}
	var resID string
	if h.pushed[c] {
		resID = resultID_internal(1, c, 1)
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
			ID:     store.ConsumerOpID("take", 1, c, h.takes[c]),
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
		rec := JobRecord{Epoch: 1, Consumer: c, Kind: "work", Row: "vision.ds", Branch: runnerBranchName_internal(c), BaseSHA: testBaseSHA,
			Gen: 1, State: JobStaged, Fix: h.fix[c], DoneWhen: []string{"go test ./..."}}
		if h.prior[c] != "none" { // an earlier attempt pushed the prior head
			rec.PriorHead, rec.StartTip = testPriorSHA, testPriorSHA
			h.git.mu.Lock()
			h.git.originHeads[rec.Branch] = testPriorSHA
			h.git.mu.Unlock()
		}
		writeStagedRecord_internal(t, dir, rec)
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
		h.git.mu.Lock()
		h.git.headByWT[filepath.Join(dir, "repo")] = testWorkSHA // the harness's commit: a regressed retire would push it
		h.git.mu.Unlock()
		h.slotsInUse++
		h.alive[c] = 1
		h.stopped[c], h.sealedAt[c] = false, h.seals_internal(c)
		h.jobState[c] = JobRunning
	case "Harvest", "Die":
		h.finishRun_internal(t, c, act, dir)
	case "Rebase", "Conflict", "Push", "ReportRefused", "Report", "Clean":
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
	case "Withdraw", "Retire", "HolderTake", "HolderPush":
		h.branchStep_internal(t, act, c)
	case "Restart":
		h.env.crashed = false
		h.env.storeDown = false
		h.env.restarted = true
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
	h.seal_internal(t, c, kept)
	// Its one push, by lease, while it still holds the card.
	if err := h.runner.pushBranch_internal(context.Background(), &kept, testWorkSHA); err != nil {
		t.Fatalf("Release %s: the push by lease: %v", c, err)
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
	h.pushed[c], h.gone[c], h.jobState[c] = true, &kept, JobCleaned
	h.known[c] = kept.PushedHead // releaseNoBlame_internal reports the head it pushed
	h.finalAt[c] = len(h.git.pushes())
	h.env.released = true
}

// branchStep_internal is defect 78's three steps: the sprint withdraws c
// (and re-deals its card to another row); the next pass retires c's job
// through the worker's own finalJob_internal, which must push nothing; the
// new holder, on another row, pushes c's branch.
func (h *runnerWalkHarness_internal) branchStep_internal(t *testing.T, act, c string) {
	t.Helper()
	dir := h.jobDirPath_internal(c)
	switch act {
	case "Withdraw":
		rec, err := h.st.Apply(context.Background(), store.Op{
			ID: fmt.Sprintf("withdraw-%s", c), Epoch: 1, Actor: "sprint", Kind: "withdraw",
			At: h.t0.Add(time.Duration(h.stepN) * time.Second), Writes: []store.Write{store.SetCardState{Card: c, State: "fail"}},
		})
		if err != nil || !rec.Accepted {
			t.Fatalf("Withdraw %s: err=%v rec=%+v", c, err, rec)
		}
		h.finalAt[c] = len(h.git.pushes())
	case "Retire":
		if kept, err := ReadJobRecord(dir); err == nil {
			h.runner.finalJob_internal(ConsumerSummary{ID: c, State: card.Fail, Gen: 2}, kept)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("Retire %s: the job directory is still there: %v", c, err)
			}
			h.gone[c] = &kept
		}
		h.jobState[c] = JobCleaned
	case "HolderTake": // the next holder's take: its prior head is StartHead's, as take hands it over
		h.git.mu.Lock()
		tip := h.git.originHeads[runnerBranchName_internal(c)]
		h.git.mu.Unlock()
		h.from[c], h.took[c] = StartHead(testBaseSHA, h.known[c], tip), true
	case "HolderPush":
		h.git.mu.Lock()
		h.git.originHeads[runnerBranchName_internal(c)] = testHolderSHA
		h.git.mu.Unlock()
		h.holder[c] = true
	}
}

// seal_internal is the worker's own seal and collect of c's tree
// (sealAndCollect_internal); jobDir.sealed is read back from the seal
// steps the wall saw (seals_internal), never set by the walk.
func (h *runnerWalkHarness_internal) seal_internal(t *testing.T, c string, rec JobRecord) {
	t.Helper()
	dir := h.jobDirPath_internal(c)
	if _, err := h.runner.sealAndCollect_internal(context.Background(), Job{Consumer: c, Row: rec.Row, JobDir: dir, WorktreeDir: filepath.Join(dir, "repo")}, rec); err != nil {
		t.Fatalf("seal %s: %v", c, err)
	}
}

// seals_internal is how many seal steps the walk's wall ran on c's tree.
func (h *runnerWalkHarness_internal) seals_internal(c string) int {
	n, out := 0, filepath.Join(h.jobDirPath_internal(c), SealBundle)
	for _, argv := range *h.wall.steps {
		if slices.Contains(argv, out) {
			n++
		}
	}
	return n
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
		// The harness said ok and committed nothing over the prior head
		// (harvestWorkHead_internal's case): nothing is pushed.
		if h.atPrior[c] = h.prior[c] != "none" && (h.atPriorAlways || h.rng != nil && h.rng.Intn(2) == 0); h.atPrior[c] {
			rec.Outcome.Head, rec.PushedHead = rec.PriorHead, rec.PriorHead
		}
	}
	if err := WriteJobRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	h.alive[c] = 0
	h.slotsInUse--
	h.seal_internal(t, c, rec)             // the seal step, after the harness
	if act == "Harvest" && !h.atPrior[c] { // harvestWorkHead_internal pushes the sealed head, by lease
		if err := h.runner.pushBranch_internal(context.Background(), &rec, testWorkSHA); err != nil {
			t.Fatalf("Harvest %s: the push by lease: %v", c, err)
		}
		if err := WriteJobRecord(dir, rec); err != nil {
			t.Fatal(err)
		}
	}
	h.jobState[c] = JobHarvested
}

func (h *runnerWalkHarness_internal) advancePostRun_internal(t *testing.T, c, act, dir string) {
	t.Helper()
	ctx := context.Background()
	switch act {
	case "Rebase":
		h.dropped[c] = !streamStepMerges_internal(t)
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
	case "ReportRefused": // sendReportAndClean_internal's, bound to the code by TestARefusedOkAtThePriorHeadIsReportedAsTheFail
		rec, _ := ReadJobRecord(dir)
		rec.Outcome = nothingNew_internal("the prior head is one an attempt finished at")
		_ = WriteJobRecord(dir, rec)
		h.bounced[c], h.nothingNew[c] = true, true
	case "Push":
		rec, _ := ReadJobRecord(dir)
		if !h.atPrior[c] { // at the prior head there is nothing of this job's to push
			if err := h.runner.pushBranch_internal(ctx, &rec, testWorkSHA); err != nil {
				t.Fatalf("Push %s: %v", c, err)
			}
		}
		h.pushed[c] = true
		if rec.ResultID == "" {
			rec.ResultID = resultID_internal(1, c, 1)
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

// atPriorStep_internal is the step after a harvest at the prior head, taken
// by the worker's own rebaseAndTestStep_internal (red: its done-when is
// red); the model's action is read off the outcome the code left, so TLC
// checks the code's choice against Runner.tla's guards: Rebase (verified
// there), NothingNew (a fix) or Conflict (red there).
func (h *runnerWalkHarness_internal) atPriorStep_internal(t *testing.T, c string, red bool) string {
	t.Helper()
	h.stepN++
	dir := h.jobDirPath_internal(c)
	rec, err := ReadJobRecord(dir)
	if err != nil {
		t.Fatal(err)
	}
	h.wall.mu.Lock()
	h.wall.testFn = func(Job, string) (WallRunResult, error) {
		if red {
			return WallRunResult{ExitCode: 1, Output: []byte("--- FAIL: TestX")}, nil
		}
		return WallRunResult{}, nil
	}
	h.wall.mu.Unlock()
	if err := h.runner.rebaseAndTestStep_internal(context.Background(), dir, &rec); err != nil {
		t.Fatalf("the step at the prior head of %s: %v", c, err)
	}
	act := "Rebase"
	switch {
	case rec.Outcome.Result == "ok" && rec.Outcome.Head == rec.PriorHead:
	case rec.Outcome.Result == "fail" && rec.Outcome == nothingNew_internal(""):
		act, h.bounced[c], h.nothingNew[c] = "NothingNew", true, true
	case rec.Outcome.Result == "fail":
		act, h.bounced[c] = "Conflict", true
	default:
		t.Fatalf("the step at the prior head of %s left %+v: no action of Runner.tla", c, rec.Outcome)
	}
	rec.State = JobRebased // as the walk's own Rebase and Conflict leave it
	if err := WriteJobRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	h.jobState[c] = JobRebased
	return act
}

// walkPriorHead_internal is a scripted walk of one of the prior-head paths
// the seeds reach rarely, c1's. A fix: nothing new is its fail. No fix and
// a finished prior head (a card held in review and unheld): its ok,
// verified, is refused and reported as the fail. No fix and an unfinished
// one (an attempt pushed it and died): c1 finishes ok there. c2 is a first
// attempt.
func walkPriorHead_internal(t *testing.T, prior string, fix bool) []runnerTraceStep_internal {
	t.Helper()
	h := newRunnerWalkHarness_internal(t)
	h.prior["c1"], h.fix["c1"], h.atPriorAlways = prior, fix, true
	steps := []runnerTraceStep_internal{refinementRunnerState_internal(t, h, "Init", "none")}
	do := func(act, c string) {
		if act == "AtPrior" {
			act = h.atPriorStep_internal(t, c, false)
		} else {
			h.applyStep_internal(t, act, c)
		}
		steps = append(steps, refinementRunnerState_internal(t, h, act, c))
	}
	do("PassStart", "none")
	for _, act := range []string{"Take", "Stage", "Run", "Harvest", "AtPrior", "Push"} {
		do(act, "c1")
	}
	if prior == "finished" && !fix {
		do("ReportRefused", "c1")
	}
	for _, act := range []string{"Report", "Clean"} {
		do(act, "c1")
	}
	for _, act := range []string{"Take", "Stage", "Run", "Harvest", "Rebase", "Push", "Report", "Clean"} { // c2, a first attempt
		do(act, "c2")
	}
	return append(steps, refinementRunnerState_internal(t, h, "Done", "none"))
}

func walkRunnerTrace_internal(t *testing.T, seed int64) []runnerTraceStep_internal {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	h := newRunnerWalkHarness_internal(t)
	h.rng = rng
	// The deal (Runner.tla Deals): c1 carries one, c2 is a first attempt.
	h.prior["c1"] = []string{"none", "unfinished", "finished"}[rng.Intn(3)]
	h.fix["c1"] = h.prior["c1"] != "none" && rng.Intn(2) == 0
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
	if m.act == "Withdraw" && rng.Intn(2) != 0 { // a withdraw is rarer than a job's own step
		m = moves[0]
	}
	if m.act == "AtPrior" { // the code picks the model's action
		return h.atPriorStep_internal(t, m.arg, rng.Intn(4) == 0), m.arg
	}
	h.applyStep_internal(t, m.act, m.arg)
	return m.act, m.arg
}

// consumerMoves_internal are c's enabled job moves in st: a take (the
// acknowledgement of a card dealt working) only in a pass; a release after
// a restart, of a job left staged or harvested (a staged job that finds
// every slot held waits for one); a job's own moves only
// while its consumer is working; the withdraw, the retire and the holder's
// push of defect 78 (branchMoves_internal).
func consumerMoves_internal(st runnerTraceStep_internal, c string) []runnerMove_internal {
	moves := branchMoves_internal(st, c)
	if st.storeState[c] != "working" && runnerRetireStates_internal[st.jobState[c]] {
		return moves // a withdrawn job's only move is Retire
	}
	if (st.jobState[c] == JobStaged || st.jobState[c] == JobHarvested) && st.env.restarted && !st.env.released &&
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
		if st.jobDir[c].outcome == "ok" && st.jobDir[c].atPrior {
			moves = append(moves, runnerMove_internal{"AtPrior", c}) // rebaseAndTestStep_internal decides: Rebase, NothingNew or Conflict
		} else if st.jobDir[c].outcome == "ok" {
			moves = append(moves, runnerMove_internal{"Rebase", c}, runnerMove_internal{"Conflict", c})
		} else {
			moves = append(moves, runnerMove_internal{"Push", c})
		}
	case JobRebased:
		moves = append(moves, runnerMove_internal{"Push", c})
	case JobReporting:
		if d := st.jobDir[c]; d.outcome == "ok" && d.atPrior && d.prior == "finished" { // the card machine refuses it (Runner.tla Refuses)
			moves = append(moves, runnerMove_internal{"ReportRefused", c})
		} else {
			moves = append(moves, runnerMove_internal{"Report", c})
		}
	case JobRecorded:
		moves = append(moves, runnerMove_internal{"Clean", c})
	}
	return moves
}

// runnerRetireStates_internal is Runner.tla's RetireStates: a job with no
// harness and no report owed.
var runnerRetireStates_internal = map[string]bool{JobNone: true, JobTaken: true, JobStaged: true, JobHarvested: true, JobRebased: true}

// branchMoves_internal: Withdraw of a working consumer with no harness and
// no report owed; Retire of a withdrawn one's job in a pass; HolderPush once
// its card is re-dealt.
func branchMoves_internal(st runnerTraceStep_internal, c string) []runnerMove_internal {
	var moves []runnerMove_internal
	retireable := runnerRetireStates_internal[st.jobState[c]]
	switch {
	case st.env.crashed || st.harnessAlive[c] != 0 || !retireable:
	case st.storeState[c] == "working" && !st.env.storeDown && st.jobState[c] != JobNone && st.jobState[c] != JobTaken:
		// The walk withdraws a job with a directory (staged, harvested,
		// rebased); the model allows the rest too.
		moves = append(moves, runnerMove_internal{"Withdraw", c})
	case st.storeState[c] == "fail" && st.pass == "planning":
		moves = append(moves, runnerMove_internal{"Retire", c})
	}
	switch {
	case st.storeState[c] != "fail" || st.branch[c].holderWrote:
	case st.branch[c].from == "unset":
		moves = append(moves, runnerMove_internal{"HolderTake", c})
	default:
		moves = append(moves, runnerMove_internal{"HolderPush", c})
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
		b.WriteString(fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, storeState |-> [c1 |-> %q, c2 |-> %q], jobState |-> [c1 |-> %q, c2 |-> %q], slotsInUse |-> %d, width |-> %d, jobDir |-> [c1 |-> %s, c2 |-> %s], harnessAlive |-> [c1 |-> %d, c2 |-> %d], appliedOps |-> %s, env |-> %s, pass |-> %q, branch |-> [c1 |-> %s, c2 |-> %s]]%s\n",
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
			formatRunnerBranchTLA_internal(s.branch["c1"]),
			formatRunnerBranchTLA_internal(s.branch["c2"]),
			comma))
	}
	b.WriteString(">>\n")
	b.WriteString("=============================================================================\n")
	return b.String()
}

func formatRunnerJobDirTLA_internal(d runnerJobDirState_internal) string {
	return fmt.Sprintf("[exists |-> %s, creates |-> %d, pid |-> %d, resId |-> %d, pushed |-> %s, outcome |-> %q, bounced |-> %s, stopped |-> %s, sealed |-> %s, keepsPrior |-> %s, prior |-> %q, fix |-> %s, atPrior |-> %s, nothingNew |-> %s]",
		tlaBool_internal(d.exists), d.creates, d.pid, d.resID, tlaBool_internal(d.pushed), d.outcome, tlaBool_internal(d.bounced), tlaBool_internal(d.stopped), tlaBool_internal(d.sealed), tlaBool_internal(d.keepsPrior),
		d.prior, tlaBool_internal(d.fix), tlaBool_internal(d.atPrior), tlaBool_internal(d.nothingNew))
}

func formatRunnerBranchTLA_internal(b runnerBranch_internal) string {
	return fmt.Sprintf("[tip |-> %q, holderWrote |-> %s, finalPushes |-> %d, known |-> %q, from |-> %q]", b.tip, tlaBool_internal(b.holderWrote), b.finalPushes, b.known, b.from)
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
	return fmt.Sprintf("[crashes |-> %d, storeDown |-> %s, crashed |-> %s, released |-> %s, restarted |-> %s]",
		e.crashes, tlaBool_internal(e.storeDown), tlaBool_internal(e.crashed), tlaBool_internal(e.released), tlaBool_internal(e.restarted))
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
	seen := map[string]int{}
	for seed := int64(1); seed <= 32; seed++ {
		steps := walkRunnerTrace_internal(t, seed)
		fresh := false // a walk of an action no earlier seed walked
		for _, s := range steps {
			fresh = fresh || seen[s.action] == 0
			seen[s.action]++
		}
		last := steps[len(steps)-1]
		if last.action != "Done" {
			t.Fatalf("seed %d: did not reach Done: %+v", seed, last)
		}
		tlaText := formatRunnerTraceTLA_internal(steps)
		// Per commit (-short) TLC checks the first 8 walks and each later one
		// that walks an action they did not; the nightly checks every walk.
		if traceDir := os.Getenv("NOVA_TRACE_DIR"); traceDir != "" && (!testing.Short() || seed <= 8 || fresh) {
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
	// The prior-head paths few seeds reach, each walked once by script.
	for name, deal := range map[string]struct {
		prior string
		fix   bool
	}{"finished": {prior: "finished"}, "unfinished": {prior: "unfinished"}, "fix": {prior: "finished", fix: true}} {
		scripted := walkPriorHead_internal(t, deal.prior, deal.fix)
		for _, s := range scripted {
			seen[s.action]++
		}
		if traceDir := os.Getenv("NOVA_TRACE_DIR"); traceDir != "" {
			sub := filepath.Join(traceDir, "runner-walk-prior-"+name)
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "RunnerTraceLog.tla"), []byte(formatRunnerTraceTLA_internal(scripted)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The seeds, together, walk every path the model checks the code on,
	// defect 78's withdraw, retire and holder's push among them.
	for _, a := range []string{"Rebase", "NothingNew", "Conflict", "Push", "ReportRefused", "Report", "Clean", "Release", "Stop", "Withdraw", "Retire", "HolderTake", "HolderPush"} {
		if seen[a] == 0 {
			t.Errorf("no seed walks %s: the trace binding does not cover it (walked: %v)", a, seen)
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

// streamStepMerges_internal runs the worker's stream step
// (OSGitOps.RebaseOntoStream) on a fake git and says whether it merges the
// stream in and never rebases: Runner.tla's keepsPrior after Rebase.
func streamStepMerges_internal(t *testing.T) bool {
	t.Helper()
	fx := edge.NewFakeExec()
	fx.On("git", func(edge.FakeCall) edge.FakeOutcome { return edge.FakeOutcome{} })
	g := &OSGitOps{WorkRoot: t.TempDir(), Exec: fx}
	if _, err := g.RebaseOntoStream(context.Background(), t.TempDir(), testCloneURL, "main"); err != nil {
		t.Fatalf("the stream step: %v", err)
	}
	merges := false
	for _, c := range fx.Calls() {
		switch c.Cmd.Argv[gitArg0_internal] {
		case "rebase":
			return false
		case "merge":
			merges = true
		}
	}
	return merges
}
