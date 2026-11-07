package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// The roster is nova-config as the steps consume it (package roster): the one
// true source of the fleet and the friends, nothing of
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

// unlisted opens the roster judgement for every store row the roster lacks;
// such a row is dealt nothing.
func (t *tick) unlisted() {
	for _, n := range t.rowNames {
		if !t.listed(n) {
			t.judge("roster", n, "row "+n+" is not in nova-config; nothing is dealt to it until nova-config lists it")
		}
	}
}
