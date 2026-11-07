// Package store is layer 1 of nova-sprint v2: the one place the sprint's state
// lives (docs/LAYER-1-STORE.md). It holds streams, cards, rows, judgements, cursors and policy
// versions, applies one checked operation at a time in one transaction, and
// logs every operation that entered it, accepted or refused.
package store

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
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
	// EventIdentity returns the op id and epoch of the event at seq, and
	// false when there is none. It reads neither image nor the receipt, so
	// a caller verifying a cursor pays for two columns, not the state.
	EventIdentity(ctx context.Context, seq int64) (op string, epoch int, found bool, err error)
	// Restored returns the digest (sha256 of the dump, from RestoreBegin) and
	// time of the store's last restore (RestoreEnd, in the restore's own
	// transaction), and false when it never was: a retried restore of the
	// same dump can tell the data is already in. It only reads.
	Restored(ctx context.Context) (digest string, at time.Time, ok bool, err error)
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
	RowStates   = []string{"up", "down"}
	RowRoles    = []string{"worker", "coordinator"} // a hooked row's role; the coordinator is also a worker
	PolicyKinds = []string{"script", "data"}
	Tiers       = []string{"flash", "pro", "heavy", "frontier"} // a card's tier
	StreamModes = []string{StreamAuto, "group", "hand"}         // how a stream's producers land
)

// JoinTiers is tiers as a row's advertised list: the known ones in Tiers'
// order, each once, comma-joined; a name not in Tiers is dropped. A list
// that does not come back from JoinTiers(SplitTiers(s)) unchanged was not
// in that form.
func JoinTiers(tiers []string) string {
	var out []string
	for _, t := range Tiers {
		if slices.Contains(tiers, t) {
			out = append(out, t)
		}
	}
	return strings.Join(out, ",")
}

// SplitTiers is a row's advertised list as tiers (nil for "").
func SplitTiers(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// StreamAuto is a stream's mode when none is given.
const StreamAuto = "auto"

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

// Stream is a row of sprint.streams. Mode and Forge are its landing
// settings (round 22): stream state, read with the snapshot, never policy
// data or a scan of the log. Mode is how its producers land, one of
// StreamModes (auto when unset); Forge is the forge it lands through, ""
// for none.
type Stream struct {
	Name  string `json:"name"`
	Repo  string `json:"repo"`
	Base  string `json:"base"`
	Held  bool   `json:"held"`
	Pos   int    `json:"pos"`
	Mode  string `json:"mode"`
	Forge string `json:"forge"`
}

// UnmarshalJSON reads a stream image, a mode the image does not carry (one
// logged before round 22) as auto, the column's default, so the log still
// rebuilds the tables of a migrated store.
func (s *Stream) UnmarshalJSON(b []byte) error {
	type plain Stream
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*s = Stream(p)
	s.Mode = cmp.Or(s.Mode, StreamAuto)
	return nil
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
	After    []string `json:"after,omitempty"` // soft: may start any time; orders its landing only behind targets that are themselves queued to land, never behind one that is not
	Holder   string   `json:"holder,omitempty"`
	Branch   string   `json:"branch"`
	Head     string   `json:"head"`
	PR       int      `json:"pr"`
	Brief    string   `json:"brief"`
	Reason   string   `json:"reason"`
	Author   string   `json:"author"`          // the row whose work the head is (schema 2)
	BaseSHA  string   `json:"base_sha"`        // the base commit the attempt started from (schema 2)
	Prior    string   `json:"prior_head"`      // on a work consumer: the head the attempt continues from
	Result   string   `json:"result"`          // "", ok, fix, fail or fault
	Bounces  int      `json:"bounces"`         // on a producer: the returns that count toward blocked
	Finding  string   `json:"finding"`         // on a producer: the last read's finding; on a work consumer: its producer's at cut, fixed
	Review   int      `json:"review"`          // on a producer: its entries into review; on a read consumer: its producer's at cut, fixed
	Red      bool     `json:"red"`             // on a producer: the last read's verdict was red (SetCardRed, round 17); false on every other kind
	Cost     int64    `json:"cost"`            // on a consumer: what its run cost, in micro-dollars
	Priced   bool     `json:"priced"`          // false when the run's route has no price
	Usage    string   `json:"usage,omitempty"` // on a consumer: its run's usage record, package usage's words (round 25); "" on every other kind
	Priority string   `json:"priority"`        // a producer's level; normal on a consumer
	Tier     string   `json:"tier"`            // flash, pro, heavy or frontier; a consumer's is its producer's
	Paths    []string `json:"paths,omitempty"`
	DoneWhen []string `json:"done_when,omitempty"`
	Prose    string   `json:"prose"` // the task a mind reads; Brief keeps the whole brief as given
	Model    string   `json:"model"` // on a consumer: the model its deal named (round 23); "" on a producer or sentinel
}

// Row is a row of sprint.rows: a worker's or a kin's sprint state, and
// nothing else. What a row is (kind, width, tiers, model, harness) is
// configuration, read from nova-config through the roster each tick, never
// copied here (round 22: "nova-config is the one true source").
type Row struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Session string `json:"session"` // the bus session the sprint hook recorded; "" when not hooked
	Role    string `json:"role"`    // worker or coordinator; "" when not hooked
	Flaps   int    `json:"flaps"`
	Held    bool   `json:"held"`   // the coordinator's hold: no deals while true
	Reason  string `json:"reason"` // why it was held or released (SetRowHeld)
	Gen     int    `json:"gen"`
	// The worker's last heartbeat (SetRowLoad): its 1-minute load average
	// in hundredths, the logical cores it was measured over (0: the row has
	// never beaten), and the nova-reserve slots in use and in all. Telemetry
	// for the board: no store rule reads it and a write of it moves no gen.
	Load1Centi int `json:"load1_centi"`
	Cores      int `json:"cores"`
	SlotsUsed  int `json:"slots_used"`
	SlotsTotal int `json:"slots_total"`
	// Tiers is what the worker's harness adapter serves, as it advertised in
	// its heartbeat: a comma list in Tiers' order, "" when it advertised
	// none in this session (a PutRow that changes the session clears it).
	// With SlotsTotal it is the roster's input for the row (defect 74:
	// roster.Advertised); no store rule reads it.
	Tiers string `json:"tiers"`
}

