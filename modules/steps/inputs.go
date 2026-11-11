package steps

import (
	"encoding/json"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Inputs is everything a tick reads besides the view: the clock value and what
// layer 6 gathered since the last tick. Nothing else is read (TestLintPure).
type Inputs struct {
	Now      time.Time
	Inbox    []InboxItem          // in arrival order: the last tick's Carry first
	Inflight []EdgeRequest        // edge requests dispatched and not yet answered by a consumed result
	Heads    map[string]string    // stream -> the head of its branch: the base a Start records
	Seen     map[string]time.Time // row -> when it was last seen on the bus (layer 6 keeps it)
	// Alive is kin row -> its last evidence of life: a pong to one of the
	// run's challenges, or when layer 6 first saw its session. A kin row's
	// bus hook is not its model, so its silence is read here, not in Seen.
	Alive   map[string]time.Time
	Refused []RefusedOp   // the last tick's refused ops, from their receipts (OpGroups)
	Roster  roster.Roster // nova-config: each row's kind, width and tiers, by name
	// Unadmitted is each producer that failed add's admission (check's
	// re-proof) when layer 6 last proved its stream, at the base it has now,
	// and the refusal with its fix: it is never dealt (admission.go).
	Unadmitted map[string]string
	// Bases is each watched repository and base's last completed CI read, by
	// BaseAbout: red or green at a tip. One with no completed read has no
	// entry, and neither opens nor clears a base-red judgement (basered.go).
	Bases map[string]BaseCI
}

// InboxItem is one thing that arrived for the tick: a row's report or take
// (Kind report, take; when layer 6 routes them here rather than applying them
// as the row's own op), or an edge result (Kind land, stop) answering a
// request in flight. ID is its result id: the carry and the acks name it.
type InboxItem struct {
	ID   string
	Kind string // report, take, land, stop

	Row, Session, Consumer string // a report or take: who, from which session, of what
	Result, Head, Reason   string // a report; land red: the failing checks and their URLs
	Cost                   int64  // the report's cost, Priced and Usage as the engine priced them
	Priced                 bool   // (verbs' priceReport): layer 6 routes a report here priced, never
	Usage                  string // raw words, so the tick reads no price sheet

	Request string   // an edge result: the request it answers
	Outcome string   // land: landed, or what kept it from landing (a judgement); stop: ack
	Landed  []string // land: the heads that landed (all of the request's); promote: the merges now on main
	Merge   string   // land: the merge commit base has now
	Main    bool     // land: base is main, so the merge is on main (LANDING.md, "Landed means on main")
}

// EdgeRequest is work for layer 4: a landing (Kind land: Heads of Cards on
// their Branches, one head rung 1, more rung 2; Base the stream's base tip
// as the tick saw it; Wait the seconds a gated sha may have no CI at all,
// policy's), a promotion of the base to main (Kind promote: Cards on base,
// at Heads, with their recorded base Merges, a card's head where none is
// recorded) or a runner stop (Kind stop: the Row's run of Consumer, after a
// preemption). After names the op whose acceptance it waits for ("" waits
// for none): a stop goes out only with its preemption (EdgeAfterAccepted).
// ID is stable across ticks, so a restart reissues the same request and a
// result is matched by id.
type EdgeRequest struct {
	ID, Kind, Stream       string
	Cards, Heads, Branches []string
	Merges                 []string
	Base                   string
	Wait                   int
	Row, Consumer          string
	After                  string
}

// RefusedOp is an op of the last tick that layer 1 refused: its groups (from
// OpGroups of the op's receipt) are packed alone next tick, and a single
// group refused on fresh state is left out with an engine judgement.
type RefusedOp struct {
	ID, Reason string
	Groups     []GroupKey
}

// GroupKey is what a group touched: its cards with the gens it read them at,
// the row it wrote, and the inbox item it answered ("" for none). An op's
// Note carries its groups' keys as JSON.
type GroupKey struct {
	Kind  string         `json:"kind,omitempty"`
	Cards []string       `json:"cards"`
	Gens  map[string]int `json:"gens,omitempty"`
	Row   string         `json:"row,omitempty"`
	Item  string         `json:"item,omitempty"`
	Body  string         `json:"body,omitempty"` // the hash of the group's conditions and writes (refusedAlone)
}

// Plan is one tick's output: Ops in priority order, each packed from up to
// pack groups and fenced on the gens its groups read; Edges for layer 4;
// Answers to rows by result id; and Carry, the inbox items the budget left,
// in arrival order, to head the next tick's inbox.
type Plan struct {
	Ops     []store.Op
	Edges   []EdgeRequest
	Answers []RowAnswer
	Carry   []InboxItem
	Nudges  []Nudge // this tick's nudges: run pushes each to its row, outside the log
}

// RowAnswer is what a row is told about its report or take: an ack from the
// record ("applied at seq N") or a refusal naming the fix.
type RowAnswer struct {
	Result  string // the inbox item's id
	Row     string
	Text    string
	Refused bool
}

// OpGroups is the groups an op of this package carries in its Note; nil for
// an op with none (a judgement op, or an op of another writer).
func OpGroups(op store.Op) []GroupKey {
	var gs []GroupKey
	if op.Note == "" || json.Unmarshal([]byte(op.Note), &gs) != nil {
		return nil
	}
	return gs
}

func noteOf(gs []GroupKey) string {
	b, err := json.Marshal(gs)
	if err != nil {
		return ""
	}
	return string(b)
}
