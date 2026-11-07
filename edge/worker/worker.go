// Package worker implements nova-worker (layer 4): the per-bench runner.
package worker

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type (
	EdgeRequest = edge.EdgeRequest
	EdgeResult  = edge.EdgeResult
)

func RequestID(epoch int64, kind, card, sha string) string {
	return edge.RequestID(epoch, kind, card, sha)
}
func consumerOpID_internal(verb string, epoch int, consumer string, gen int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%d:%s:%d", len(verb), verb, cmp.Or(epoch, 1), consumer, gen)))
	return fmt.Sprintf("consumer:%x", sum)
}
func FormatResultID(epoch int, consumer string, _ int, gen int) string {
	return consumerOpID_internal("report", epoch, consumer, gen)
}
func FormatTakeOpID(epoch int, consumer string, gen int, _ string) string {
	return consumerOpID_internal("take", epoch, consumer, gen)
}

const (
	JobNone, JobTaken, JobStaged, JobRunning, JobHarvested = "none", "taken", "staged", "running", "harvested"
	JobRebased, JobCIWait, JobReporting, JobRecorded       = "rebased", "ciwait", "reporting", "recorded"
	JobCleaned                                             = "cleaned"
	MaxReasonBytes                                         = 4096
)

type Timeouts struct {
	Stage, LaunchGrace                                                                                                                     time.Duration
	LaunchTries                                                                                                                            int
	LaunchBackoff1Min, LaunchBackoff1Max, LaunchBackoff2Min, LaunchBackoff2Max, TerminateGrace, RebaseAndTest, Push, BusClaim, ReserveWait time.Duration
}

func DefaultTimeouts() Timeouts {
	return Timeouts{
		Stage: 120 * time.Second, LaunchGrace: 15 * time.Second, LaunchTries: 3,
		LaunchBackoff1Min: 5 * time.Second, LaunchBackoff1Max: 20 * time.Second,
		LaunchBackoff2Min: 30 * time.Second, LaunchBackoff2Max: 60 * time.Second,
		TerminateGrace: 3 * time.Second, RebaseAndTest: 2 * time.Minute,
		Push: 60 * time.Second, BusClaim: 60 * time.Second, ReserveWait: 5 * time.Second,
	}
}
func (t Timeouts) WithDefaults() Timeouts {
	d := DefaultTimeouts()
	t.Stage, t.LaunchGrace, t.LaunchTries = cmp.Or(t.Stage, d.Stage), cmp.Or(t.LaunchGrace, d.LaunchGrace), cmp.Or(t.LaunchTries, d.LaunchTries)
	t.LaunchBackoff1Min, t.LaunchBackoff1Max = cmp.Or(t.LaunchBackoff1Min, d.LaunchBackoff1Min), cmp.Or(t.LaunchBackoff1Max, d.LaunchBackoff1Max)
	t.LaunchBackoff2Min, t.LaunchBackoff2Max = cmp.Or(t.LaunchBackoff2Min, d.LaunchBackoff2Min), cmp.Or(t.LaunchBackoff2Max, d.LaunchBackoff2Max)
	t.TerminateGrace, t.RebaseAndTest, t.Push = cmp.Or(t.TerminateGrace, d.TerminateGrace), cmp.Or(t.RebaseAndTest, d.RebaseAndTest), cmp.Or(t.Push, d.Push)
	t.BusClaim, t.ReserveWait = cmp.Or(t.BusClaim, d.BusClaim), cmp.Or(t.ReserveWait, d.ReserveWait)
	return t
}

type (
	Job struct {
		Consumer, Producer, Kind, Row, Stream, Repo, Base, StreamRef, Branch, BaseSHA, PriorHead, Head, Tier, Model, HarnessName, Brief, Prose, Finding string
		Paths, DoneWhen, Siblings                                                                                                                       []string
		Epoch, Gen, Attempt, Review, SlotWeight, ProxyStatus                                                                                            int
		JobDir, WorktreeDir, ProxyAddr, CommitName, CommitEmail, ScriptMode, Forge                                                                      string
		Deadline                                                                                                                                        time.Duration
		Needs                                                                                                                                           Needs // the harness's wall, from its adapter
	}
	Outcome struct {
		Result, Head, Reason, Finding string
		Retryable5xx                  bool
		// NoVerdict: the harness wrote no verdict this worker can read (no
		// result.json, an unknown one, ok on a failed exit). A work job
		// reports it "fail: no verdict"; a read, a fault (ReadOutcomeOf).
		NoVerdict bool `json:",omitempty"`
	}
	Cost struct {
		MicroUSD                  int64 `json:"micro_usd"`
		Priced                    bool  `json:"priced"`
		InputTokens, OutputTokens int64
	}
	JobRecord struct {
		Consumer, Producer, Kind, Row, Stream, Repo, Base, StreamRef, Branch, BaseSHA, PriorHead, TargetHead string
		DoneWhen                                                                                             []string
		Epoch, Gen, Attempt, PID                                                                             int
		ResultID, State, Nonce, PushedHead, Forge                                                            string
		StartedAt, DeadlineAt                                                                                int64
		Outcome                                                                                              Outcome
		Cost                                                                                                 Cost
		// Refusals counts the sends of this record's report the sprint
		// refused the same way in a row (LastRefusal); at RefusalLimit the
		// record is set aside for a reader (defect 69).
		Refusals    int    `json:",omitempty"`
		LastRefusal string `json:",omitempty"`
		// StartTip is the card's branch on origin when the job was staged
		// ("" with StartTipRead: absent). Every push leases from PushedHead,
		// the head this job last pushed, or else from StartTip (defect 78).
		StartTip     string `json:",omitempty"`
		StartTipRead bool   `json:",omitempty"`
	}
	StepLogger struct {
		mu       sync.Mutex
		StateDir string
		MaxBytes int64
		Now      func() time.Time
	}
)

func WriteJobRecord(jobDir string, rec JobRecord) error {
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, dst := filepath.Join(jobDir, "job.json.tmp"), filepath.Join(jobDir, "job.json")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
func ReadJobRecord(jobDir string) (JobRecord, error) {
	data, err := os.ReadFile(filepath.Join(jobDir, "job.json"))
	var rec JobRecord
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("job record %s: %s", filepath.Join(filepath.Base(jobDir), "job.json"), edge.ParseFailure(err, data))
	}
	return rec, nil
}
func TailBytes(b []byte, max int) string {
	if max = cmp.Or(max, MaxReasonBytes); len(b) > max {
		b = b[len(b)-max:]
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(string(b), ""), "\x00", ""))
}
func (l *StepLogger) Log(row, consumer, step, result string, d time.Duration) error {
	if l == nil || l.StateDir == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	dir := filepath.Join(l.StateDir, "nova-worker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, row+".log")
	var rotateErr error
	if info, err := os.Stat(path); err == nil && info.Size() >= cmp.Or(l.MaxBytes, int64(10<<20)) {
		if err := os.Rename(path, path+".1"); err != nil {
			rotateErr = fmt.Errorf("step log rotation (the log keeps growing): %w", err)
		}
	}
	now := time.Unix(1000, 0).UTC()
	if l.Now != nil {
		now = l.Now().UTC()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	tok := strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(result))
	_, err = fmt.Fprintf(f, "ts=%s row=%s consumer=%s step=%s result=%s dur_ms=%d\n",
		now.Format(time.RFC3339), row, consumer, step, tok, d.Milliseconds())
	return errors.Join(err, rotateErr)
}
func cloneStrings_internal(in []string) []string { return slices.Clone(in) }
