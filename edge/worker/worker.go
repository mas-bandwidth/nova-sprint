// Package worker implements nova-worker (layer 4): the per-bench runner that
package worker

import (
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

type EdgeRequest = edge.EdgeRequest
type EdgeResult = edge.EdgeResult

func RequestID(epoch int64, kind, card, sha string) string {
	return edge.RequestID(epoch, kind, card, sha)
}

const (
	JobNone      = "none"
	JobTaken     = "taken"
	JobStaged    = "staged"
	JobRunning   = "running"
	JobHarvested = "harvested"
	JobRebased   = "rebased"
	JobCIWait    = "ciwait"
	JobReporting = "reporting"
	JobRecorded  = "recorded"
	JobCleaned   = "cleaned"
)
const MaxReasonBytes = 4096

var DefaultCoordinatorHosts = []string{"studio", "studio.local"}

func CheckHost(host string, coordinatorHosts []string) error {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if h == "" {
		return errors.New("worker refused: host is empty: pass --host <bench> or set HOSTNAME")
	}
	list := coordinatorHosts
	if len(list) == 0 {
		list = DefaultCoordinatorHosts
	}
	short := h
	if i := strings.IndexByte(h, '.'); i > 0 {
		short = h[:i]
	}
	for _, raw := range list {
		c := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		cShort := c
		if i := strings.IndexByte(c, '.'); i > 0 {
			cShort = c[:i]
		}
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
		Stage:             120 * time.Second,
		LaunchGrace:       15 * time.Second,
		LaunchTries:       3,
		LaunchBackoff1Min: 5 * time.Second,
		LaunchBackoff1Max: 20 * time.Second,
		LaunchBackoff2Min: 30 * time.Second,
		LaunchBackoff2Max: 60 * time.Second,
		TerminateGrace:    3 * time.Second,
		RebaseAndTest:     2 * time.Minute,
		Push:              60 * time.Second,
		GH:                30 * time.Second,
		BusSend:           5 * time.Second,
		SSHConnect:        10 * time.Second,
		CIWaitExtra:       10 * time.Minute,
		ReserveWait:       5 * time.Second,
		TierDeadline:      map[string]time.Duration{"flash": 15 * time.Minute, "pro": 45 * time.Minute, "heavy": 90 * time.Minute, "frontier": 120 * time.Minute},
	}
}
func (t Timeouts) WithDefaults() Timeouts {
	d := DefaultTimeouts()
	if t.Stage <= 0 {
		t.Stage = d.Stage
	}
	if t.LaunchGrace <= 0 {
		t.LaunchGrace = d.LaunchGrace
	}
	if t.LaunchTries <= 0 {
		t.LaunchTries = d.LaunchTries
	}
	if t.LaunchBackoff1Min <= 0 {
		t.LaunchBackoff1Min = d.LaunchBackoff1Min
	}
	if t.LaunchBackoff1Max <= 0 {
		t.LaunchBackoff1Max = d.LaunchBackoff1Max
	}
	if t.LaunchBackoff2Min <= 0 {
		t.LaunchBackoff2Min = d.LaunchBackoff2Min
	}
	if t.LaunchBackoff2Max <= 0 {
		t.LaunchBackoff2Max = d.LaunchBackoff2Max
	}
	if t.TerminateGrace <= 0 {
		t.TerminateGrace = d.TerminateGrace
	}
	if t.RebaseAndTest <= 0 {
		t.RebaseAndTest = d.RebaseAndTest
	}
	if t.Push <= 0 {
		t.Push = d.Push
	}
	if t.GH <= 0 {
		t.GH = d.GH
	}
	if t.BusSend <= 0 {
		t.BusSend = d.BusSend
	}
	if t.SSHConnect <= 0 {
		t.SSHConnect = d.SSHConnect
	}
	if t.CIWaitExtra <= 0 {
		t.CIWaitExtra = d.CIWaitExtra
	}
	if t.ReserveWait <= 0 {
		t.ReserveWait = d.ReserveWait
	}
	if len(t.TierDeadline) == 0 {
		t.TierDeadline = d.TierDeadline
	}
	return t
}
func (t Timeouts) DeadlineFor(tier string) time.Duration {
	if d, ok := t.TierDeadline[tier]; ok && d > 0 {
		return d
	}
	return 45 * time.Minute
}

type Job struct {
	Consumer    string   `json:"consumer"`
	Producer    string   `json:"producer"`
	Kind        string   `json:"kind"`
	Row         string   `json:"row"`
	Stream      string   `json:"stream"`
	Repo        string   `json:"repo"`
	Base        string   `json:"base"`
	StreamRef   string   `json:"stream_ref"`
	Branch      string   `json:"branch"`
	BaseSHA     string   `json:"base_sha"`
	PriorHead   string   `json:"prior_head"`
	Head        string   `json:"head"`
	Tier        string   `json:"tier"`
	Model       string   `json:"model"`
	HarnessName string   `json:"harness"`
	Brief       string   `json:"brief"`
	Prose       string   `json:"prose"`
	Finding     string   `json:"finding"`
	Paths       []string `json:"paths"`
	DoneWhen    []string `json:"done_when"`
	Siblings    []string `json:"siblings"`
	Gen         int      `json:"gen"`
	Attempt     int      `json:"attempt"`
	Review      int      `json:"review"`
	SlotWeight  int      `json:"slot_weight"`
	JobDir      string   `json:"job_dir"`
	WorktreeDir string   `json:"worktree_dir"`
	ProxyAddr   string   `json:"proxy_addr"`
	CommitName  string   `json:"commit_name"`
	CommitEmail string   `json:"commit_email"`
	RequirePR   bool     `json:"require_pr"`
	ScriptMode  string   `json:"script_mode,omitempty"`
}
type Outcome struct {
	Result       string `json:"result"`
	Head         string `json:"head,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Finding      string `json:"finding,omitempty"`
	Retryable5xx bool   `json:"retryable_5xx,omitempty"`
}
type Cost struct {
	MicroUSD     int64 `json:"micro_usd"`
	Priced       bool  `json:"priced"`
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
}
type JobRecord struct {
	Consumer   string  `json:"consumer"`
	Producer   string  `json:"producer"`
	Kind       string  `json:"kind"`
	Row        string  `json:"row"`
	Stream     string  `json:"stream"`
	Repo       string  `json:"repo"`
	Base       string  `json:"base"`
	StreamRef  string  `json:"stream_ref"`
	Branch     string  `json:"branch"`
	BaseSHA    string  `json:"base_sha"`
	PriorHead  string  `json:"prior_head"`
	TargetHead string  `json:"target_head,omitempty"`
	Gen        int     `json:"gen"`
	Attempt    int     `json:"attempt"`
	ResultID   string  `json:"result_id"`
	State      string  `json:"state"`
	PID        int     `json:"pid,omitempty"`
	Nonce      string  `json:"nonce,omitempty"`
	StartedAt  int64   `json:"started_at"`
	DeadlineAt int64   `json:"deadline_at"`
	PushedHead string  `json:"pushed_head,omitempty"`
	PRNumber   int     `json:"pr_number,omitempty"`
	RequirePR  bool    `json:"require_pr,omitempty"`
	Outcome    Outcome `json:"outcome"`
	Cost       Cost    `json:"cost"`
}

func WriteJobRecord(jobDir string, rec JobRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(jobDir, "job.json.tmp")
	dst := filepath.Join(jobDir, "job.json")
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
	if err := json.Unmarshal(data, &rec); err != nil {
		return JobRecord{}, err
	}
	return rec, nil
}
func TailBytes(b []byte, max int) string {
	if max <= 0 {
		max = MaxReasonBytes
	}
	if len(b) > max {
		b = b[len(b)-max:]
	}
	s := strings.ToValidUTF8(string(b), "")
	return strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
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
	maxBytes := l.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if info, err := os.Stat(path); err == nil && info.Size() >= maxBytes {
		_ = os.Rename(path, path+".1")
	}
	now := time.Unix(1000, 0).UTC()
	if l.Now != nil {
		now = l.Now().UTC()
	}
	line := fmt.Sprintf("ts=%s row=%s consumer=%s step=%s result=%s dur_ms=%d\n",
		now.Format(time.RFC3339), row, consumer, step, sanitizeLogToken_internal(result), d.Milliseconds())
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}
func sanitizeLogToken_internal(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
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
