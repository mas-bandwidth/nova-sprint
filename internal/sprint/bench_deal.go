package sprint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// A card's bench (docs/SPEC-SPRINT.md section 5, the deal: a card's BENCH line). A brief
// whose header carries `BENCH: <member>` (cardhdr.KeyValue reads the line, in any case)
// names the member that has what the card needs: a tool only that member's machine holds.
// Such a card is dealt only to that member, or to the members a comma-separated line
// names, by every placement of a work card (the deal, a redeal, an escalation, a rework,
// a member going down and the level); while no member of its bench is up (down, or held)
// it waits in ready and the no-stall rule says why (held.go), so a card that would fail
// on a missing tool is never dealt to a member that has not got it. A BENCH naming no
// fleet member is refused at add, with the members there are. A card with no BENCH line
// is dealt round the fleet as every card before it was.

// FieldBench is a primary's bench as its brief's BENCH line names it: the members' names,
// comma-separated, written by add and brief with the brief; absent on a card with none.
const FieldBench = "bench"

// BenchOfBrief is the bench a brief gives its card (FieldBench), read from its header
// block (the `key: value` lines under line 1, as cardhdr.ReadWho reads WHO): nil and ""
// when the brief names no bench, else the members it names. why is "" or the one line
// naming what is wrong with the line and what to write
// (docs/SPEC-SPRINT.md section 5, a card's BENCH line).
func BenchOfBrief(brief string) (bench []string, why string) {
	_, rest, _ := strings.Cut(brief, "\n")
	for rest != "" {
		var l string
		l, rest, _ = strings.Cut(rest, "\n")
		k, v, ok := cardhdr.KeyValue(l)
		if !ok {
			break
		}
		if !strings.EqualFold(k, "bench") {
			continue
		}
		for _, m := range Split(v) {
			if !ValidID(m) {
				return nil, "BENCH: " + v + " wants a comma-separated list of fleet members, each of letters, digits, _ and -; a card with no BENCH line is dealt round the fleet"
			}
			bench = append(bench, m)
		}
		if len(bench) == 0 {
			return nil, "BENCH: names no member: write `BENCH: <member>[,<member>...]`, or leave the line out and the card is dealt round the fleet"
		}
		return bench, ""
	}
	return nil, ""
}

// Bench is the primary's bench (FieldBench): the members its brief's BENCH line names,
// nil when it names none (docs/SPEC-SPRINT.md section 5, a card's BENCH line).
func Bench(c *Card) []string {
	if c == nil {
		return nil
	}
	return Split(c.F(FieldBench))
}

// BenchKnown is the names of bench that are no fleet member of s (Snapshot.Members): what
// add refuses a brief for, so a card is never admitted waiting for a member the fleet has
// not got (docs/SPEC-SPRINT.md section 5, a card's BENCH line).
func BenchKnown(s *Snapshot, bench []string) []string {
	members := s.Members()
	var out []string
	for _, m := range bench {
		if !contains(members, m) {
			out = append(out, m)
		}
	}
	return out
}

// BenchRefused is add's refusal of a brief whose BENCH line names no fleet member, with
// the members there are and the remedy (docs/SPEC-SPRINT.md section 5, a card's BENCH
// line).
func BenchRefused(s *Snapshot, bench, unknown []string) string {
	members := s.Members()
	have := "the fleet has no member"
	if len(members) > 0 {
		have = "the fleet's members are " + Preview(members, ", ")
	}
	return fmt.Sprintf("BENCH: %s names no fleet member (%s): %s; run: nova-sprint fleet list, and name a member of it, or start the member the card needs with nova-sprint fleet beat %s on its machine",
		strings.Join(bench, ","), Preview(unknown, ", "), have, unknown[0]) // nonempty: BenchRefused is called only with unknown names add found (add.go, BenchKnown)
}

// onlyBench is the members of up the bench names, in up's order: up itself when the bench
// is empty (a card with no BENCH line is dealt round the fleet), and none when no member
// of its bench is up, so the card waits in ready for it (docs/SPEC-SPRINT.md section 5, a
// card's BENCH line).
func onlyBench(up, bench []string) []string {
	if len(bench) == 0 {
		return up
	}
	var out []string
	for _, m := range up {
		if contains(bench, m) {
			out = append(out, m)
		}
	}
	return out
}

