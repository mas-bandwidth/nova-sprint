// Package store is layer 1 of nova-sprint v2: the one place the sprint's state
// lives (docs/LAYER-1-STORE.md). It holds streams, cards, rows and policy
// versions, applies one checked operation at a time in one transaction, and
// logs every operation it was asked for, accepted or refused.
package store

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Store is the whole API of the layer. PG is the real one; Fake is the
// in-memory one for unit tests of the layers above.
type Store interface {
	// Snapshot is one consistent read of the current epoch.
	Snapshot(ctx context.Context) (Snapshot, error)
	// Apply runs one operation as one transaction. It returns an accepted
	// receipt, a refused receipt (nothing changed, the refusal is logged), or
	// an error; an error wrapping ErrUnknown means the outcome is not known
	// and the caller calls Replay.
	Apply(ctx context.Context, op Op) (Receipt, error)
	// Replay returns the receipt an op id already got, and false when the
	// store has never seen the id. It changes nothing.
	Replay(ctx context.Context, opID string) (Receipt, bool, error)
	// Events returns up to max events with seq above afterSeq, in seq order;
	// max <= 0 means all of them.
	Events(ctx context.Context, afterSeq int64, max int) ([]Event, error)
	// Clear starts a new epoch (n = n + 1) and logs itself as an event. It is
	// idempotent by op id like Apply, and takes no conditions or writes.
	Clear(ctx context.Context, op Op) (epoch int, err error)
}

// ErrUnknown is wrapped by every error after which the operation may or may
// not have landed: the request may have reached the server. Call Replay.
var ErrUnknown = errors.New("store: outcome unknown, call Replay with the op id")

// The closed sets the schema checks.
var (
	CardStates = []string{"waiting", "ready", "working", "review", "merging", "landed", "left"}
	RowKinds   = []string{"worker", "kin"}
	RowStates  = []string{"up", "down", "blocked"}
	PolicyKind = []string{"script", "data"}
)

// Op is one operation: conditions that must all hold and writes that all land,
// or nothing does.
type Op struct {
	ID     string    // chosen by the caller, unique forever
	Actor  string    // who asked
	Kind   string    // a word for the event log: add, take, finish, ...
	At     time.Time // the time, from the caller; the store never reads a clock
	Expect []Expect
	Writes []Write
	Note   string // kept in the receipt
}

// Stream is a row of sprint.streams.
type Stream struct {
	Name string `json:"name"`
	Repo string `json:"repo,omitempty"`
	Base string `json:"base,omitempty"`
	Held bool   `json:"held,omitempty"`
	Pos  int    `json:"pos,omitempty"`
}

// Card is a row of sprint.cards. Holder "" is no holder.
type Card struct {
	ID     string   `json:"id"`
	Stream string   `json:"stream"`
	State  string   `json:"state"`
	Gen    int      `json:"gen"`
	Score  int      `json:"score,omitempty"`
	Needs  []string `json:"needs,omitempty"`
	Holder string   `json:"holder,omitempty"`
	Head   string   `json:"head,omitempty"`
	PR     int      `json:"pr,omitempty"`
	Brief  string   `json:"brief,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// Row is a row of sprint.rows: a worker or a kin. A zero Seen is never seen.
type Row struct {
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	Width int       `json:"width,omitempty"`
	State string    `json:"state"`
	Seen  time.Time `json:"seen"`
	Flaps int       `json:"flaps,omitempty"`
}

// Policy is one version of sprint.policy.
type Policy struct {
	Version int       `json:"version"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	Body    string    `json:"body"`
	Active  bool      `json:"active,omitempty"`
	Actor   string    `json:"actor"`
	At      time.Time `json:"at"`
}

// State is a set of rows by key: the rows an operation touched, before or
// after it. Epoch is set only on the after-state of a clear.
type State struct {
	Epoch    int               `json:"epoch,omitempty"`
	Streams  map[string]Stream `json:"streams,omitempty"`
	Cards    map[string]Card   `json:"cards,omitempty"`
	Rows     map[string]Row    `json:"rows,omitempty"`
	Policies map[int]Policy    `json:"policies,omitempty"`
}

// Receipt is what an operation got. It is stored with its event, so Replay
// and a repeated Apply return it exactly.
type Receipt struct {
	Op       string    `json:"op"`
	Kind     string    `json:"kind"`
	Accepted bool      `json:"accepted"`
	Reason   string    `json:"reason,omitempty"` // why it was refused
	Seq      int64     `json:"seq"`              // its event
	Epoch    int       `json:"epoch"`            // the epoch it ran in
	At       time.Time `json:"at"`
	Note     string    `json:"note,omitempty"`
	After    *State    `json:"after,omitempty"` // the touched rows after; nil when refused
}

// Event is a row of sprint.events. Before and After are nil when refused.
type Event struct {
	Seq      int64
	Op       string
	Actor    string
	Kind     string
	Accepted bool
	Reason   string
	Before   *State
	After    *State
	Receipt  Receipt
	At       time.Time
	Epoch    int
}

// Snapshot is the whole current epoch, plus the active policy version (0 when
// none is active).
type Snapshot struct {
	Epoch   int
	Policy  int
	Streams map[string]Stream
	Cards   map[string]Card
	Rows    map[string]Row
}

func emptySnapshot(epoch int) Snapshot {
	return Snapshot{Epoch: epoch, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{}}
}

// Rebuild folds accepted events, in seq order from the first, into the
// snapshot they produce: a clear empties the epoch, every other accepted event
// lays its after-state over the rows. Guarantee 5 is Rebuild(all events) ==
// Snapshot().
func Rebuild(events []Event) Snapshot {
	s := emptySnapshot(0)
	policies := map[int]Policy{}
	for _, e := range events {
		if !e.Accepted || e.After == nil {
			continue
		}
		if e.After.Epoch != 0 {
			s = emptySnapshot(e.After.Epoch)
		}
		for k, v := range e.After.Streams {
			s.Streams[k] = v
		}
		for k, v := range e.After.Cards {
			s.Cards[k] = v
		}
		for k, v := range e.After.Rows {
			s.Rows[k] = v
		}
		for k, v := range e.After.Policies {
			policies[k] = v
		}
	}
	for v, p := range policies {
		if p.Active {
			s.Policy = v
		}
	}
	return s
}

// at is a time as the store keeps it: UTC, to the microsecond PostgreSQL holds.
func at(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

// needs is a needs list as the store keeps it: nil when empty.
func needs(n []string) []string {
	if len(n) == 0 {
		return nil
	}
	return slices.Clone(n)
}
