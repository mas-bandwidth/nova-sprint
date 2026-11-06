// Package card is layer 2 of nova-sprint v2: every legal move of a producer
// card, a consumer card and a row, and nothing else. Pure Go: no store, no
// clock, no bus. Given the records an event touches, Move, MoveConsumer and
// MoveRow return the plan layer 1 applies (conditions and writes from package
// store) or a refusal naming why. The contract is docs/LAYER-2-CARD-MACHINE.md;
// the model is tla/CardMachine.tla (landing with the model's branch), checked
// with TLC, and the tables here are its actions.
package card

import (
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
	PR                          int
	Result, Reason              string
	Bounces                     int
	Review                      int // a producer: its entries into review; a read: the producer's at cut
	Cost                        int64
	Priced                      bool
	Prose                       string // the contract fields Edit sets
	Paths, DoneWhen             []string
}

// Row is a row of sprint.rows as the machine reads it.
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
// gen, plus the conditions the design names.
type Plan struct {
	Expect []store.Expect
	Writes []store.Write
}

// Merge is plans applied as one op, in order (a preemption: the withdraw of
// the youngest consumer and the start of the blocker).
func Merge(plans ...Plan) Plan {
	var m Plan
	for _, p := range plans {
		m.Expect = append(m.Expect, p.Expect...)
		m.Writes = append(m.Writes, p.Writes...)
	}
	return m
}

// Refusal is an illegal move, as a value: the reason names the fix.
type Refusal struct{ Reason string }

// Event is one move asked of the machine: a producer event (Move), a
// consumer event (MoveConsumer) or a row event (MoveRow). The set is closed
// by the unexported method.
type Event interface{ isEvent() }

// Producer events, one per row of the design's producer table.
type (
	Resolve  struct{}                // waiting -> ready when every need landed
	Park     struct{ Reason string } // ready -> waiting while the stream is held
	Reached  struct{}                // a sentinel: waiting -> landed when every need landed
	Start    struct{ Row, Branch, Base string }
	Finished struct{ Consumer string } // the work consumer reported ok; review +1
	Broken   struct{ Consumer string } // the work consumer reported fail
	Faulted  struct{ Consumer string } // the work consumer reported fault
	AskRead  struct{ Row string }
	ReadYes  struct{}
	ReadFix  struct{ Reason string }
	Landed   struct{ Head string }
	Red      struct{ Head, Reason string } // review +1: reads before it no longer count
	Released struct{ Reason string }       // the work consumer of this attempt was withdrawn
	Cancel   struct{ Reason string }
	Replace  struct{ By, Reason string }
	// NeedFailed takes a dependant with its failed need.
	NeedFailed struct{ Need string }
	Waive      struct{ Need, Reason string }
	Relink     struct{ Need, To, Reason string }
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

// Consumer events, one per row of the consumer table.
type (
	Take   struct{ Row string }
	Report struct {
		Row, Result, Head, Reason string
		Cost                      int64
		Priced                    bool
	}
	Withdraw struct{ Reason string }
)

// Row events, one per row of the row table.
type (
	Seen   struct{}
	Gone   struct{}
	Width  struct{ N int }
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
func (ReadYes) isEvent()     {}
func (ReadFix) isEvent()     {}
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
func (Take) isEvent()        {}
func (Report) isEvent()      {}
func (Withdraw) isEvent()    {}
func (Seen) isEvent()        {}
func (Gone) isEvent()        {}
func (Width) isEvent()       {}
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

// The levels a producer's priority may take, lowest first; read consumers
// occupy a rung between high and critical in the deal order (policy's).
var Levels = []string{"low", "normal", "high", "critical", "blocker"}
