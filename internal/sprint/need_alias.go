package sprint

import (
	"fmt"
	"strings"
)

// A dropped card that has a successor is a permanent alias (docs/SPEC-SPRINT.md
// section 2; the owner, 2026-10-11: "fix it so this class of problems becomes
// impossible"). When a card is dropped and replaced by a successor (relink old
// new, replace, or drop --replaced-by new), the old id becomes an alias of the
// new one, recorded once on the old card's kept record as FieldReplacedBy. From
// then on no path can bring the old id back as a live need: every reader of needs
// resolves the stored name through the alias to its successor, so a brief edit,
// an add, or a twin whose text names the old id stores the need as the successor
// instead, and a need on the old id never blocks a card once the successor lands.
// The record is keyed by epoch: the old card's record lives in the table of the
// epoch it was dropped in, and a clear starts the next epoch with no record.
const (
	FieldReplacedBy = "replaced_by"
)

// ReplaceRecord is a dropped card's successor as recorded: the new id, the stamp
// the alias was made, and who made it.
type ReplaceRecord struct {
	New   string
	Stamp string
	Who   string
}

// replacedByOf reads a card's alias record (FieldReplacedBy): "<new> <stamp> by
// <who>", false when the card records no successor.
func replacedByOf(c *Card) (ReplaceRecord, bool) {
	v := c.F(FieldReplacedBy)
	if v == "" {
		return ReplaceRecord{}, false
	}
	f := strings.Fields(v)
	r := ReplaceRecord{New: f[0]}
	if len(f) > 1 {
		r.Stamp = f[1]
	}
	if i := strings.Index(v, " by "); i >= 0 {
		r.Who = strings.TrimSpace(v[i+4:])
	}
	return r, r.New != ""
}

// AliasOf resolves a need's id through the alias map: the successor a dropped
// card was replaced by, following a chain of aliases to the first id that is not
// itself an alias. It returns id unchanged when the card has no record, no
// successor, or the chain loops.
func AliasOf(s *Snapshot, id string) string {
	seen := map[string]bool{}
	for {
		c := s.Work.Card(id)
		if c == nil {
			return id
		}
		r, ok := replacedByOf(c)
		if !ok || r.New == id || seen[id] {
			return id
		}
		seen[id] = true
		id = r.New
	}
}

// ResolveNeedAliases maps each stored need through the alias map, each resolved
// id once in the order of needs. notes is one NOTE line per need whose name is
// now its successor ("need old is now new (relinked <time> by <actor>)").
func ResolveNeedAliases(s *Snapshot, needs []string) (out, notes []string) {
	for _, n := range needs {
		t := AliasOf(s, n)
		if t != n {
			r, _ := replacedByOf(s.Work.Card(n))
			notes = append(notes, fmt.Sprintf("need %s is now %s (relinked %s by %s)", n, t, orDash(r.Stamp), orDash(r.Who)))
		}
		if !contains(out, t) {
			out = append(out, t)
		}
	}
	return out, notes
}

// aliasUnits is the units that record the alias old => nw on each old card's kept
// record, once: it is written when a drop and a relink are made, so the successor
// outlives the drop and every reader of a need on the old id finds the new one.
func aliasUnits(s *Snapshot, olds []string, nw, who string) []Unit {
	var out []Unit
	for _, old := range olds {
		c := s.Work.Card(old)
		if c == nil || old == nw {
			continue
		}
		if r, ok := replacedByOf(c); ok && r.New == nw {
			continue
		}
		set := map[string]string{FieldReplacedBy: fmt.Sprintf("%s %s by %s", nw, stamp(s.Now), orDash(who))}
		out = append(out, Unit{Key: old, Stream: c.Row, Changes: []Change{change(Work, setEntry(c, set))},
			Moved: old + " -> replaces by " + nw})
	}
	return out
}
