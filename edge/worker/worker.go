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

func cmpOr_internal(a, b string) string { return cmp.Or(a, b) }

// CheckHost refuses the coordinator's machine (LESSONS 4.18). The worker
// holds no list of its own: with none given it refuses to run, because a
// check against nothing would pass on the coordinator.
func CheckHost(host string, coordinatorHosts []string) error {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(cmpOr_internal(host, ""))), ".")
	if h == "" {
		return errors.New("worker refused: host is empty: pass --host <bench> or set HOSTNAME")
	}
	if len(coordinatorHosts) == 0 {
		return errors.New("worker refused: no coordinator hosts given, so the coordinator's machine cannot be told from a bench: pass --coordinator-hosts <host,host> (the machines nova-config gives the coordinator role)")
	}
	short, _, _ := strings.Cut(h, ".")
	for _, raw := range coordinatorHosts {
		c := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if cShort, _, _ := strings.Cut(c, "."); c != "" && (h == c || short == cShort) {
			return fmt.Errorf("worker refused: host %s is a coordinator host (no card runs on the coordinator's machine): run nova-worker on a worker bench", host)
		}
	}
	return nil
}

type Timeouts struct {
	Stage, LaunchGrace                                                                                                                         time.Duration
	LaunchTries                                                                                                                                int
	LaunchBackoff1Min, LaunchBackoff1Max, LaunchBackoff2Min, LaunchBackoff2Max, TerminateGrace, RebaseAndTest, Push, GH, BusClaim, ReserveWait time.Duration
}

func DefaultTimeouts() Timeouts {
	return Timeouts{
		Stage: 120 * time.Second, LaunchGrace: 15 * time.Second, LaunchTries: 3,
		LaunchBackoff1Min: 5 * time.Second, LaunchBackoff1Max: 20 * time.Second,
		LaunchBackoff2Min: 30 * time.Second, LaunchBackoff2Max: 60 * time.Second,
		TerminateGrace: 3 * time.Second, RebaseAndTest: 2 * time.Minute,
		Push: 60 * time.Second, GH: 30 * time.Second, BusClaim: 60 * time.Second, ReserveWait: 5 * time.Second,
	}
}
func (t Timeouts) WithDefaults() Timeouts {
	d := DefaultTimeouts()
	t.Stage, t.LaunchGrace, t.LaunchTries = cmp.Or(t.Stage, d.Stage), cmp.Or(t.LaunchGrace, d.LaunchGrace), cmp.Or(t.LaunchTries, d.LaunchTries)
	t.LaunchBackoff1Min, t.LaunchBackoff1Max = cmp.Or(t.LaunchBackoff1Min, d.LaunchBackoff1Min), cmp.Or(t.LaunchBackoff1Max, d.LaunchBackoff1Max)
	t.LaunchBackoff2Min, t.LaunchBackoff2Max = cmp.Or(t.LaunchBackoff2Min, d.LaunchBackoff2Min), cmp.Or(t.LaunchBackoff2Max, d.LaunchBackoff2Max)
	t.TerminateGrace, t.RebaseAndTest, t.Push = cmp.Or(t.TerminateGrace, d.TerminateGrace), cmp.Or(t.RebaseAndTest, d.RebaseAndTest), cmp.Or(t.Push, d.Push)
	t.GH, t.BusClaim, t.ReserveWait = cmp.Or(t.GH, d.GH), cmp.Or(t.BusClaim, d.BusClaim), cmp.Or(t.ReserveWait, d.ReserveWait)
	return t
}

type (
	Job struct {
		Consumer, Producer, Kind, Row, Stream, Repo, Base, StreamRef, Branch, BaseSHA, PriorHead, Head, Tier, Model, HarnessName, Brief, Prose, Finding string
		Paths, DoneWhen, Siblings                                                                                                                       []string
		Epoch, Gen, Attempt, Review, SlotWeight, ProxyStatus                                                                                            int
		JobDir, WorktreeDir, ProxyAddr, CommitName, CommitEmail, ScriptMode, Forge                                                                      string
		Deadline                                                                                                                                        time.Duration
	}
	Outcome struct {
		Result, Head, Reason, Finding string
		Retryable5xx                  bool
	}
	Cost struct {
		MicroUSD                  int64 `json:"micro_usd"`
		Priced                    bool  `json:"priced"`
		InputTokens, OutputTokens int64
	}
	JobRecord struct {
		Consumer, Producer, Kind, Row, Stream, Repo, Base, StreamRef, Branch, BaseSHA, PriorHead, TargetHead string
		DoneWhen                                                                                             []string
		Epoch, Gen, Attempt, PID, PRNumber                                                                   int
		ResultID, State, Nonce, PushedHead, Forge                                                            string
		StartedAt, DeadlineAt                                                                                int64
		Outcome                                                                                              Outcome
		Cost                                                                                                 Cost
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
	if info, err := os.Stat(path); err == nil && info.Size() >= cmp.Or(l.MaxBytes, int64(10<<20)) {
		_ = os.Rename(path, path+".1")
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
	return err
}
func cloneStrings_internal(in []string) []string { return slices.Clone(in) }
