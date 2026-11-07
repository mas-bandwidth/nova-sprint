package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Roster is nova-config as the steps consume it: the one true source of the
// fleet and the friends (Glenn, 2026-11-06), nothing of which is copied into
// the store. A store row is a name, up or down, a session, held and its
// reason, flaps and a gen; its kind, width and the tiers it serves are read
// here by name. The interface has the names of package roster (store round
// 22, branch v2-store-fix22), which replaces it when that lands: its Roster
// has this Row method.
type Roster interface {
	Row(name string) (kind string, width int, tiers []string, ok bool)
}

// RosterRow is one row's facts for a test roster.
type RosterRow struct {
	Kind  string
	Width int
	Tiers []string
}

// FakeRoster is a Roster from a map, for tests.
type FakeRoster map[string]RosterRow

// Row is the roster's facts for name.
func (f FakeRoster) Row(name string) (string, int, []string, bool) {
	r, ok := f[name]
	return r.Kind, r.Width, slices.Clone(r.Tiers), ok
}

// rowOf is the roster's facts for row name, read once per tick; Kind "" is
// unlisted.
func (t *tick) rowOf(name string) RosterRow {
	if r, ok := t.roster[name]; ok {
		return r
	}
	r := RosterRow{}
	if t.in.Roster != nil {
		if kind, w, ts, ok := t.in.Roster.Row(name); ok {
			r = RosterRow{Kind: kind, Width: w, Tiers: ts}
			if r.Kind == "" {
				r.Kind = "worker"
			}
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
// from the roster (the machine's deal guards take them from there; until
// card round 8 lands they travel in the Row record).
func (t *tick) rowRecord(r store.Row) card.Row {
	cr := card.RowFromStore(r)
	rr := t.rowOf(r.Name)
	cr.Width, cr.Tiers = rr.Width, rr.Tiers
	return cr
}

// unlisted opens the roster judgement for every store row the roster lacks;
// such a row is dealt nothing.
func (t *tick) unlisted() {
	for _, n := range t.rowNames {
		if !t.listed(n) {
			t.judge("roster", n, "row "+n+" is not in nova-config; nothing is dealt to it until nova-config lists it")
		}
	}
}
