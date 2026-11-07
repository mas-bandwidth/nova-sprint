// Package card is layer 2: every legal move of a producer
// card, a consumer card and a row, and nothing else. Pure Go: no store, no
// clock, no bus. Given the records an event touches, Move, MoveConsumer,
// MoveRow, MoveStream and MoveAdd return the plan layer 1 applies (conditions
// and writes from package store) or a refusal naming why. The contract is
// AGENTS.md, Card machine; the model is tla/CardMachine.tla, checked
// with TLC (AGENTS.md, TLA+ models and trace checks), and the tables here are its actions.
package card

import (
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Card is a row of sprint.cards as the machine reads it: a producer (kind
// producer or sentinel) or a consumer (kind work or read).
type Card struct {
	ID, Kind, Of, Stream, State string
	Gen, Attempt                int
	Priority, Tier              string
	Score                       int
	Needs, After                []string
	Holder                      string // a consumer's row; none on a producer
	Author                      string // a consumer: the row whose head it reads; a producer: the row whose work the head is
	Branch, BaseSHA             string
	PriorHead, Head             string
	Result, Reason              string // reason is the status reason
	Finding                     string // a producer: the last read's finding; a work consumer: the producer's at cut
	Bounces                     int
	Review                      int  // a producer: its entries into review; a read: the producer's at cut
	Red                         bool // a producer: the last read's verdict was red; set by Red, cleared by Finished and Start
	Cost                        int64
	Priced                      bool
	Usage                       string // a consumer: its run's usage record (package usage's words)
	Brief                       string // the brief as given: immutable, copied to each consumer at cut
	Prose                       string // the contract fields Edit sets
	Paths, DoneWhen             []string
}

// Row is a row of sprint.rows as the machine reads it, with its width and
// tiers from the roster (the store keeps neither).
type Row struct {
	Name, State string
	Width, Gen  int
	Flaps       int
	Held        bool
	Reason      string
	Tiers       []string
}

// Stream is a row of sprint.streams as the machine reads it.
type Stream struct {
	Name string
	Held bool
}

// Plan is what a legal move writes: layer 1's conditions and writes, applied
// as one op. Every card the move reads is fenced by its gen, every row by its
// gen, plus the conditions the design names. A plan the machine built also
// carries, unexported, the move that built it, so Merge can run that move
// again after the plans ahead of it have been folded in.
type Plan struct {
	Expect []store.Expect
	Writes []store.Write

	label   string  // the move, for a merge refusal: "Relink p1"
	seen    records // the records the move read, before its own writes
	again   rerun   // the move, run again against folded records; nil if hand-built
	replays int     // how many moves a Merge ran again
	ev      Event   // the move, so a merge can read its fields
	parts   []Plan  // the flattened moves, when this plan is itself a merge
}

// Refusal is an illegal move, as a value: the reason names the fix.
type Refusal struct{ Reason string }

// Event is one move asked of the machine: a producer event (Move), a
// consumer event (MoveConsumer) or a row event (MoveRow). The set is closed
// by the unexported method.
type Event interface{ isEvent() }

// Producer events, one per row of the design's producer table.
type (
	Resolve  struct{}                                  // waiting -> ready when every need landed
	Park     struct{ Reason string }                   // ready -> waiting while the stream is held
	Reached  struct{}                                  // a sentinel: waiting -> landed when every need landed
	Start    struct{ Row, Branch, Base, Model string } // Model: the model the deal names, carried by the consumer to its take
	Finished struct{ Consumer string }                 // the work consumer reported ok; review +1
	Broken   struct{ Consumer string }                 // the work consumer reported fail
	Faulted  struct{ Consumer string }                 // the work consumer reported fault
	AskRead  struct{ Row, Model string }
	AskReads struct {
		Rows   []string // several reads of this head, one plan, distinct indices
		Models []string // the model the deal names for Rows[i]; nil for none
	}
	ReadYes struct{}
	ReadFix struct{ Finding string } // the read's finding, kept in its own column
	// WaiveRed moves a producer a Red sent to fix (ready, or waiting in a
	// held stream) on to merging without a new read, when that Red is still
	// recorded and a done ok read of the previous review at the same head
	// exists. Who may waive is layer 3's; the event carries no actor.
	WaiveRed struct{ Reason string }
	// Landed is merging -> landed at Head. Merge is the merge commit base
	// has (the landing's, or the hand landing's base tip), recorded in the
	// producer's reason with Reason (a hand landing's why).
	Landed struct{ Head, Merge, Reason string }
	// Red is a red landing: merging -> fix (ready, waiting when held) with
	// Reason, the CI finding, as the finding; review +1, so reads before it
	// do not count (a red landing is never terminal).
	Red      struct{ Head, Reason string }
	Released struct{ Reason string } // the work consumer of this attempt was withdrawn
	Cancel   struct{ Reason string }
	Replace  struct{ By, Reason string }
	// NeedFailed takes a dependant with its failed need. InThisOp says the
	// step fails the need earlier in the same op, so the plan carries no
	// "need is fail" condition (layer 1 checks conditions before writes).
	NeedFailed struct {
		Need     string
		InThisOp bool
	}
	Waive struct {
		Need, Reason string
		InThisOp     bool // as NeedFailed's
	}
	// Relink replaces a failed need. InThisOp, as Waive: the drop fails the
	// need earlier in the same op, so the plan carries no "need is fail" fence.
	Relink struct {
		Need, To, Reason string
		InThisOp         bool
	}
	// Edit sets prose, paths and done_when; the brief as given is immutable.
	// A nil list or "" prose leaves that field as it is.
	Edit struct {
		Prose           string
		Paths, DoneWhen []string
		Reason          string
	}
	Rescore     struct{ Score int }
	SetPriority struct{ Level, Reason string }
	Block       struct{ Reason string }
	Unblock     struct{ Reason string }
)

// Consumer events, one per row of the consumer table. There is no Take: a
// consumer is cut in working, in its row's hand at the deal; the row's
// acknowledgement is not a card move (AGENTS.md, Card machine).
type (
	// Report is a row's result. Cost, Priced and Usage are the run's usage
	// record as the engine priced it (package usage): the machine stores
	// them and prices nothing.
	Report struct {
		Row, Result, Head, Reason string
		Cost                      int64
		Priced                    bool
		Usage                     string
	}
	// Withdraw fails a live consumer; Head is its last pushed head, when it
	// has one. Withdrawing the work consumer of a working producer releases
	// the producer in the same plan.
	// Stream is the producer's stream, read with it: a release into a held
	// stream parks the producer (waiting); needed only when the withdraw
	// releases.
	// Preempt marks the blocker's preempt. The pair guard and the
	// working-consumer check read this field, never the reason text.
	Withdraw struct {
		Reason, Head string
		Stream       Stream
		Preempt      bool
	}
)

// Add is a new producer or sentinel (MoveAdd): the put-card plan. Consumers
// are cut by Start and AskRead, never added.
type Add struct {
	ID, Kind, Stream, Tier, Priority string
	Score                            int
	Needs, After                     []string
	Brief, Prose                     string
	Paths, DoneWhen                  []string
}

func (Add) isEvent() {}

// FromStore is a store card as the machine reads it.
func FromStore(c store.Card) Card {
	return Card{ID: c.ID, Kind: c.Kind, Of: c.Of, Stream: c.Stream, State: c.State, Gen: c.Gen, Attempt: c.Attempt, Priority: c.Priority,
		Tier: c.Tier, Score: c.Score, Needs: c.Needs, After: c.After, Holder: c.Holder, Author: c.Author, Branch: c.Branch,
		BaseSHA: c.BaseSHA, PriorHead: c.Prior, Head: c.Head, Result: c.Result, Reason: c.Reason, Finding: c.Finding,
		Bounces: c.Bounces, Review: c.Review, Red: c.Red, Cost: c.Cost, Priced: c.Priced, Usage: c.Usage, Brief: c.Brief, Prose: c.Prose, Paths: c.Paths, DoneWhen: c.DoneWhen}
}

// RowFromStore is a store row as the machine reads it, with width and
// tiers passed in from the roster.
func RowFromStore(r store.Row, width int, tiers []string) Row {
	return Row{Name: r.Name, State: r.State, Width: width, Gen: r.Gen, Flaps: r.Flaps, Held: r.Held, Reason: r.Reason, Tiers: tiers}
}

// StreamFromStore is a store stream as the machine reads it.
func StreamFromStore(s store.Stream) Stream { return Stream{Name: s.Name, Held: s.Held} }

// Stream events. HoldStream holds the stream and parks, in the same op,
// the ready producers named (the caller passes every ready producer of the
// stream, each once; one it misses is parked later by Park). Seq is the
// snapshot's seq: the plan fences AtSeq, so a card that became ready after
// the snapshot refuses the op. UnholdStream clears the hold and parks
// nothing (Resolve does that). A stream has no reason column; UnholdStream's
// reason is for the caller, and a park into the hold names the stream.
type HoldStream struct {
	Reason string
	Ready  []Card
	Seq    int64
}

// UnholdStream clears a held stream. Reason is not stored.
type UnholdStream struct{ Reason string }

func (HoldStream) isEvent()   {}
func (UnholdStream) isEvent() {}

// Row events, one per row of the row table.
type (
	Seen   struct{}
	Gone   struct{}
	Hold   struct{ Reason string }
	Unhold struct{ Reason string }
)

func (Resolve) isEvent()     {}
func (Park) isEvent()        {}
func (Reached) isEvent()     {}
func (Start) isEvent()       {}
func (Finished) isEvent()    {}
func (Broken) isEvent()      {}
func (Faulted) isEvent()     {}
func (AskRead) isEvent()     {}
func (AskReads) isEvent()    {}
func (ReadYes) isEvent()     {}
func (ReadFix) isEvent()     {}
func (WaiveRed) isEvent()    {}
func (Landed) isEvent()      {}
func (Red) isEvent()         {}
func (Released) isEvent()    {}
func (Cancel) isEvent()      {}
func (Replace) isEvent()     {}
func (NeedFailed) isEvent()  {}
func (Waive) isEvent()       {}
func (Relink) isEvent()      {}
func (Edit) isEvent()        {}
func (Rescore) isEvent()     {}
func (SetPriority) isEvent() {}
func (Block) isEvent()       {}
func (Unblock) isEvent()     {}
func (Report) isEvent()      {}
func (Withdraw) isEvent()    {}
func (Seen) isEvent()        {}
func (Gone) isEvent()        {}
func (Hold) isEvent()        {}
func (Unhold) isEvent()      {}

// The states. A producer is final in landed or fail, a consumer in done or
// fail.
const (
	Waiting = "waiting"
	Ready   = "ready"
	Working = "working"
	Review  = "review"
	Merging = "merging"
	Done    = "done"
	Fail    = "fail"
	Blocked = "blocked"
	Up      = "up"
	Down    = "down"
)

// LandedState is the producer's final state of success (Landed is its event).
const LandedState = "landed"

// MissingRoute is the reason kind of a fault that names a route or device
// the row lacks: a work consumer's fault whose reason starts
// with it ("missing-route: adb no device attached") blocks its producer on
// a judgement instead of a re-deal. The first word after it is the route's
// name, the one vocabulary of routes: the name a brief declares with
// `NEEDS-ROUTE: <name>` (RouteField) and a machine serves through its
// `route:<name>` role; the rest of the reason says what is missing.
const MissingRoute = "missing-route:"

// RouteFault says whether reason is a missing-route fault's, and what it
// names as missing (the route's name, then the detail).
func RouteFault(reason string) (string, bool) {
	what, ok := strings.CutPrefix(strings.TrimSpace(reason), MissingRoute)
	return strings.TrimSpace(what), ok
}

// RouteName is the route a missing-route fault's reason names: the first
// word after MissingRoute, "" when it names none.
func RouteName(reason string) string {
	what, ok := RouteFault(reason)
	if f := strings.Fields(what); ok && len(f) > 0 {
		return f[0]
	}
	return ""
}

// HeldPrefix starts the reason of a producer the coordinator holds with
// `hold <card>`: a Block whose reason is "held: <why>". The
// board lists it with the other blocked producers under its reason.
const HeldPrefix = "held: "

// ReleasedPrefix starts a released consumer's reason: the record that it was
// given back (by its row, or by a drop of its producer), not failed.
const ReleasedPrefix = "released: "

// The levels a producer's priority may take, lowest first; read consumers
// occupy a rung between high and critical in the deal order (policy's).
var Levels = []string{"low", "normal", "high", "critical", "blocker"}
