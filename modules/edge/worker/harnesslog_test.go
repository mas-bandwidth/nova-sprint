package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// writeHarnessLog_internal writes the harness's own log in a job directory,
// where the opencode adapter declares it (Needs.LogDir).
func writeHarnessLog_internal(t *testing.T, jobDir, text string) {
	t.Helper()
	dir := filepath.Join(jobDir, "data", "opencode", "log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.log"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

const harnessLogFixture = `timestamp=2026-11-10T21:38:40Z level=INFO run=1 message="creating instance"
timestamp=2026-11-10T21:38:41Z level=ERROR run=1 message="Failed to fetch models.dev" cause="network denied"
timestamp=2026-11-10T21:40:00Z level=INFO run=1 message=tool
timestamp=2026-11-10T21:47:59Z level=ERROR run=1 message="session error" error="APICallError: 400 the provider refused the request"
timestamp=2026-11-10T21:48:00Z level=INFO run=1 message=exiting
`

// The harness's real exit cause is in its own log, not in its last output:
// on 2026-11-10, 181 work attempts on one model ended "no verdict: the
// harness exited 1" with only the last tool output in the reason, and the
// clean step then removed the log that said why. The reason now carries the
// log's last error lines.
func TestHarnessLogErrorsAreTheLastErrorLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeHarnessLog_internal(t, dir, harnessLogFixture)
	got := harnessLogErrors_internal(dir, filepath.Join("data", "opencode", "log"))
	if !strings.Contains(got, "APICallError: 400 the provider refused the request") || !strings.Contains(got, "Failed to fetch models.dev") {
		t.Errorf("the log's error lines: %q, want its last two ERROR lines", got)
	}
	if strings.Contains(got, "level=INFO") {
		t.Errorf("the log's error lines carry an INFO line: %q", got)
	}
	if got := harnessLogErrors_internal(dir, ""); got != "" {
		t.Errorf("a harness with no log dir gave %q", got)
	}
}

// The job directory is the harness's to write: a log name it planted as a
// link out of the job is never read by the worker.
func TestHarnessLogErrorsNeverFollowALinkOutOfTheJob(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret.log")
	if err := os.WriteFile(outside, []byte("level=ERROR outside the job\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logs := filepath.Join(dir, "data", "opencode", "log")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(logs, "opencode.log")); err != nil {
		t.Fatal(err)
	}
	if got := harnessLogErrors_internal(dir, filepath.Join("data", "opencode", "log")); strings.Contains(got, "outside the job") {
		t.Errorf("read a log linked out of the job: %q", got)
	}
}

func TestNoVerdictReasonCarriesTheHarnessLog(t *testing.T) {
	t.Parallel()
	wall := &OSReserveWall{Exec: &wallExec{res: edge.Result{ExitCode: 1, Stdout: "tool output\n", PID: 4242}}}
	r := crRunner_internal(t, &crSprint{}, wall)
	r.Harnesses = map[string]Harness{"opencode": &OpenCodeHarness{}}
	dir := t.TempDir()
	writeHarnessLog_internal(t, dir, harnessLogFixture)
	job := Job{Consumer: "p1.r1", Kind: "read", Row: "vision.ds", JobDir: dir, WorktreeDir: dir, HarnessName: "opencode"}
	res, runErr := wall.RunHarness(context.Background(), job, []string{"opencode", "run"}, nil, time.Minute)
	var rec JobRecord
	if err := r.harvestStep_internal(context.Background(), job, &rec, res, runErr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Outcome.Reason, "the harness's log:") || !strings.Contains(rec.Outcome.Reason, "APICallError: 400") {
		t.Errorf("the no-verdict reason does not carry the harness's log: %q", rec.Outcome.Reason)
	}
	if !strings.HasPrefix(rec.Outcome.Reason, "no verdict:") || !strings.Contains(rec.Outcome.Reason, "tool output") {
		t.Errorf("the reason lost its head or its last output: %q", rec.Outcome.Reason)
	}
}

// A job that did not end ok keeps its logs after the clean step removes its
// directory: <root>/kept-logs/<consumer>@<gen>, each file its last
// KeptLogBytes, the newest KeptLogJobs jobs only, so a disk never fills.
func TestCleanKeepsTheLogsOfAJobThatDidNotEndOK(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r, err := NewRunner(Runner{Host: "b1", RootDir: root, Wall: &fakeWall{}, Now: testClock_internal(), Sleep: noSleep_internal, Harnesses: map[string]Harness{"opencode": &OpenCodeHarness{}}})
	if err != nil {
		t.Fatal(err)
	}
	clean := func(consumer, result string) string {
		jobDir := filepath.Join(root, "jobs", consumer)
		writeHarnessLog_internal(t, jobDir, harnessLogFixture)
		big := strings.Repeat("x", KeptLogBytes+100) + "END"
		if err := os.WriteFile(filepath.Join(jobDir, "stderr.log"), []byte(big), 0o600); err != nil {
			t.Fatal(err)
		}
		rec := JobRecord{Epoch: 1, Consumer: consumer, Kind: "read", Gen: 1, Harness: "opencode", Outcome: Outcome{Result: result, Reason: "x"}}
		if err := r.cleanJobDir_internal(context.Background(), jobDir, rec); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(jobDir); !os.IsNotExist(err) {
			t.Fatalf("the clean left the job directory: %v", err)
		}
		return filepath.Join(root, KeptLogsDir, consumer+"@1")
	}
	if kept := clean("ok.r1", "ok"); fileExists_internal(kept) {
		t.Errorf("an ok job's logs were kept at %s", kept)
	}
	kept := clean("nv.r1", "fault")
	if b, err := os.ReadFile(filepath.Join(kept, "data", "opencode", "log", "opencode.log")); err != nil || !strings.Contains(string(b), "APICallError") {
		t.Errorf("the harness's log was not kept: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(kept, "stderr.log")); err != nil || len(b) != KeptLogBytes || !strings.HasSuffix(string(b), "END") {
		t.Errorf("stderr.log kept as %d bytes (%v), want its last %d", len(b), err, KeptLogBytes)
	}
	for i := range KeptLogJobs + 3 {
		clean("more"+strings.Repeat("x", i)+".w1", "fail")
	}
	entries, err := os.ReadDir(filepath.Join(root, KeptLogsDir))
	if err != nil || len(entries) != KeptLogJobs {
		t.Errorf("kept %d jobs' logs (%v), want the newest %d", len(entries), err, KeptLogJobs)
	}
}
