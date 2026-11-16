package steps

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// RosterRow is one row's facts as the tick caches them; Kind "" is unlisted.
type RosterRow struct {
	Kind      string
	Width     int
	Tiers     []string
	Tags      []string
	Resources map[string]int
	Held      string
}

var defaultWorkNeeds = card.Needs{Amounts: map[string]int{"cpu": 1, "memory": 1}}

func rosterVocabulary(ro roster.Roster) (tags, resources map[string]bool) {
	if len(ro.Workers) == 0 {
		return nil, nil
	}
	tags, resources = map[string]bool{}, map[string]bool{}
	for _, w := range ro.Workers {
		for _, t := range w.Tags {
			tags[t] = true
		}
		for r := range w.Resources {
			resources[r] = true
		}
	}
	return tags, resources
}

// rowOf is the roster's facts for row name, read once per tick; Kind "" is
// unlisted.
func (t *tick) rowOf(name string) RosterRow {
	if r, ok := t.roster[name]; ok {
		return r
	}
	r := RosterRow{}
	if w, ok := t.in.Roster.Workers[name]; ok {
		r = RosterRow{
			Kind:      roster.KindWorker,
			Tiers:     slices.Clone(w.Tiers),
			Tags:      slices.Clone(w.Tags),
			Resources: maps.Clone(w.Resources),
			Held:      w.Held,
		}
	} else if k, ok := t.in.Roster.Kin[name]; ok {
		r = RosterRow{Kind: roster.KindKin, Width: k.Width, Tiers: slices.Clone(k.Tiers)}
	}
	t.roster[name] = r
	return r
}

// listed says whether row name is in the roster; one not listed is never
// dealt to, and a step opens a roster judgement for it.
func (t *tick) listed(name string) bool { return t.rowOf(name).Kind != "" }

// kind is row name's kind from the roster (worker or kin), "" when unlisted.
func (t *tick) kind(name string) string { return t.rowOf(name).Kind }

// width is row name's width from the roster (0 for workers or unlisted).
func (t *tick) width(name string) int { return t.rowOf(name).Width }

// tiers is the tiers row name serves, from the roster; none when unlisted.
func (t *tick) tiers(name string) []string { return t.rowOf(name).Tiers }

// rowFitsMax reports whether row name can ever fit needs by static capacity
// and tags (benches check FitsMax; kin rows choose a machine themselves).
func (t *tick) rowFitsMax(name string, needs card.Needs) bool {
	rr := t.rowOf(name)
	if rr.Kind == roster.KindWorker {
		return t.in.Roster.Workers[name].FitsMax(needs)
	}
	return rr.Kind == roster.KindKin
}

// consumerNeeds resolves the resource and tag needs of a consumer of kind
// ("work" or "read") with brief. Reads always use DefaultReadNeeds.
func (t *tick) consumerNeeds(kind, brief string) card.Needs {
	if kind == "read" {
		return card.DefaultReadNeeds()
	}
	if brief == "" || !strings.Contains(brief[:min(len(brief), 4096)], card.ResourcesField) {
		return defaultWorkNeeds
	}
	return card.ResolveNeeds("work", brief, t.knownTags, t.knownResources)
}

// consumerNeedsByID returns the resource needs of consumer id in the view
// (falling back to DefaultReadNeeds for unknown cross-epoch consumer ids).
func (t *tick) consumerNeedsByID(id string) card.Needs {
	if c, ok := t.v.Cards[id]; ok {
		brief := c.Brief
		if brief == "" && c.Of != "" {
			brief = t.v.Cards[c.Of].Brief
		}
		return t.consumerNeeds(c.Kind, brief)
	}
	return card.DefaultReadNeeds()
}

// canonicalConsumerID strips a matching "@<epoch>" suffix from a SprintConsumers
// entry so "<id>@<epoch>" deduplicates with "<id>" in liveOnRow.
func canonicalConsumerID(raw string, epoch int) string {
	raw = strings.TrimSpace(raw)
	base, ep, ok := strings.Cut(raw, "@")
	if !ok {
		return base
	}
	if n, err := strconv.Atoi(ep); err == nil && n == epoch {
		return base
	}
	return raw
}

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
// from the roster.
func (t *tick) rowRecord(r store.Row) card.Row {
	rr := t.rowOf(r.Name)
	return card.RowFromStore(r, rr.Width, rr.Tiers)
}

// RosterGroup is the group key of the judgement that row name is not in
// nova-config: one per row, and the steps' alone (policy opens none).
func RosterGroup(name string) string { return "roster:" + name }

// RetiredReason is the reason a retired row keeps.
const RetiredReason = "retired: nova-config no longer names it"

// unlisted is every store row the roster lacks.
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

// Retirable says whether store row r is retired on roster ro.
func Retirable(ro roster.Roster, r store.Row) bool {
	return !ro.Empty() && !ro.Names(r.Name) && r.Role != "coordinator"
}

// retire is the tick's retire of row n: its hand returned and the row
// retired in one op (retireMoves).
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
// return, then, when that is the whole hand (whole), the row's Retire.
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
