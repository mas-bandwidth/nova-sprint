package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Staging merges the stream's tip into a work retry's prior head
// (checkoutJob_internal, 2026-11-15): a retry started on the head the last
// attempt left met the stream step's conflict again after every attempt
// (sm-a1-c-unnamed-record-check, four fails on mini-m5). The merge itself
// is real git (premerge_functional_test.go); these are the worker's
// decisions around it.

const testMergeSHA = "7777777777777777777777777777777777777777"

// Only a work retry, from a prior head that is not its base, onto a known
// stream tip it does not hold, is merged; everything else stages as before.
func TestPreMergeTipOnlyForAWorkRetry(t *testing.T) {
	t.Parallel()
	retry := Job{Kind: "work", Base: "main", BaseSHA: testBaseSHA, PriorHead: testPriorSHA}
	for _, c := range []struct {
		name string
		j    Job
		tip  string
		want string
	}{
		{"a work retry", retry, testRebSHA, testRebSHA},
		{"the tip is the prior head", retry, testPriorSHA, ""},
		{"no tip fetched", retry, "", ""},
		{"a first attempt", Job{Kind: "work", Base: "main", BaseSHA: testBaseSHA}, testRebSHA, ""},
		{"a prior head at the base", Job{Kind: "work", Base: "main", BaseSHA: testBaseSHA, PriorHead: testBaseSHA}, testRebSHA, ""},
		{"a read", Job{Kind: "read", Base: "main", BaseSHA: testBaseSHA, PriorHead: testPriorSHA, Head: testPriorSHA}, testRebSHA, ""},
		{"no stream base", Job{Kind: "work", BaseSHA: testBaseSHA, PriorHead: testPriorSHA}, testRebSHA, ""},
	} {
		if got := preMergeTip_internal(c.j, c.tip); got != c.want {
			t.Errorf("%s: preMergeTip_internal = %q, want %q", c.name, got, c.want)
		}
	}
}

// The prompt and JOB.md say the same about staging's merge: nothing when
// there is none, the merge commit when it went clean, and when it
// conflicted, the files, that the markers are the harness's to resolve
// keeping the card's intent, and that generated/ is regenerated, never
// hand-merged.
func TestPreMergeNoteInThePromptAndJobMD(t *testing.T) {
	t.Parallel()
	j := Job{Consumer: "p1.w3", Kind: "work", Repo: "o/r", Base: "main", Branch: "sprint/s1/p1", BaseSHA: testBaseSHA, PriorHead: testPriorSHA, JobDir: t.TempDir()}
	read := func() (string, string) {
		if err := writeJobMD_internal(j); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(j.JobDir, JobMDName))
		if err != nil {
			t.Fatal(err)
		}
		return string(b), buildPrompt_internal(j)
	}
	md, prompt := read()
	if strings.Contains(md, "The worker merged") || strings.Contains(prompt, "The worker merged") {
		t.Errorf("no merge, yet the prompt or JOB.md speaks of one:\n%s\n%s", md, prompt)
	}
	j.PreMerge = &PreMerge{Tip: testRebSHA, Head: testMergeSHA}
	md, prompt = read()
	note := preMergeNote_internal(j)
	for _, want := range []string{"merged main at " + testRebSHA, testMergeSHA, "Continue from it"} {
		if !strings.Contains(note, want) {
			t.Errorf("the clean merge's note %q does not say %q", note, want)
		}
	}
	if !strings.Contains(md, note) || !strings.Contains(prompt, note) {
		t.Errorf("the note is not in both:\nJOB.md:\n%s\nprompt:\n%s", md, prompt)
	}
	j.PreMerge.Conflicts = []string{"src/record.go", "generated/record.json"}
	md, prompt = read()
	note = preMergeNote_internal(j)
	for _, want := range []string{"conflicted", "src/record.go", "generated/record.json", "<<<<<<<", "You must resolve every one of them", "keeping the card's intent", "Never hand-merge a file under generated/", "regenerate it"} {
		if !strings.Contains(note, want) {
			t.Errorf("the conflicted merge's note %q does not say %q", note, want)
		}
	}
	if !strings.Contains(md, note) || !strings.Contains(prompt, note) {
		t.Errorf("the conflict note is not in both:\nJOB.md:\n%s\nprompt:\n%s", md, prompt)
	}
}

