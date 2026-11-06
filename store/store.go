// Package store is layer 1 of nova-sprint v2: the one place the sprint's state
// lives (docs/LAYER-1-STORE.md). It holds streams, cards, rows and policy
// versions, applies one checked operation at a time in one transaction, and
// logs every operation that entered it, accepted or refused.
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
	// Snapshot is one consistent read of the current epoch, with the seq it
	// was read at.
	Snapshot(ctx context.Context) (Snapshot, error)
	// Apply runs one operation as one transaction. It ends one of four ways:
	// an accepted receipt; a refused receipt (nothing changed, the refusal is
	// logged); ErrInvalid (the op never entered the store); ErrUnknown (the
	// outcome is not known: call Replay).
	Apply(ctx context.Context, op Op) (Receipt, error)
	// Replay returns the receipt an op id already got, and false when no
	// event has that id. It changes nothing.
	Replay(ctx context.Context, opID string) (Receipt, bool, error)
	// Events returns events with seq above afterSeq, ascending, at most max
	// of them; max is capped at MaxEvents, and max <= 0 means MaxEvents.
	Events(ctx context.Context, afterSeq int64, max int) ([]Event, error)
	// Clear opens a new epoch (n = n + 1) with the protocol of Apply: an op of
	// kind clear, no writes, at most an AtSeq condition. It is logged with the
	// epoch it closed; the receipt's NewEpoch is the one it opened.
	Clear(ctx context.Context, op Op) (Receipt, error)
	// Policy returns one policy version; ErrNotFound when there is none.
	Policy(ctx context.Context, version int) (PolicyVersion, error)
}

// MaxEvents caps one Events call.
const MaxEvents = 1000

var (
	// ErrUnknown is wrapped by every error after which the operation may or
	// may not have landed: the request may have reached the server. Call
	// Replay with the op id; if it is not found, Apply again with the same id.
	ErrUnknown = errors.New("store: outcome unknown, call Replay with the op id")
	// ErrInvalid is wrapped by the error of an op that never entered the
	// store: no id, no epoch, a condition or write outside the closed sets,
	// two writes to one field. Nothing is logged. It is a caller bug.
	ErrInvalid = errors.New("store: invalid op")
	// ErrNotFound is wrapped by Policy for a version that does not exist.
	ErrNotFound = errors.New("store: not found")
)

// The closed sets the schema checks.
var (
	CardStates  = []string{"waiting", "ready", "working", "review", "merging", "landed", "fail", "blocked", "done"}
	CardKinds   = []string{"producer", "sentinel", "work", "read"}
	CardResults = []string{"", "ok", "fix", "fail", "fault"}
	Priorities  = []string{"low", "normal", "high", "critical", "blocker"} // a producer's or sentinel's level, lowest first
	RowKinds    = []string{"worker", "kin"}
	RowStates   = []string{"up", "down"}
	PolicyKinds = []string{"script", "data"}
)

// Op is one operation: conditions that must all hold and writes that all land,
// or nothing does.
type Op struct {
	ID     string    // chosen by the caller, unique forever
	Epoch  int       // the epoch the caller planned against; required (epochs start at 1)
	Actor  string    // who asked
	Kind   string    // a label for the log: add, take, finish, ...; never read by rebuild
	At     time.Time // the time, from the caller; the store never reads a clock
	Expect []Expect
	Writes []Write
	Note   string // kept in the receipt
}

// Stream is a row of sprint.streams.
type Stream struct {
	Name string `json:"name"`
	Repo string `json:"repo"`
	Base string `json:"base"`
	Held bool   `json:"held"`
	Pos  int    `json:"pos"`
}

