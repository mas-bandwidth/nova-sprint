package sprint

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Resolved and unresolved needs (v1.2.17, a-missing-or-blocked-need-is-found-at-check-and-while-
// running; the owner, 2026-10-13: "we need something mechanical that catches missing dependencies
// at check time and while the sprint runs." and "This needs to stop happening. A missing card
// stalls the sprint."). Epoch 20: a card needed one that hit its attempt bound overnight under an
// open bound judgment no one decided, and every check passed, since the need was on the table.
//
// A need is RESOLVED when it names, for this epoch, a card that is landed, or placed (waiting,
// ready, working, review, merging) or a sentinel and not held, in a stream whose control card
// exists. Every other need is UNRESOLVED, with exactly one reason (docs/SPEC-SPRINT.md sections 9
// and 11; tla/NeedsBlocked.tla AdmittedNeedsResolved, StartOnlyResolved).

// The reasons a need is unresolved.
const (
	UnresolvedAbsent        = "absent"         // no record in this epoch
	UnresolvedDropped       = "dropped"        // a kept record off the table
	UnresolvedUnknownStream = "unknown stream" // its stream has no control card in this epoch
	UnresolvedHeld          = "held"           // an open judgment of a holding kind names it
	UnresolvedCycle         = "cycle"          // the need closes a cycle (needsCycle)
)

// NeedHoldingKinds is the judgments that hold the card they name until someone decides: a
// bound, a brief-wrong bound and a repeated harness fault. None of them can be waited out
// (TickDecisions): a rework or a drop decides it.
var NeedHoldingKinds = []string{NBound, NBriefWrong, NHarnessFault}

// UnresolvedNeed is one need not resolved: the need, its one reason, the words that say it
// (add's refusal and check's finding), and the verbs that decide it.
type UnresolvedNeed struct {
	Need   string   `json:"need"`
	Reason string   `json:"reason"`
	Words  string   `json:"words"`
	Decide []string `json:"decide,omitempty"`
}

// needHolder is the open judgment of a holding kind that names id, the oldest first; nil when
// none does (cardsHeldBy: its primaries, else its card, else its subject).
func needHolder(s *Snapshot, id string) *Note {
	var held *Note
	for i := range s.Open {
		n := s.Open[i].Note
		if !contains(NeedHoldingKinds, n.Type) || !contains(cardsHeldBy(n), id) {
			continue
		}
		if held == nil || n.At.Before(held.At) {
			cp := n
			held = &cp
		}
	}
	return held
}

// needDecideVerbs is the verbs that decide the judgment n holding card: from its own
// decisions (boundDecisions), a wait aside, since a holding judgment is not waited out.
func needDecideVerbs(n *Note, card string) []string {
	decisions := n.Decisions
	if len(decisions) == 0 {
		decisions = []string{"rework with a fix", "drop"}
	}
	var out []string
	for _, d := range decisions {
		switch d {
		case "rework with a fix":
			out = append(out, "nova-sprint rework "+card+" --fix <text>")
		case ReworkOnAHigherTier:
			out = append(out, "nova-sprint rework "+card+" --fix <text> --tier <higher>")
		case "drop":
			out = append(out, "nova-sprint drop "+card+" --reason <text> (then relink or drop what needs it)")
		case "wait":
		default:
			out = append(out, d)
		}
	}
	return out
}

// noteName is how a judgment is named to a person: its alias (j<n>), else its id.
func noteName(n *Note) string {
	if n.Alias != "" {
		return n.Alias
	}
	return n.ID
}

// UnresolvedNeeds is the needs of card id that are not resolved, each with its one reason, in
// the order named: needs named by adding (cards of the same add) are resolved by the add. A
// waived need is no need. The cycle reason is the stored table's: a card not yet on the table
// (an add) is checked for cycles by the add's own graph (Add, newNeedGraph).
//
// tla/NeedsBlocked.tla AdmittedNeedsResolved (Admit's guard, Resolved);
// tla/SoftDependency.tla SoftNeverBlocksDeal (DealEligible: only hard needs gate ready and the deal; soft dependencies never do).
func UnresolvedNeeds(s *Snapshot, id string, needs []string, adding map[string]bool) []UnresolvedNeed {
	self := s.Work.Card(id)
	waived := Split(self.F("waived"))
	var out []UnresolvedNeed
	seen := map[string]bool{}
	for _, n := range needs {
		if n == "" || seen[n] || adding[n] || contains(waived, n) {
			continue
		}
		seen[n] = true
		c := s.Work.Card(n)
		switch {
		case n == id:
			out = append(out, UnresolvedNeed{Need: n, Reason: UnresolvedCycle, Words: n + " is the card itself",
				Decide: []string{"nova-sprint needs set " + id + " --needs <the needs without " + n + ">"}})
		case c == nil:
			out = append(out, UnresolvedNeed{Need: n, Reason: UnresolvedAbsent, Words: n + " is no primary on the table or in this add",
				Decide: []string{"add " + n + ", or remove the need if " + n + " landed before this epoch: nova-sprint needs set " + id + " --needs <the needs without " + n + ">"}})
		case !c.Placed() && !IsSentinel(c):
			out = append(out, UnresolvedNeed{Need: n, Reason: UnresolvedDropped, Words: n + ", which was " + orDash(c.F("outcome")),
				Decide: []string{"nova-sprint needs set " + id + " --needs <the needs without " + n + ">", "nova-sprint relink " + n + " <its twin>"}})
		case c.Col == Landed:
		case s.Merge != nil && c.Row != "" && s.StreamCtl(c.Row) == nil:
			out = append(out, UnresolvedNeed{Need: n, Reason: UnresolvedUnknownStream, Words: n + "'s stream " + c.Row + " has no control card in this epoch",
				Decide: []string{"nova-sprint move " + n + " --stream <a stream of this epoch>", "nova-sprint needs set " + id + " --needs <the needs without " + n + ">"}})
		default:
			if h := needHolder(s, n); h != nil {
				age := s.Now.Sub(h.At).Round(time.Minute)
				out = append(out, UnresolvedNeed{Need: n, Reason: UnresolvedHeld,
					Words:  fmt.Sprintf("%s is held by judgment %s (%s, open %s) until it is decided", n, noteName(h), h.Type, age),
					Decide: needDecideVerbs(h, n)})
			}
		}
	}
	if self.Placed() && len(out) == 0 {
		var live []string
		for _, n := range needs {
			if n != "" && !contains(waived, n) && !adding[n] {
				live = append(live, n)
			}
		}
		for _, n := range live {
			if why := needsCycle(s, id, []string{n}); why != "" {
				loop := strings.TrimSuffix(strings.TrimPrefix(why, "the needs would make a cycle: "), "; nothing was changed")
				out = append(out, UnresolvedNeed{Need: n, Reason: UnresolvedCycle, Words: "the need closes a cycle: " + loop,
					Decide: []string{"nova-sprint needs set " + id + " --needs <the needs that break the cycle>"}})
				break
			}
		}
	}
	return out
}

// TableUnresolved is every unlanded card's unresolved needs, by card id: what check cards names
// (kind needs) and start refuses on, live.
//
// tla/NeedsBlocked.tla Unresolved, StartOnlyResolved
func TableUnresolved(s *Snapshot) map[string][]UnresolvedNeed {
	out := map[string][]UnresolvedNeed{}
	for _, c := range s.Work.Cards() {
		if !c.Placed() || c.Col == Landed {
			continue
		}
		if u := UnresolvedNeeds(s, c.ID, Split(c.F("needs")), nil); len(u) > 0 {
			out[c.ID] = u
		}
	}
	return out
}

// UnresolvedIDs is the cards of TableUnresolved in id order.
func UnresolvedIDs(m map[string][]UnresolvedNeed) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// NeedsRefusal is add's words for a card's unresolved needs: the needs that name no card
// together, as add said them before this card, then each other need with its reason.
func NeedsRefusal(us []UnresolvedNeed) string {
	var absent []string
	var why []string
	for _, u := range us {
		if u.Reason == UnresolvedAbsent {
			absent = append(absent, u.Need)
		}
	}
	if len(absent) > 0 {
		why = append(why, "needs "+strings.Join(absent, ",")+", which is no primary on the table or in this add")
	}
	for _, u := range us {
		switch u.Reason {
		case UnresolvedAbsent:
		case UnresolvedDropped:
			why = append(why, "needs "+u.Words)
		default:
			w := "needs " + u.Need + " (" + u.Reason + "): " + u.Words
			if len(u.Decide) > 0 {
				w += "; decide it: " + strings.Join(u.Decide, " or ")
			}
			why = append(why, w)
		}
	}
	return strings.Join(why, "; ")
}