// Judgement is a row of sprint.judgements: a question owed to a person,
// stored so it outlives any push. It has no gen; JudgementOpen fences it.
// Deadline and ClosedAt are zero for none (NULL).
type Judgement struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`  // a word for the dashboard: red, fix, stuck, ...
	About    string    `json:"about"` // the card, stream or row it concerns
	Question string    `json:"question"`
	Command  string    `json:"command"`   // the exact verb that answers it
	Group    string    `json:"group_key"` // judgements with one key are shown and pushed as one
	State    string    `json:"state"`     // open, answered or expired
	Deadline time.Time `json:"deadline,omitzero"`
	Answer   string    `json:"answer"` // the answer, or an expiry's reason
	Actor    string    `json:"actor"`  // who answered or expired it
	OpenedAt time.Time `json:"opened_at"`
	ClosedAt time.Time `json:"closed_at,omitzero"`
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
// opened. Cursors maps a cursor's name to its seq.
type State struct {
	Epoch      int                   `json:"epoch,omitempty"`
	Streams    map[string]Stream     `json:"streams,omitempty"`
	Cards      map[string]Card       `json:"cards,omitempty"`
	Rows       map[string]Row        `json:"rows,omitempty"`
	Policies   map[int]PolicyVersion `json:"policies,omitempty"`
	Judgements map[string]Judgement  `json:"judgements,omitempty"`
	Cursors    map[string]int64      `json:"cursors,omitempty"`
	Stop       *Stop                 `json:"stop,omitempty"`
	Identity   *Identity             `json:"identity,omitempty"`
}

// Stop is the sprint's stop: one value for the whole store, as the epoch
// is, kept by a clear. A stopped sprint moves nothing: Run calls no Tick
// while Stopped. The store refuses nothing on it; ops from people still
// land. SetStopped is its one write.
type Stop struct {
	Stopped bool   `json:"stopped"`
	Reason  string `json:"reason"`
}

// Identity is the git identity every commit of the sprint is authored and
// committed under (round 24): one value for the whole store, as the stop
// is, kept by a clear. A repository's contributor agreement keys off the
// forge account, so every agent, friend and bench worker commits as one
// account on the agreement's allowlist and names itself only in the
// message. The zero value is not set. SetIdentity is its one write.
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Set is whether the identity has been written.
func (i Identity) Set() bool { return i.Name != "" && i.Email != "" }

// NeverStarted is a fresh store's stop reason: a store begins stopped, and
// nothing moves until the coordinator's first `nova-sprint start` (Glenn,
// 2026-11-07: the run ticked on a fresh store while up was at step 7).
const NeverStarted = "never started"

// FreshStop is the stop every store begins with, which no event records:
// Migrate writes it into a new store's epoch row, NewFake starts with it and
// Rebuild folds the log over it (Store.tla Empty.stopped; Run.tla Init).
var FreshStop = Stop{Stopped: true, Reason: NeverStarted}

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
// Policy is the version that was active when the op was checked (0 when
// none), so a decision can be traced to the rules that made it; Rebuild
// ignores it.
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
	Policy   int
}

// Snapshot is the whole current epoch: the last event seq when it was read,
// the epoch, the active policy version (0 when none) and the rows; Cursors
// are every cursor, whatever epoch set it (an absent one reads as 0).
type Snapshot struct {
	Seq        int64
	Epoch      int
	Policy     int
	Streams    map[string]Stream
	Cards      map[string]Card
	Rows       map[string]Row
	Judgements map[string]Judgement
	Cursors    map[string]int64
	Stopped    bool     // the sprint is stopped (Stop)
	StopReason string   // why it was stopped or started again
	Identity   Identity // the git identity the sprint commits as; zero when not set
}

func emptySnapshot(epoch int) Snapshot {
	return Snapshot{Epoch: epoch, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{}, Judgements: map[string]Judgement{}, Cursors: map[string]int64{}}
}

// Rebuild folds events, in seq order from the first, over a fresh store
// (epoch 1, FreshStop) into the snapshot they produce. Guarantee 5 is
// Rebuild(every event) == Snapshot().
func Rebuild(events []Event) Snapshot { return Fold(freshSnapshot(), events) }

// freshSnapshot is a store before its first event: epoch 1, empty, stopped
// with FreshStop's reason.
func freshSnapshot() Snapshot {
	s := emptySnapshot(1)
	s.Stopped, s.StopReason = FreshStop.Stopped, FreshStop.Reason
	return s
}

// Fold lays events, in seq order, over s, the snapshot before them, and
// returns the snapshot after them (s's maps are reused): a clear empties the
// streams, cards, rows and judgements (its after-image carries the rows) and
// keeps the cursors, which are global as seq is, the stop, the identity
// and the active policy; every other accepted event lays its after-images over them, the
// active version included. It is the one fold: Rebuild, run's copy (layer 6)
// and the trace checks all call it, so none can drift from the store.
func Fold(s Snapshot, events []Event) Snapshot {
	for _, e := range events {
		s.Seq = e.Seq
		if !e.Accepted || e.After == nil {
			continue
		}
		if e.After.Epoch != 0 {
			keep := s
			s = emptySnapshot(e.After.Epoch)
			s.Seq, s.Cursors, s.Stopped, s.StopReason, s.Policy, s.Identity = keep.Seq, keep.Cursors, keep.Stopped, keep.StopReason, keep.Policy, keep.Identity
		}
		if e.After.Stop != nil {
			s.Stopped, s.StopReason = e.After.Stop.Stopped, e.After.Stop.Reason
		}
		if e.After.Identity != nil {
			s.Identity = *e.After.Identity
		}
		overlay(s.Streams, e.After.Streams)
		overlay(s.Cards, e.After.Cards)
		overlay(s.Rows, e.After.Rows)
		overlay(s.Judgements, e.After.Judgements)
		overlay(s.Cursors, e.After.Cursors)
		for v, p := range e.After.Policies {
			if p.Active {
				s.Policy = v
			} else if s.Policy == v {
				s.Policy = 0
			}
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

// normList is a list as the store keeps it (needs, after, paths, done when,
// tiers): a copy, or nil when empty, so an empty list and none are one value.
func normList(n []string) []string {
	if len(n) == 0 {
		return nil
	}
	return slices.Clone(n)
}

// normSet is a set as the store keeps it (tiers, needs, after): sorted in
// byte order without repeats, or nil when empty, so a reorder is the same
// value (and a same-value write is refused). Fake and PG both keep it so:
// every write builds it here, and every read passes through it.
func normSet(n []string) []string {
	s := normList(n)
	slices.Sort(s)
	return slices.Compact(s)
}
