package friend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/atomicfile"
)

// The friend's files, one writer each. The state files live in the state
// directory (DefaultStateDir under the home directory, or --state-dir),
// never on the friend's volume: a background process on this platform may
// not touch a removable volume without the person's permission (measured
// 2026-10-04, "operation not permitted" on the mkdir). The daemon writes
// Status and the log (the daemon is nova-tools' nova-friend run; SPEC-FRIEND.md,
// the files).
const (
	StatusFile = "status.json"
)

// Status is what the daemon knows, as status reads it: one file, rewritten
// whole, so a reader never sees half a state.
type Status struct {
	Friend     string    `json:"friend"`
	Harness    string    `json:"harness"`
	Started    time.Time `json:"started"`
	At         time.Time `json:"at"` // when this was written
	Connection string    `json:"connection"`
	LastPing   time.Time `json:"last_ping"`
	Seat       string    `json:"seat"`
	SeatSince  time.Time `json:"seat_since"`
	Challenge  string    `json:"challenge"`
	Nonce      string    `json:"nonce"`
	LastPong   time.Time `json:"last_pong"` // the session's pong, the only answer that ends a challenge
	Pongs      int       `json:"pongs"`
	// LastDaemonPong is when the daemon last answered a ping itself: transport,
	// never the session (docs/SPEC-FRIEND.md, session-pong.w1).
	LastDaemonPong time.Time `json:"last_daemon_pong"`
	Delivered      int       `json:"delivered"` // messages acked this run
	Beats          int       `json:"beats"`
	LastBeat       time.Time `json:"last_beat"`
	BeatError      string    `json:"beat_error,omitempty"`
	Envelope       int       `json:"envelope"`       // messages the last envelope carried (docs/SPEC-FRIEND.md, the loop)
	EnvelopeBytes  int       `json:"envelope_bytes"` // its text's size: at most the deliverer's text limit, the first message always in
	// Push is the push proof this run (proved, or unproven while the
	// session has not answered its check: nothing is delivered), PushNonce the
	// check's nonce and PushSince when the daemon started waiting, while unproven;
	// empty for a harness with no session to prove. ProofSent is the session's
	// proof (the presence file's last_heard) the sprint server last took on her
	// beat, zero before any (docs/SPEC-FRIEND.md, The push proof).
	Push      string    `json:"push,omitempty"`
	PushNonce string    `json:"push_nonce,omitempty"`
	PushSince time.Time `json:"push_since,omitzero"`
	ProofSent time.Time `json:"proof_sent,omitzero"`
	// HarnessSeen is what the harness check last read (HarnessRunning,
	// "not-seen", or empty: cannot tell). Advisory: it
	// never makes the friend down (alive.go).
	HarnessSeen string `json:"harness_seen,omitempty"`
	// HarnessAlive is the rule the harness check read it by: RuleSession (the
	// session's last turn) or RuleApp (the app in the process table); empty
	// for another signal, or before a check.
	HarnessAlive string `json:"alive,omitempty"`
	StoreError   string `json:"store_error,omitempty"`
	Width        int    `json:"width"`
	// Session is "ok", or "broken" once the provider refused BrokenAfter
	// turns in a row the same way; empty for a passive harness.
	Session       string `json:"session,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	SessionReason string `json:"session_reason,omitempty"`
	// SessionLive is the conversation a mailbox harness delivers into:
	// the one that reads, as the daemon last followed it; empty for every other harness.
	SessionLive string `json:"session_live,omitempty"`
	// Queued is her harness's own queue of deliveries not yet taken, as the last delivery
	// read it (codex: the open chat's queue), when QueueKnown.
	Queued     int       `json:"queued,omitempty"`
	QueueKnown bool      `json:"queue_known,omitempty"`
	BrokenAt   time.Time `json:"broken_at,omitempty"`
	// While the harness is at its usage limit or out of credits Session is
	// "limited", LimitKind says which (limit or credits) and
	// LimitUntil is the reset (docs/SPEC-FRIEND.md, limits-mean-down-w-r.w1~15).
	LimitKind  string    `json:"limit_kind,omitempty"`
	LimitUntil time.Time `json:"limit_until,omitzero"`
	// Mode is how the daemon delivers now (batch or one-shot), and Lanes the
	// one-shot lanes as n:session:card/turn, empty in batch.
	Mode  string `json:"mode,omitempty"`
	Lanes string `json:"lanes,omitempty"`
	// Paced is the lanes' effective width under the subscription windows' pacing
	// (pacing.go), nil before the lanes have stepped; Window is the windows' use
	// as the harness last reported it ("5h 62% 7d 31%", empty when none is live),
	// and Pacing the row's pacing as a percent.
	Paced  *int   `json:"paced,omitempty"`
	Window string `json:"window,omitempty"`
	Pacing string `json:"pacing,omitempty"`
	// Held, InboxJobs and Missing are the last inbox reconcile's counts: the
	// cards on her row, the sprint jobs in her inbox, and the held cards with no BRIEF.md
	// after it; HeldKnown is false until the server has answered once, and InboxError is
	// why the last reconcile did not finish, empty when it did.
	HeldKnown  bool   `json:"held_known,omitempty"`
	Held       int    `json:"held"`
	InboxJobs  int    `json:"inbox"`
	Missing    int    `json:"missing"`
	InboxError string `json:"inbox_error,omitempty"`
	// HeldFrom is where the last answer came from: friend cards, or the worker view while the
	// server does not serve it (no brief is written from the view).
	HeldFrom string `json:"held_from,omitempty"`
}

// DefaultStateDir is the home directory's state directory for friend,
// ~/.nova-friend/<friend>: where a daemon from before the state moved under
// --dir kept its files, and where one goes when its directory refuses them
// (DaemonStateDir).
func DefaultStateDir(home, friend string) string { return filepath.Join(home, ".nova-friend", friend) }

func statusPath(stateDir string) string { return filepath.Join(stateDir, StatusFile) }

// write writes v as JSON to path atomically, making the directory.
func write(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, append(raw, '\n'), 0o644)
}

// read reads JSON at path into v; missing is a nil error with found false.
func read(path string, v any) (found bool, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return true, fmt.Errorf("%s: %v", path, err)
	}
	return true, nil
}

// ReadStatus is what the daemon last wrote; found is false when it never has.
func ReadStatus(stateDir string) (s Status, found bool, err error) {
	found, err = read(statusPath(stateDir), &s)
	return s, found, err
}
