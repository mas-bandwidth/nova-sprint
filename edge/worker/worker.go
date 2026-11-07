// Package worker implements nova-worker (layer 4): the per-bench runner.
package worker

import (
	"cmp"
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
func FormatResultID(epoch int, consumer string, attempt, gen int) string {
	return fmt.Sprintf("res-e%d-%s-a%d-g%d", epoch, consumer, attempt, gen)
}
func FormatTakeOpID(epoch int, consumer string, gen int, row string) string {
	return fmt.Sprintf("take-e%d-%s-g%d-%s", epoch, consumer, gen, row)
}

const (
	JobNone        = "none"
	JobTaken       = "taken"
	JobStaged      = "staged"
	JobRunning     = "running"
	JobHarvested   = "harvested"
	JobRebased     = "rebased"
	JobCIWait      = "ciwait"
	JobReporting   = "reporting"
	JobRecorded    = "recorded"
	JobCleaned     = "cleaned"
	MaxReasonBytes = 4096
)

var DefaultCoordinatorHosts = []string{"studio", "studio.local"}

func CheckHost(host string, coordinatorHosts []string) error {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if h == "" {
		return errors.New("worker refused: host is empty: pass --host <bench> or set HOSTNAME")
	}
	if len(coordinatorHosts) == 0 {
		coordinatorHosts = DefaultCoordinatorHosts
	}
	short, _, _ := strings.Cut(h, ".")
	for _, raw := range coordinatorHosts {
		c := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		cShort, _, _ := strings.Cut(c, ".")
		if c != "" && (h == c || short == cShort) {
			return fmt.Errorf("worker refused: host %s is a coordinator host (no card runs on the coordinator's machine): run nova-worker on a worker bench", host)
		}
	}
	return nil
}

type Timeouts struct {
	Stage, LaunchGrace                                                                                                                                                 time.Duration
	LaunchTries                                                                                                                                                        int
	LaunchBackoff1Min, LaunchBackoff1Max, LaunchBackoff2Min, LaunchBackoff2Max, TerminateGrace, RebaseAndTest, Push, GH, BusSend, SSHConnect, CIWaitExtra, ReserveWait time.Duration
	TierDeadline                                                                                                                                                       map[string]time.Duration
}

func DefaultTimeouts() Timeouts {
	return Timeouts{
		Stage: 120 * time.Second, LaunchGrace: 15 * time.Second, LaunchTries: 3,
		LaunchBackoff1Min: 5 * time.Second, LaunchBackoff1Max: 20 * time.Second,
		LaunchBackoff2Min: 30 * time.Second, LaunchBackoff2Max: 60 * time.Second,
		TerminateGrace: 3 * time.Second, RebaseAndTest: 2 * time.Minute,
		Push: 60 * time.Second, GH: 30 * time.Second, BusSend: 5 * time.Second,
		SSHConnect: 10 * time.Second, CIWaitExtra: 10 * time.Minute, ReserveWait: 5 * time.Second,
		TierDeadline: map[string]time.Duration{"flash": 15 * time.Minute, "pro": 45 * time.Minute, "heavy": 90 * time.Minute, "frontier": 120 * time.Minute},
	}
}
func (t Timeouts) WithDefaults() Timeouts {
	d := DefaultTimeouts()
	t.Stage, t.LaunchGrace, t.LaunchTries = cmp.Or(t.Stage, d.Stage), cmp.Or(t.LaunchGrace, d.LaunchGrace), cmp.Or(t.LaunchTries, d.LaunchTries)
	t.LaunchBackoff1Min, t.LaunchBackoff1Max = cmp.Or(t.LaunchBackoff1Min, d.LaunchBackoff1Min), cmp.Or(t.LaunchBackoff1Max, d.LaunchBackoff1Max)
	t.LaunchBackoff2Min, t.LaunchBackoff2Max = cmp.Or(t.LaunchBackoff2Min, d.LaunchBackoff2Min), cmp.Or(t.LaunchBackoff2Max, d.LaunchBackoff2Max)
	t.TerminateGrace, t.RebaseAndTest, t.Push = cmp.Or(t.TerminateGrace, d.TerminateGrace), cmp.Or(t.RebaseAndTest, d.RebaseAndTest), cmp.Or(t.Push, d.Push)
	t.GH, t.BusSend, t.SSHConnect = cmp.Or(t.GH, d.GH), cmp.Or(t.BusSend, d.BusSend), cmp.Or(t.SSHConnect, d.SSHConnect)
	t.CIWaitExtra, t.ReserveWait = cmp.Or(t.CIWaitExtra, d.CIWaitExtra), cmp.Or(t.ReserveWait, d.ReserveWait)
	if len(t.TierDeadline) == 0 {
		t.TierDeadline = d.TierDeadline
	}
	return t
}
func (t Timeouts) DeadlineFor(tier string) time.Duration {
	return cmp.Or(t.TierDeadline[tier], 45*time.Minute)
}

type Job struct {
	Consumer, Producer, Kind, Row, Stream, Repo, Base, StreamRef, Branch, BaseSHA, PriorHead, Head, Tier, Model, HarnessName, Brief, Prose, Finding string
	Paths, DoneWhen, Siblings                                                                                                                       []string
	Epoch, Gen, Attempt, Review, SlotWeight                                                                                                         int
	JobDir, WorktreeDir, ProxyAddr, CommitName, CommitEmail, ScriptMode                                                                             string
	RequirePR                                                                                                                                       bool
}
type Outcome struct {
	Result, Head, Reason, Finding string
	Retryable5xx                  bool
}
type Cost struct {
	MicroUSD                  int64 `json:"micro_usd"`
	Priced                    bool  `json:"priced"`
	InputTokens, OutputTokens int64
}
type JobRecord struct {
	Consumer, Producer, Kind, Row, Stream, Repo, Base, StreamRef, Branch, BaseSHA, PriorHead, TargetHead string
	DoneWhen                                                                                             []string
	Epoch, Gen, Attempt, PID, PRNumber                                                                   int
	ResultID, State, Nonce, PushedHead                                                                   string
	StartedAt, DeadlineAt                                                                                int64
	RequirePR                                                                                            bool
	Outcome                                                                                              Outcome
	Cost                                                                                                 Cost
}

func WriteJobRecord(jobDir string, rec JobRecord) error {
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
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
	if err != nil {
		return JobRecord{}, err
	}
	var rec JobRecord
	return rec, json.Unmarshal(data, &rec)
}
func TailBytes(b []byte, max int) string {
	max = cmp.Or(max, MaxReasonBytes)
	if len(b) > max {
		b = b[len(b)-max:]
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(string(b), ""), "\x00", ""))
}

type StepLogger struct {
	mu       sync.Mutex
	StateDir string
	MaxBytes int64
	Now      func() time.Time
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
	_, err = fmt.Fprintf(f, "ts=%s row=%s consumer=%s step=%s result=%s dur_ms=%d\n",
		now.Format(time.RFC3339), row, consumer, step, sanitizeLogToken_internal(result), d.Milliseconds())
	return err
}
func sanitizeLogToken_internal(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	if len(s) > 240 {
		s = s[:240]
	}
	return strings.TrimSpace(s)
}
func cloneStrings_internal(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}