// notBench is the members of up the bench does not name: the level's avoid (round.levelTo),
// so a bench card is never moved off the bench its deal put it on; nil when the bench is
// empty (docs/SPEC-SPRINT.md section 5, a card's BENCH line).
func notBench(up, bench []string) []string {
	if len(bench) == 0 {
		return nil
	}
	return without(up, bench)
}

// benchOfWork is the bench of the work card's primary: the one place the bench lives is the
// primary its brief carries (docs/SPEC-SPRINT.md section 5, a card's BENCH line).
func benchOfWork(s *Snapshot, wc *Card) []string {
	if s.Work == nil || wc == nil {
		return nil
	}
	return Bench(s.Work.Placed(wc.F("primary")))
}

// benchWaits is why a card whose bench has no member up waits in ready: no placement deals
// it to another member (docs/SPEC-SPRINT.md section 5, a card's BENCH line).
func benchWaits(bench []string) string {
	return "waits for its bench " + Preview(bench, ", ") + ": no member of it is up, and a card whose brief says BENCH: is dealt only to the members it names"
}

// benchRefusal is the deal's refusal of a card whose bench has no member up: its non-empty
// output is benchWaits with the remedy; an empty bench (a card with no BENCH line that
// benchWaits cannot name) still gets a refusal, never an index of bench (docs/SPEC-SPRINT.md
// section 5, a card's BENCH line).
func benchRefusal(bench []string) string {
	if len(bench) > 0 {
		return benchWaits(bench) + "; run: nova-sprint fleet beat " + bench[0] + " on its machine, or release its hold with nova-sprint fleet up " + bench[0]
	}
	return "no bench member up: every member that beats is quiet, draining or held; the tick deals it when one is up (nova-sprint where shows the fleet)"
}

// benchRoom is the refusal of a card whose bench is up and every member of it at its room
// (DealAhead times its width, width.go): the tick deals it when one has room
// (docs/SPEC-SPRINT.md section 5, a card's BENCH line).
func benchRoom(bench []string) string {
	return "every member of its bench " + Preview(bench, ", ") + " is at its room (DealAhead times its width): the tick deals it when one has room"
}

// benchDrop is a member of a bench the order dropped (benchOrder), with why: it
// starts no card (no room: its word), or it is overloaded.
type benchDrop struct {
	Member string
	Why    string
}

// benchOrder is the order the deal deals a card whose brief names a bench
// (docs/SPEC-SPRINT.md section 5, a card's BENCH line; the owner, 2026-10-13:
// "distribute CI and bench jobs across the fleet, vs. one machine"): members is
// the up members of the bench the notQuiet pool left, and the card goes to the
// least-loaded member of it that has room. A member whose fresh beat says it
// starts no card (Snapshot.NoRoom) and an overloaded one (Overloaded) are
// dropped; the rest are sorted by beat load ascending (memberBeatLoad: the load
// cell its presence beat writes on its control card, LoadText), then by work
// cards held ascending (memberLoads), then by name. A card with no BENCH line
// keeps the round order it has today; this is never applied to one.
func benchOrder(s *Snapshot, members []string) []string {
	q := memberLoads(s, members)
	var out []string
	for _, m := range members {
		if s.NoRoom[m] != "" {
			continue
		}
		if _, ol := Overloaded(s, m); ol {
			continue
		}
		out = append(out, m)
	}
	slices.SortStableFunc(out, func(a, b string) int {
		if la, lb := memberBeatLoad(s, a), memberBeatLoad(s, b); la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		if qa, qb := q[a], q[b]; qa != qb {
			return qa - qb
		}
		return strings.Compare(a, b)
	})
	return out
}

// memberBeatLoad is a fleet member's beat load, the percent of all its cores,
// as the load cell on its control card reads it (LoadText, written by the
// presence beat each tick; store/presence.go SyncFleet): the number before the
// percent sign, 0 for no cell, an unreadable cell or one no fresh beat wrote.
// The bench order (benchOrder) sorts by it, least loaded first.
func memberBeatLoad(s *Snapshot, m string) float64 {
	cell := ""
	if c := s.MemberCtl(m); c != nil {
		cell = c.F(Load)
	}
	pct, _, _ := strings.Cut(cell, "%")
	load, err := strconv.ParseFloat(strings.TrimSpace(pct), 64)
	if err != nil {
		return 0
	}
	return load
}

