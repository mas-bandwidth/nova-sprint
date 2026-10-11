package sprint

import (
	"slices"
	"sort"
)

// The tick's automatic retirement of a fleet row whose machine record is GONE
// from the inventory (nova-config), the solution the owner asked for on
// 2026-10-11: "what you need then is some process in the tick that removes old
// entries that are no longer in nova-config." Fleet sync adds and resizes; a
// rename (hetzner -> hetzner1) used to leave the old row on the table forever,
// held, because nothing ever removed a row. This part reads the same machine
// records fleet sync reads (no new store) and, every tick, makes the moves the
// sync's removal already makes for a member with no machine row (fleet_sync.go):
// a row with no record and no card stays on leaves the fleet, its reader
// reader-<old> is retired, its stats stay queryable under the old name, and one
// HAPPENED note records it; a row with no record that still holds cards is held
// and its cards are dealt away (no new deals), retired on the first tick its set
// is empty. The record is GONE from the inventory: a machine whose record exists
// but is offline stays down, never retired (the owner, 2026-10-10: down is not
// an error when actually down).
//
// A config read that fails retires nothing: never act on a missing read. The
// tick's binding reads the inventory and leaves Machines nil (or empty) when
// the read fails or holds no row, and this part does nothing then.
//
// The model is tla/RetireAbsent.tla: RetireAbsent is the tick's action,
// FleetRowsMatchConfig is the invariant that no up/held/down row outlives its
// empty set once its record is absent, and LiveRets rows a row retired.

// PartRetireAbsent is the name of the tick's retirement part (TickRetireAbsent).
const PartRetireAbsent = "retire absent"

// AbsentMembers is every fleet member whose machine record the inventory no
// longer names, in name order: the rows the tick's retire part acts on. A
// member the inventory still names is never here, however its record reads: a
// record that exists but is offline keeps its row down.
func AbsentMembers(s *Snapshot, machines []string) []string {
	var out []string
	for _, m := range s.Members() {
		if !slices.Contains(machines, m) {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// retireAbsentPlan is the tick's retirement of every fleet row whose machine
// record is absent from machines: one member going down plan per row
// (downPlan), held by the sync with Remove set, so its ready and working cards
// are dealt round the members that stay up and, once no card stays on it, its
// control card leaves the table and one HAPPENED note records it. A row that
// keeps cards (withdrawn, no room) stays held and is retired on the first tick
// its set is empty. Nothing here adds a member, sets a width or releases a
// hold: only a record that is GONE retires.
func retireAbsentPlan(s *Snapshot, machines []string, who string, rr *round, moves roundMoves) Plan {
	var p Plan
	absent := map[string]bool{}
	for _, m := range AbsentMembers(s, machines) {
		if s.MemberCtl(m) != nil {
			absent[m] = true
		}
	}
	if len(absent) == 0 {
		return p
	}
	var stay []string
	for _, m := range s.UpMembers() {
		if !absent[m] {
			stay = append(stay, m)
		}
	}
	q, widths := memberLoads(s, stay), memberWidths(s, stay)
	names := make([]string, 0, len(absent))
	for m := range absent {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, m := range names {
		hold := FleetReq{Op: "hold", Member: m, Who: who, Why: "no machine row in the inventory", HeldBy: HeldBySync, Remove: true}
		hp := downPlan(s, hold, stay, rr, moves, q, widths)
		p.Units = append(p.Units, hp.Units...)
		p.Refused = append(p.Refused, hp.Refused...)
		p.Notes = append(p.Notes, hp.Notes...)
	}
	return p
}

// TickRetireAbsent is the tick's retirement part (PartRetireAbsent): every
// fleet row whose machine record the inventory no longer names leaves the
// fleet, the moves fleet sync's removal makes, in the tick's own update
// (retireAbsentPlan). r.Machines is the machine records the tick's binding read
// from the inventory; nil or empty (a failed or empty read) retires nothing.
func TickRetireAbsent(s *Snapshot, r TickReq) (Plan, int) {
	if len(r.Machines) == 0 {
		return Plan{}, 0
	}
	if len(AbsentMembers(s, r.Machines)) == 0 {
		return Plan{}, 0
	}
	rr := dealRoundWith(s)
	moves := roundMoves{}
	p := Lawful(retireAbsentPlan(s, r.Machines, r.who(), rr, moves))
	roundWrites(&p, rr, moves)
	return p, 0
}
