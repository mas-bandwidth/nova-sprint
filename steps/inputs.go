package steps

import (
	"encoding/json"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Inputs is everything a tick reads besides the view: the clock value and what
// layer 6 gathered since the last tick. Nothing else is read (TestLintPure).
type Inputs struct {
	Now      time.Time
	Inbox    []InboxItem          // in arrival order: the last tick's Carry first
	Inflight []EdgeRequest        // edge requests dispatched and not yet answered by a consumed result
	Heads    map[string]string    // stream -> the head of its branch: the base a Start records
	Seen     map[string]time.Time // row -> when it was last seen on the bus (layer 6 keeps it)
	Refused  []RefusedOp          // the last tick's refused ops, from their receipts (OpGroups)
	Roster   roster.Roster        // nova-config: each row's kind, width and tiers, by name
}

// InboxItem is one thing that arrived for the tick: a row's report or take
// (Kind report, take; when layer 6 routes them here rather than applying them
// as the row's own op), or an edge result (Kind land, stop) answering a
// request in flight. ID is its result id: the carry and the acks name it.
type InboxItem struct {
	ID   string
	Kind string // report, take, land, stop

	Row, Session, Consumer string // a report or take: who, from which session, of what
	Result, Head, Reason   string // a report
	Cost                   int64
	Priced                 bool

	Request string   // an edge result: the request it answers
	Outcome string   // land: landed, red, fault, conflict, base-red, base-green; stop: ack
	Landed  []string // land: the heads that landed (a bisected batch lands a part)
	Blamed  string   // red: the head the bisection blamed
	Base    string   // landed: the base the batch was merged onto (the request's); base-red, base-green: the base
	Tip     string   // landed: the merged tip CI passed, now the base's tip; a landed result without it is refused
}

// EdgeRequest is work for layer 4: a landing (Kind land: merge Heads of
// Cards onto Base and gate them, one CI) or a runner stop (Kind stop: the
// Row's run of Consumer, after a preemption). After names the op whose
// acceptance it waits for ("" waits for none): a stop goes out only with
// its preemption (EdgeAfterAccepted). ID is stable across ticks, so a
// restart reissues the same request and a result is matched by id.
type EdgeRequest struct {
	ID, Kind, Stream string
	Cards, Heads     []string
	Base             string
	Row, Consumer    string
	After            string
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
}

// RowAnswer is what a row is told about its report or take: an ack from the
// record ("applied by seq N") for a repeat, or a refusal naming the fix. An
// item whose group was applied gets no answer here: its receipt is the op's
// (layer 6 tells the row from it).
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