// benchDropped is each member of members the bench order drops (benchOrder), with
// its reason: no room (its word), or overloaded.
func benchDropped(s *Snapshot, members []string) []benchDrop {
	var out []benchDrop
	for _, m := range members {
		switch {
		case s.NoRoom[m] != "":
			out = append(out, benchDrop{m, "no room: " + s.NoRoom[m]})
		default:
			if _, ol := Overloaded(s, m); ol {
				out = append(out, benchDrop{m, "overloaded"})
			}
		}
	}
	return out
}

// benchNoRoom is the refusal of a card whose bench is up and every member of it
// the order dropped (benchOrder) for room or overload: the tick deals it when one
// has room, and it is never dealt off its bench.
func benchNoRoom(bench []string, dropped []benchDrop) string {
	var who []string
	for _, d := range dropped {
		who = append(who, d.Member+" ("+d.Why+")")
	}
	return "every member of its bench " + Preview(bench, ", ") + " starts no card (" + strings.Join(who, "; ") + "): the tick deals it when one has room"
}

// benchPool is the pool a card whose brief names a bench is dealt from, and the
// refusal when the pool is empty: the up members of its bench the notQuiet pool
// left (up, the quiet and no-room members already out of it), ordered by the
// bench order (benchOrder). A bench whose every member is up but starts no card
// (its no-room word, or overloaded) refuses with benchNoRoom, naming each member
// and its reason, never "waits for its bench"; a bench with no member up refuses
// with benchRefusal. upAll is the members up before the quiet filter: the dropped
// pool, so a member the quiet held back is named too. A card with no BENCH line
// keeps the round pool it has today (onlyBench, never ordered), and an empty pool
// of it refuses with noPoolWhy (steps_work.go, the deal).
func benchPool(s *Snapshot, up, upAll, bench []string) (members []string, why string) {
	members = onlyBench(up, bench)
	if len(bench) == 0 {
		return members, noPoolWhy
	}
	if len(members) == 0 {
		if dropped := benchDropped(s, onlyBench(upAll, bench)); len(dropped) > 0 {
			return nil, benchNoRoom(bench, dropped)
		}
		return nil, benchRefusal(bench)
	}
	ordered := benchOrder(s, members)
	if len(ordered) == 0 {
		return nil, benchNoRoom(bench, benchDropped(s, members))
	}
	return ordered, ""
}

// benchNext is the member a card the deal places on the fleet goes to: for a card
// with a bench, the first of ms -- the launchers of the ordered pool -- that holds
// fewer cards (q) than a lane (lanes, its width), else fewer than its room (widths,
// DealAhead times its width), in the bench order's order, no wrap from the round
// index (round.nextFree's two scans without the wrap, firstFreeOrdered then
// firstOpenOrdered). A card with no BENCH line keeps the round's wrap (rr.nextFree).
func benchNext(bench, ms []string, rr *round, q, lanes, widths map[string]int) string {
	if len(bench) == 0 {
		return rr.nextFree(ms, q, lanes, widths, "")
	}
	if m := firstFreeOrdered(ms, q, lanes); m != "" {
		return m
	}
	return firstOpenOrdered(ms, q, widths)
}

// firstFreeOrdered is the first of ms holding fewer cards than a lane (its
// width), in ms order: the bench order's first with a free lane, as nextLane
// finds it without wrapping from the round index.
func firstFreeOrdered(ms []string, q, lanes map[string]int) string {
	for _, m := range ms {
		if q[m] < lanes[m] {
			return m
		}
	}
	return ""
}

// firstOpenOrdered is the first of ms holding fewer cards than its room
// (DealAhead times its width), in ms order: nextFree's second scan over the
// bench order.
func firstOpenOrdered(ms []string, q, room map[string]int) string {
	for _, m := range ms {
		if q[m] < room[m] {
			return m
		}
	}
	return ""
}