// Card is a row of sprint.cards. Holder "" is no holder; Of "" is a producer's
// (a consumer's Of is its producer).
type Card struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Of       string   `json:"of,omitempty"`
	Stream   string   `json:"stream"`
	State    string   `json:"state"`
	Gen      int      `json:"gen"`
	Attempt  int      `json:"attempt"`
	Score    int      `json:"score"`
	Needs    []string `json:"needs,omitempty"` // hard: cannot start until these landed
	After    []string `json:"after,omitempty"` // soft: cannot land until these landed
	Holder   string   `json:"holder,omitempty"`
	Branch   string   `json:"branch"`
	Head     string   `json:"head"`
	PR       int      `json:"pr"`
	Brief    string   `json:"brief"`
	Reason   string   `json:"reason"`
	Author   string   `json:"author"`     // the row whose work the head is (schema 2)
	BaseSHA  string   `json:"base_sha"`   // the base commit the attempt started from (schema 2)
	Prior    string   `json:"prior_head"` // on a work consumer: the head the attempt continues from
	Result   string   `json:"result"`     // "", ok, fix, fail or fault
	Bounces  int      `json:"bounces"`    // on a producer: the returns that count toward blocked
	Cost     int64    `json:"cost"`       // on a consumer: what its run cost, in micro-dollars
	Priced   bool     `json:"priced"`     // false when the run's route has no price
	Priority string   `json:"priority"`   // a producer's level; normal on a consumer
}

// Row is a row of sprint.rows: a worker or a kin.
type Row struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Width   int    `json:"width"`
	State   string `json:"state"`
	Session string `json:"session"`
	Flaps   int    `json:"flaps"`
	Held    bool   `json:"held"` // the coordinator's hold: no deals while true
	Gen     int    `json:"gen"`
}

// PolicyVersion is one version of sprint.policy.
type PolicyVersion struct {
	Version int       `json:"version"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	Body    string    `json:"body"`
	Active  bool      `json:"active"`
	Actor   string    `json:"actor"`
	At      time.Time `json:"at"`
}

// State is a set of full rows by key: the rows an operation touched, before
// or after it. Epoch is set only on the after-state of a clear: the epoch it
// opened.
type State struct {
	Epoch    int                   `json:"epoch,omitempty"`
	Streams  map[string]Stream     `json:"streams,omitempty"`
	Cards    map[string]Card       `json:"cards,omitempty"`
	Rows     map[string]Row        `json:"rows,omitempty"`
	Policies map[int]PolicyVersion `json:"policies,omitempty"`
}

// Receipt is what an operation got. It is stored with its event, so Replay
// and a repeated Apply return it exactly. It carries the original op's id,
// kind, actor, epoch, at and note.
type Receipt struct {
	Op       string    `json:"op"`
	Kind     string    `json:"kind"`
	Actor    string    `json:"actor"`
	Epoch    int       `json:"epoch"` // the op's epoch
	At       time.Time `json:"at"`
	Note     string    `json:"note,omitempty"`
	Accepted bool      `json:"accepted"`
	Reason   string    `json:"reason,omitempty"`    // why it was refused
	Seq      int64     `json:"seq"`                 // its event
	NewEpoch int       `json:"new_epoch,omitempty"` // a clear's: the epoch it opened
	After    *State    `json:"after,omitempty"`     // the touched rows after; nil when refused
}

// Event is a row of sprint.events. Epoch is the store's epoch when the op ran
// (for a clear, the epoch it closed). Before and After are nil when refused.
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

// Snapshot is the whole current epoch: the last event seq when it was read,
// the epoch, the active policy version (0 when none) and the rows.
type Snapshot struct {
	Seq     int64
	Epoch   int
	Policy  int
	Streams map[string]Stream
	Cards   map[string]Card
	Rows    map[string]Row
}

func emptySnapshot(epoch int) Snapshot {
	return Snapshot{Epoch: epoch, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{}}
}

// Rebuild folds events, in seq order from the first, into the snapshot they
// produce: a clear empties the streams, cards and rows, every other accepted
// event lays its after-images over them, policy included. Guarantee 5 is
// Rebuild(every event) == Snapshot().
func Rebuild(events []Event) Snapshot {
	s := emptySnapshot(1)
	policies := map[int]PolicyVersion{}
	for _, e := range events {
		s.Seq = e.Seq
		if !e.Accepted || e.After == nil {
			continue
		}
		if e.After.Epoch != 0 {
			seq := s.Seq
			s = emptySnapshot(e.After.Epoch)
			s.Seq = seq
		}
		overlay(s.Streams, e.After.Streams)
		overlay(s.Cards, e.After.Cards)
		overlay(s.Rows, e.After.Rows)
		overlay(policies, e.After.Policies)
	}
	for v, p := range policies {
		if p.Active {
			s.Policy = v
		}
	}
	return s
}

func overlay[K comparable, V any](dst, src map[K]V) {
	for k, v := range src {
		dst[k] = v
	}
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
