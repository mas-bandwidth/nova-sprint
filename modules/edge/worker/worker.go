// Package worker implements nova-worker (layer 4): the per-bench runner.
package worker

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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

// resultID_internal is the op id the report verb writes a consumer's
// report under (store.ConsumerOpID), which the receipt is checked against.
func resultID_internal(epoch int, consumer string, gen int) string {
	return store.ConsumerOpID("report", cmp.Or(epoch, 1), consumer, gen)
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
		JobDir, WorktreeDir, ProxyAddr, CommitName, CommitEmail, Forge                                                                                  string
		Deadline                                                                                                                                        time.Duration
		Needs                                                                                                                                           Needs // the harness's wall, from its adapter
		// Models are declared to the harness beside Model (the probe's
		// listing: every tier candidate with a key-proxy route).
		Models []string
		// Routes are the key proxy's route names: a harness declares only
		// models whose provider is one of them.
		Routes []string
	}
	Outcome struct {
		Result, Head, Reason, Finding string
		Retryable5xx                  bool
		// NoVerdict: the harness wrote no verdict this worker can read (no
		// result.json, an unknown one, ok on a failed exit). A work job
		// reports it "fail: no verdict"; a read, a fault (ReadOutcomeOf).
		NoVerdict bool `json:",omitempty"`
	}
	// Cost is what a run's harness reported: its own dollar figure (MicroUSD,
	// Priced when it had one), sent as --cost, the charged evidence; and
	// Usage, its tokens by class as package usage's words, sent as --usage,
	// which the engine prices (never the worker).
	Cost struct {
		MicroUSD int64  `json:"micro_usd"`
		Priced   bool   `json:"priced"`
		Usage    string `json:"usage,omitempty"`
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
		// ("": absent). Every push leases from PushedHead, the head this job
		// last pushed, or else from StartTip (defect 78).
		StartTip string `json:",omitempty"`
	}
	StepLogger struct {
		mu       sync.Mutex
		StateDir string
		MaxBytes int64
		Now      func() time.Time
	}
)

func WriteJobRecord(jobDir string, rec JobRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return writeKept_internal(jobDir, "job.json", append(data, '\n'))
}
func ReadJobRecord(jobDir string) (JobRecord, error) {
	data, err := readKept_internal(jobDir, "job.json")
	var rec JobRecord
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("job record %s: %s", filepath.Join(filepath.Base(jobDir), "job.json"), edge.ParseFailure(err, data))
	}
	return rec, nil
}

// RecordsDir holds, outside every job directory (which the wall can
// write), the worker's own copy of each job's record and take receipt:
// <root>/records/<consumer>/. The worker reads only that copy; a job
// directory whose copy differs is refused (ErrJobDirChanged).
const RecordsDir = "records"

// ErrJobDirChanged: a job directory's record or receipt is not the copy the
// worker kept outside it.
var ErrJobDirChanged = errors.New("refused: the job directory's copy is not the one the worker kept outside it: the job directory was changed after the worker wrote it, and the worker acts only on its own copy")

// keptDir_internal is the worker's own copy of a job directory
// <root>/jobs/<consumer>: <root>/records/<consumer>; "" for any other
// directory (a retired record's, which is itself outside the job).
func keptDir_internal(jobDir string) string {
	if filepath.Base(filepath.Dir(jobDir)) != "jobs" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(jobDir)), RecordsDir, filepath.Base(jobDir))
}

// writeKept_internal writes name's data in the job directory and in the
// worker's kept copy, each through a new file (O_EXCL: a link the harness
// left is never followed) renamed into place.
func writeKept_internal(jobDir, name string, data []byte) error {
	dirs := []string{jobDir}
	if kept := keptDir_internal(jobDir); kept != "" {
		dirs = append(dirs, kept)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		tmp := filepath.Join(dir, name+".tmp")
		if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp, filepath.Join(dir, name))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// readKept_internal is name as the worker kept it outside jobDir, refused
// when the job directory's copy is missing or differs.
func readKept_internal(jobDir, name string) ([]byte, error) {
	kept := keptDir_internal(jobDir)
	if kept == "" {
		return os.ReadFile(filepath.Join(jobDir, name))
	}
	data, err := os.ReadFile(filepath.Join(kept, name))
	if errors.Is(err, os.ErrNotExist) && fileExists_internal(filepath.Join(jobDir, name)) { // a job taken before kept copies, or one the job dir made up
		return nil, fmt.Errorf("%s of %s: %w (the worker kept no copy: %w)", name, filepath.Base(jobDir), ErrJobDirChanged, err)
	}
	if err != nil {
		return nil, err
	}
	cp, err := os.ReadFile(filepath.Join(jobDir, name))
	if err != nil {
		return nil, fmt.Errorf("%s of %s: %w (read: %w)", name, filepath.Base(jobDir), ErrJobDirChanged, err)
	}
	if !bytes.Equal(cp, data) {
		return nil, fmt.Errorf("%s of %s: %w (its content differs)", name, filepath.Base(jobDir), ErrJobDirChanged)
	}
	return data, nil
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