// premergeRunner_internal is a runner whose git is g and whose wall runs
// the seal step, with c's record at harvest: a work retry from testPriorSHA
// that staging merged testRebSHA into (testMergeSHA).
func premergeRunner_internal(t *testing.T, g *fakeGit, finished bool) (*Runner, Job, JobRecord) {
	t.Helper()
	r, err := NewRunner(Runner{Now: (&gateClock_internal{sec: 1600}).now, Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: &cr2Sprint{}, Wall: &gateWall_internal{}, Git: g, DefaultHarness: &ScriptHarness{}, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	rec := JobRecord{Consumer: "p1.w3", Kind: "work", Row: "vision.ds", Repo: "o/r", Forge: "github", Gen: 2, StreamRef: "main", Branch: "sprint/s1/p1",
		BaseSHA: testBaseSHA, PriorHead: testPriorSHA, StartTip: testPriorSHA, PriorFinished: finished, StartedAt: 1000, DeadlineAt: 6400,
		DoneWhen: []string{"go test ./..."}, PreMerge: &PreMerge{Tip: testRebSHA, Head: testMergeSHA}, State: JobRunning}
	jobDir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.RootDir, RemotesDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, RemotesDir, rec.Consumer), []byte(recordRemote_internal(rec)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := Job{Consumer: rec.Consumer, Kind: "work", Row: rec.Row, Repo: rec.Repo, Forge: rec.Forge, Branch: rec.Branch, BaseSHA: rec.BaseSHA, PriorHead: rec.PriorHead,
		JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail, PreMerge: rec.PreMerge}
	g.originHeads[rec.Branch] = testPriorSHA
	g.headByWT[j.WorktreeDir] = testMergeSHA // staging left the tree at its merge
	return r, j, rec
}

// A harness that adds nothing over staging's merge added nothing over the
// prior head: nothing is pushed, and the head it reports is the prior head,
// so a finished one is NothingNew and an unfinished one is verified with
// the stream merged in by the stream step, as before the merge at staging.
// The collect is told the tip staging merged (Sealed.Merged).
func TestNothingOverThePreMergeIsNothingOverThePriorHead(t *testing.T) {
	t.Parallel()
	for _, finished := range []bool{true, false} {
		g := newFakeGit_internal()
		r, j, rec := premergeRunner_internal(t, g, finished)
		out := r.harvestWorkHead_internal(context.Background(), j, &rec, Outcome{Result: "ok"})
		if out.Result != "ok" || out.Head != testPriorSHA || rec.PushedHead != testPriorSHA {
			t.Fatalf("finished=%v: outcome %+v pushed %q, want ok at the prior head, not staging's merge", finished, out, rec.PushedHead)
		}
		if p := g.pushes(); len(p) != 0 {
			t.Errorf("finished=%v: pushes %v: staging's merge alone is never pushed", finished, p)
		}
		if len(g.collected) != 1 || g.collected[0].Merged != testRebSHA || g.collected[0].Base != testPriorSHA {
			t.Errorf("finished=%v: collected %+v, want the prior head as base and staging's tip as merged", finished, g.collected)
		}
		rec.Outcome, rec.State = out, JobHarvested
		if err := r.rebaseAndTestStep_internal(context.Background(), j.JobDir, &rec); err != nil {
			t.Fatal(err)
		}
		if finished && rec.Outcome != nothingNew_internal() {
			t.Errorf("at a finished prior head: %+v, want NothingNew", rec.Outcome)
		}
		if !finished && (rec.Outcome.Result != "ok" || !strings.Contains(rec.Outcome.Reason, "this attempt added nothing")) {
			t.Errorf("at an unfinished prior head: %+v, want it verified there, saying this attempt added nothing", rec.Outcome)
		}
	}
}

// Work over staging's merge is the attempt's head, pushed by lease from the
// branch tip the job started from.
func TestWorkOverThePreMergeIsPushed(t *testing.T) {
	t.Parallel()
	g := newFakeGit_internal()
	r, j, rec := premergeRunner_internal(t, g, true)
	g.dirtyByWT[j.WorktreeDir] = true // the harness's work, sealed into testCommitSHA
	out := r.harvestWorkHead_internal(context.Background(), j, &rec, Outcome{Result: "ok"})
	if out.Result != "ok" || out.Head != testCommitSHA {
		t.Fatalf("outcome %+v, want ok at the work's head", out)
	}
	if p := g.pushes(); len(p) != 1 || p[0] != (fakePush{branch: "sprint/s1/p1", sha: testCommitSHA, expect: testPriorSHA}) {
		t.Errorf("pushes %v, want the work by lease from the prior head", p)
	}
}

// The stage step records staging's merge for the job's later passes and
// puts it in JOB.md; a first attempt records none.
func TestStageStepRecordsThePreMerge(t *testing.T) {
	t.Parallel()
	for _, prior := range []string{testPriorSHA, ""} {
		g := newFakeGit_internal()
		g.preMerge = &PreMerge{Tip: testRebSHA, Head: testMergeSHA, Conflicts: []string{"f.go"}}
		r, err := NewRunner(Runner{Now: (&gateClock_internal{sec: 1600}).now, Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: &cr2Sprint{}, Wall: &gateWall_internal{}, Git: g, DefaultHarness: &ScriptHarness{}, Warn: func(string) {}})
		if err != nil {
			t.Fatal(err)
		}
		jobDir := filepath.Join(r.RootDir, "jobs", "p1.w3")
		if err := os.MkdirAll(jobDir, 0o755); err != nil {
			t.Fatal(err)
		}
		j := Job{Consumer: "p1.w3", Kind: "work", Row: "vision.ds", Repo: "o/r", Base: "main", Branch: "sprint/s1/p1", BaseSHA: testBaseSHA, PriorHead: prior,
			JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
		rec := JobRecord{Consumer: j.Consumer, Kind: "work", State: JobTaken, PriorHead: prior}
		if err := r.stageStep_internal(context.Background(), &j, &rec); err != nil {
			t.Fatal(err)
		}
		kept, err := ReadJobRecord(jobDir)
		if err != nil {
			t.Fatal(err)
		}
		if prior == "" {
			if kept.PreMerge != nil || j.PreMerge != nil {
				t.Errorf("a first attempt recorded a merge: %+v", kept.PreMerge)
			}
			continue
		}
		if kept.PreMerge == nil || !slices.Equal(kept.PreMerge.Conflicts, g.preMerge.Conflicts) || kept.PreMerge.Head != testMergeSHA || j.PreMerge == nil {
			t.Fatalf("recorded %+v, job %+v: want staging's merge in both", kept.PreMerge, j.PreMerge)
		}
		b, err := os.ReadFile(filepath.Join(jobDir, JobMDName))
		if err != nil || !strings.Contains(string(b), "You must resolve every one of them") {
			t.Errorf("JOB.md (%v) does not carry the conflict:\n%s", err, b)
		}
	}
}
