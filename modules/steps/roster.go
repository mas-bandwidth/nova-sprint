package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The roster is nova-config as the steps consume it (package roster): the one
// true source of the fleet and the friends (Glenn, 2026-11-06), nothing of
// which is copied into the store. A store row is a name, up or down, its
// hook (session and role), held and its reason, flaps and a gen; its kind,
// width and the tiers it serves are read here by name (Inputs.Roster).

// RosterRow is one row's facts as the tick caches them; Kind "" is unlisted.
type RosterRow struct {
	Kind  string
	Width int
	Tiers []string
}

// rowOf is the roster's facts for row name, read once per tick; Kind "" is
// unlisted.
func (t *tick) rowOf(name string) RosterRow {
	if r, ok := t.roster[name]; ok {
		return r
	}
	r := RosterRow{}
	if kind, w, ts, ok := t.in.Roster.Row(name); ok {
		r = RosterRow{Kind: kind, Width: w, Tiers: ts}
		if r.Kind == "" {
			r.Kind = roster.KindWorker
		}
	}
	t.roster[name] = r
	return r
}

// listed says whether row name is in the roster; one not listed is never
// dealt to, and a step opens a roster judgement for it.
func (t *tick) listed(name string) bool { return t.rowOf(name).Kind != "" }

// kind is row name's kind from the roster (worker or kin), "" when unlisted.
func (t *tick) kind(name string) string { return t.rowOf(name).Kind }

// width is row name's width from the roster, 0 when unlisted.
func (t *tick) width(name string) int { return t.rowOf(name).Width }

// tiers is the tiers row name serves, from the roster; none when unlisted.
func (t *tick) tiers(name string) []string { return t.rowOf(name).Tiers }

// rank is the highest tier a row lists; "" for none.
func (t *tick) rank(name string) string {
	best := -1
	for _, tier := range t.tiers(name) {
		best = max(best, slices.Index(store.Tiers, tier))
	}
	if best < 0 {
		return ""
	}
	return store.Tiers[best]
}

// rowRecord is a store row as the card machine reads it, its width and tiers
// from the roster (the machine's deal guards take them from there).
func (t *tick) rowRecord(r store.Row) card.Row {
	rr := t.rowOf(r.Name)
	return card.RowFromStore(r, rr.Width, rr.Tiers)
}

// RosterGroup is the group key of the judgement that row name is not in
// nova-config: one per row, and the steps' alone (policy opens none).
func RosterGroup(name string) string { return "roster:" + name }

// RetiredReason is the reason a retired row keeps.
const RetiredReason = "retired: nova-config no longer names it"

// unlisted is every store row the roster lacks. One nova-config does not name
// at all is retired, its hand returned to the queue in the same op, branches
// and heads kept (Glenn, 2026-11-15: "the nova-sprint system and dash
// follows the set of machines defined automatically in nova-config";
// tla/RowAlive.tla Retire): it then shows nowhere until it hooks in again.
// An empty roster retires nothing (Retirable): a read that failed plans no
// tick at all (run RosterUnread), and one that returned nothing is a fault,
// never a fleet with no rows. One nova-config names that is not a roster row
// (a machine that is not a bench, a disabled friend, a refused row) is dealt
// nothing and opens the roster judgement.
func (t *tick) unlisted() {
	for _, n := range t.rowNames {
		switch {
		case t.listed(n):
		case Retirable(t.in.Roster, t.v.Rows[n]):
			t.retire(n)
		default:
			t.judge("roster", n, "row "+n+" is not in nova-config; nothing is dealt to it until nova-config lists it")
		}
	}
}

// Retirable says whether store row r is retired on roster ro: ro is not
// empty, nova-config does not name the row, and it is not hooked as the
// coordinator (tla/RowAlive.tla RetireDue). The coordinator is never
// unhooked by a tick: a sprint never runs without one, so that row keeps the
// roster judgement until it unhooks or nova-config names it. The steps, the
// retire verb and the doctor read it, so all three judge a row the same way.
func Retirable(ro roster.Roster, r store.Row) bool {
	return !ro.Empty() && !ro.Names(r.Name) && r.Role != "coordinator"
}

// retire is the tick's retire of row n: its hand returned and the row
// retired in one op (retireMoves). The store refuses a retire while the row
// holds a live card, so when this tick cannot return the whole hand (a card
// a step claimed, one whose producer is not free, one the card machine will
// not withdraw) it returns what it can and retires the row on a later tick,
// when the hand is empty (tla/RowAlive.tla Retire: RetireDue needs the hand
// empty after the op's own withdraws).
func (t *tick) retire(n string) {
	if !t.unclaimed(n) {
		return
	}
	moves, ids, whole := t.retireMoves(n)
	if !whole {
		for i, m := range moves[:len(ids)] {
			t.group("withdraw", ids[i], m)
		}
		return
	}
	t.group("retire", "", moves...)
}

// retireMoves is the Withdraw of every live consumer of row n this op can
// return, each released with its branch and pushed head kept (ids, in
// order), then, when that is the whole hand (whole), the row's Retire.
func (t *tick) retireMoves(n string) (moves []move, ids []string, whole bool) {
	whole = true
	for _, id := range t.v.Live(n) {
		if !t.unclaimed(id) || !t.free(t.v.Cards[id].Of) {
			whole = false
			continue
		}
		m := t.withdraw(id, card.ReleasedPrefix+card.Returned(n, "retired"))
		if _, r := m.plan(); r != nil {
			whole = false
			continue
		}
		moves, ids = append(moves, m), append(ids, id)
	}
	if whole {
		moves = append(moves, t.rowMove(n, card.Retire{Reason: RetiredReason}))
	}
	return moves, ids, whole
}
