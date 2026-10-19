package sprint

import "fmt"

// NInventoryEmpty is the tick's judgment on an empty inventory: nova-config's read of the
// machine rows answered and held none. That is a likely-wrong config (an empty or wrong
// database; fleet sync refuses the same read, exit 3), never a fleet of width 0: the
// binding keeps the last applied widths, writes no zero and retires nothing, and the tick
// says so, loudly, once while it holds (the coordinator, 2026-10-18, on
// v17-width-one-definition-and-hard.w4; tla/WidthHard.tla OneSource holds over the last
// applied read).
const NInventoryEmpty = "nova-config inventory is empty"

// inventorySubject is the one subject of the empty-inventory judgment: a non-card subject
// (its colon), so the orphan rule never takes it for a card off the table and retires it
// (OrphanJudgments; check rule 8).
const inventorySubject = "config:inventory"

// TickInventoryEmpty is the tick's empty-inventory judgment, planned in the deadlines part
// on the binding's word (TickReq.InventoryEmpty): one judgment while the read answers with
// no machine row, closed when a read holds rows again. No read (nil: a read that failed,
// or none bound) keeps an open judgment as it stands: no fact is never a clear, as
// TickBaseCI carries a red base the facts say nothing of.
func TickInventoryEmpty(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	var conds []cond
	switch {
	case r.InventoryEmpty == nil:
		carry := func(o Open) {
			conds = append(conds, cond{typ: NInventoryEmpty, primaries: []string{o.Subject()}, what: o.Note.What})
		}
		for _, o := range s.Open {
			if o.Note.Kind == Judgment && o.Note.Type == NInventoryEmpty {
				carry(o)
			}
		}
		for _, o := range s.Acked {
			if !o.Note.Review.IsZero() && o.Note.Type == NInventoryEmpty {
				carry(o)
			}
		}
	case *r.InventoryEmpty:
		conds = append(conds, cond{typ: NInventoryEmpty, primaries: []string{inventorySubject}, decisions: TickDecisions[NInventoryEmpty],
			what: fmt.Sprintf("%s; widths kept: the read of nova-config's machine rows answered with none (an empty or wrong config database?); the members' widths are kept as last applied, no width is written 0 and no member is retired; check nova-config machine list and the config store this server reads (--pg, NOVA_PG_DSN)", NInventoryEmpty)})
	}
	due := notify(&p, s, conds, []string{NInventoryEmpty}, r)
	return p, due
}
