package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The rebalance (docs/SPEC-SPRINT.md section 1, the rebalance; the owner, 2026-10-06: "This
// should be a holistic rebalance, not just across friends, not just across tiers, but BOTH,
// depending on what tiers are enabled per-fleet/friends table." and "When you rebalance,
// always rebalance this way from now on."). The work table's part after the deal: the excess
// of every unit (its ready cards beyond the halves its working cards leave free) goes to a
// unit of either side with a free lane that may take it, friend or member, within the side's
// tier set, own tier first then round the deal's cursor. tla/WhoPreference.tla, Rebalance.
// NoQueuedCardWhileACapableLaneIsFree, the model card tla-whopreference-free-lanes-first-and-taken-stays:
// no unit keeps a queued card while another unit able to take it has a free lane.

// PartRebalance is the work table's part after the deal (TickTables).
const PartRebalance = "rebalance"

// FieldRebalancedFrom is the rows a work card was rebalanced off, comma joined: the
// rebalance never places it on one of them again.
const FieldRebalancedFrom = "rebalanced_from"

// NRebalanced is the happened note of a queued card rebalanced to an idle lane.
const NRebalanced = "a queued card rebalanced to an idle lane"

// rebalanceUnit is a row the rebalance reads: a friend dealable or a member up, its lanes,
// the lanes working (reads at half a slot) and the cards it holds, ready and working.
type rebalanceUnit struct {
	name, row string
	friend    bool
	seat      FriendSeat
	width     int
	working   int
	load      int
}

// idle is the unit's idle lanes: its width less every card it holds.
func (u *rebalanceUnit) idle() int { return u.width - u.load }

// TickRebalance is the rebalance as a part of the tick (Rebalance).
func TickRebalance(s *Snapshot, r TickReq) (Plan, int) {
	seats := r.Friends
	if seats == nil {
		seats = s.Friends
	}
	return Rebalance(s, seats, r.who()), 0
}

// Rebalance moves the excess of every unit (docs/SPEC-SPRINT.md section 1, the rebalance;
// NoQueuedCardWhileACapableLaneIsFree, tla/WhoPreference.tla): a free lane is a half-lane no
// card on its row holds, ready or working (a work card two, a read one: halfLoad), and a
// unit's excess is its ready cards beyond the halves its working cards leave free, taken from
// the end of the order it would start them in. Every unit with excess is a source, friend or
// member, whether or not all its lanes work. A work card (a fix card is kind work: a rework
// attempt carrying the fix field, or a primary of priority fix) dealt and not started (ready
// on its row: a friend has not started it, friendStarted; a member has not taken it) in the
// excess goes to a unit with a free lane that may take it, one move a card, at most the free
// lanes. Read cards use the same excess rule in rebalanceReads. A work card stays when it is
// a sentinel's, of a held stream, a bench card, a hard pin (OnlyFriend), a pin honoured where
// it sits (its WHO names the friend it is on, or a model pin on a member; a WHO: friend card
// on a friend's row goes to no member), run by a lane (laneRunsIt), or its route rests. A
// friend may take it when she is dealable, her tiers and the friends' set hold its tier
// (friendTakes) and it has not left her (friendsLeft); a member up, while the fleet's work is
// on, when the fleet's set holds its tier, a route of the tier serves it (routeOf) and it did
// not refuse it at staging. Never a row it was rebalanced off (FieldRebalancedFrom). The
// target is chosen round the units that may take it from the deal's rolling cursor
// (rebalanceWorkRound, deal_index), moved past each target ((index++) % n): the card's own
// tier first (a member draws on its tier's routes, a friend whose highest tier is the card's),
// then one tier up as overflow, never more; then the cursor, never the most-idle or name
// order. The card moves at its next generation, its attempt kept and nothing carried; to a
// member it draws a route of its tier, to a friend its route comes off.
func Rebalance(s *Snapshot, seats []FriendSeat, who string) Plan {
	var p Plan
	if s == nil || s.Work == nil || s.Fleet == nil {
		return p
	}
	s, _ = s.withRests()
	var units []*rebalanceUnit
	add := func(u *rebalanceUnit) {
		work, reads := rowWorking(s, u.row)
		u.working = halfLoad(work, reads)
		u.load = halfLoad(rowLoad(s, u.row))
		units = append(units, u)
	}
	for _, f := range seats {
		if friendDealable(s, f) {
			g := f
			g.ReadsFirst = 0
			_, width := friendRoom(g)
			add(&rebalanceUnit{name: f.Name, row: FriendRow(f.Name), friend: true, seat: f, width: width})
		}
	}
	if !s.FleetOff() {
		// a member quiet, or whose beat says it starts no card (its disk, or it drains:
		// drain.go), is given nothing (notQuiet); its own ready cards are the level's
		for _, m := range notQuiet(s, s.UpMembers()) {
			add(&rebalanceUnit{name: m, row: m, width: s.Width(m)})
		}
	}
	idle := false
	for _, u := range units {
		idle = idle || rebalanceReadHalfRoom(s, u, 0) > 0
	}
	if !idle {
		return p
	}
	cursor := rebalanceWorkRound(s, units)
	from, work := map[string]*rebalanceUnit{}, map[string]*Card{}
	var prims []*Card
	for _, g := range units {
		ew := excessWork(s, g)
		if ew <= 0 {
			continue // no excess work: its ready cards are its own to start
		}
		ready := readyCardsOf(s, g.row, false)
		if ew < len(ready) {
			ready = ready[len(ready)-ew:] // the excess, from the end of its start order
		}
		for _, wc := range ready {
			pr := s.Work.Placed(wc.F("primary"))
			if wc.F("kind") != "work" || pr == nil || pr.Col != Working || pr.F("work") != wc.ID || IsSentinel(pr) ||
				OnlyFriend(pr) || StreamHeld(s, pr.Row) || len(Bench(pr)) > 0 || laneRunsIt(s, seats, pr, wc) != "" ||
				wc.F(FieldRoute) == RoutePin {
				continue
			}
			if name, ok := FriendCard(pr); ok && g.friend && name == g.name {
				continue // its pin is honoured where it sits
			}
			if g.friend && friendStarted(s, g.seat, wc) {
				continue
			}
			if _, rests := cardRest(s, wc); rests {
				continue // the deal withdraws it (restWithdrawals)
			}
			from[pr.ID], work[pr.ID] = g, wc
			prims = append(prims, pr)
		}
	}
	ri := routeIndexesOf(s) // tla/RouteIndex.tla Deal (the rebalance's routeOf draw)
	declared := map[string]bool{}
	workMoved := map[string]int{}
	for _, pr := range ladderOrder(dealOrder(s, prims)) {
		g, wc := from[pr.ID], work[pr.ID]
		tier := s.DealTier(pr)
		to := rebalanceTo(s, units, g, pr, wc, tier, cursor)
		if to == nil {
			continue
		}
		to.load++
		workMoved[to.row]++
		p.Units = append(p.Units, rebalanceMove(s, &p, declared, ri, g, to, pr, wc, tier, who))
	}
	writeRebalanceRound(&p, cursor)
	// Reads are rebalanced too (docs/SPEC-SPRINT.md section 1, the rebalance): the excess
	// read cards of every unit move to units with a free lane whose reader row serves the
	// read's tier, as many as the free lanes take (a read is half a slot), in one tick.
	p.Units = append(p.Units, rebalanceReads(s, seats, units, workMoved, &p, ri, who)...)
	ri.write(&p)
	return p
}

// rebalanceWorkRound is the work rebalance's rolling cursor: the deal's index (round.go,
// deal_index) over the units that may take a card (friends by name, members by row), moved
// past each target, ((index++) % n), never the tier distance, most-idle, name order.
func rebalanceWorkRound(s *Snapshot, units []*rebalanceUnit) *round {
	names := make([]string, 0, len(units))
	for _, u := range units {
		names = append(names, u.name)
	}
	return tableRound(s.Fleet, PropDealIndex, names)
}

// writeRebalanceRound writes the cursor's new value with the plan, when it moved.
func writeRebalanceRound(p *Plan, r *round) {
	if r == nil || r.count == roundCount(r.order, r.read) {
		return // no target moved the cursor: its place is where it was read
	}
	p.Props = append(p.Props, PropWrite{Table: r.table, Name: r.name, Value: r.value(), Was: r.read, WasAbsent: !r.had})
}

// freeHalves is the unit's free half-slots: twice its width less the halves every card on
// its row holds, ready or working (a work card two, a read one).
func freeHalves(s *Snapshot, u *rebalanceUnit) int {
	ww, wr := rowWorking(s, u.row)
	return max(2*u.width-2*ww-wr, 0)
}

// excessWork is the unit's ready work cards beyond the halves its working cards leave free:
// its ready reads start first (reader priority, half a slot each), then its work. The rework
// and fix kinds are work cards and count with them.
func excessWork(s *Snapshot, u *rebalanceUnit) int {
	ww, wr := rowWorking(s, u.row)
	work, reads := rowLoad(s, u.row)
	free := freeHalves(s, u)
	readsKept := min(reads-wr, free)
	halves := max(free-readsKept, 0)
	return work - ww - min(work-ww, halves/2)
}

// excessReads is the unit's ready read cards beyond the halves its working cards leave free:
// the first free halves' reads it starts itself, the rest are excess.
func excessReads(s *Snapshot, u *rebalanceUnit) int {
	_, wr := rowWorking(s, u.row)
	_, reads := rowLoad(s, u.row)
	return max(reads-wr-freeHalves(s, u), 0)
}

// readyCardsOf is the unit's ready cards of the kind (isRead), in its start order (score).
func readyCardsOf(s *Snapshot, row string, read bool) []*Card {
	var out []*Card
	for _, c := range s.Fleet.Cell(row, Ready) {
		if isRead(c) == read {
			out = append(out, c)
		}
	}
	return out
}

// rebalanceTo is the unit the card goes to, nil when none may take it (Rebalance): the
// card's own tier before one up (dist), then round the units that may take it from the
// deal's rolling cursor, moved past the target ((index++) % n).
func rebalanceTo(s *Snapshot, units []*rebalanceUnit, g *rebalanceUnit, pr, wc *Card, tier string, r *round) *rebalanceUnit {
	want := slices.Index(capLadder, tier)
	gone := Split(wc.F(FieldRebalancedFrom))
	byName := map[string]*rebalanceUnit{}
	dist := map[string]int{}
	bestDist := -1
	for _, u := range units {
		if u == g || u.idle() <= 0 || slices.Contains(gone, u.row) {
			continue
		}
		if name, ok := FriendCard(pr); ok && name == "" && g.friend && !u.friend {
			continue // WHO: friend on a friend's row: its pin is honoured, it stays with the friends
		}
		d := 0
		if u.friend {
			if !friendTakes(s, u.seat, tier) || slices.Contains(cardLeft(pr, wc), u.name) {
				continue
			}
			top := -1
			for _, t := range friendTiers(u.seat) {
				top = max(top, slices.Index(capLadder, t))
			}
			d = top - want
		} else {
			if !s.FleetTakes(tier) || slices.Contains(StagingRefusers(wc), u.name) {
				continue
			}
			if _, _, why, byFriend := s.routeOf(pr, wc, nil, u.name); why != "" || byFriend {
				continue
			}
			if s.backoffWait(tier, []string{u.name}) != "" {
				// the check of the tier counts a backed-off route as served, but the move's
				// draw does not: never onto a machine with no route it can draw
				// (a-rate-limited-refusal-never-fails-the-cardc; tla/RateBackoff.tla)
				continue
			}
			if !g.friend && !s.Launches(u.name, Route{Harness: wc.F(FieldHarness)}) {
				continue // machine to machine keeps its route: never to one that cannot launch it
			}
		}
		if want < 0 || d < 0 || d > 1 {
			continue // own tier, or one up as overflow: never more, never below
		}
		byName[u.name] = u
		dist[u.name] = d
		if bestDist < 0 || d < bestDist {
			bestDist = d
		}
	}
	if bestDist < 0 {
		return nil
	}
	name := r.scan(func(x string) bool {
		u, ok := byName[x]
		return ok && dist[x] == bestDist && u.idle() > 0
	})
	if name == "" {
		return nil
	}
	r.moved(name)
	return byName[name]
}

// rebalanceMove is the card moved from g to to at its next generation, its old row named on
// it and in the note.
func rebalanceMove(s *Snapshot, p *Plan, declared map[string]bool, ri routeIndexes, g, to *rebalanceUnit, pr, wc *Card, tier, who string) Unit {
	set := nextGen(wc, to.row, s.Now)
	unset := []string{FieldFriendDeadline}
	set[FieldRebalancedFrom] = strings.Join(append(Split(wc.F(FieldRebalancedFrom)), g.row), ",")
	if g.friend {
		set[FieldFriendsLeft] = strings.Join(append(friendsLeft(wc), g.name), ",")
	}
	var prim map[string]string
	switch {
	case to.friend:
		// a friend runs her own model: the fleet route comes off
		unset = append(unset, FieldRoute, FieldModel, FieldTokens, FieldUSD, FieldHarness, FieldDeadline)
		prim = tierNowSet(pr, tier)
		if !s.Fleet.HasRow(to.row) && !declared[to.row] {
			p.Rows = append(p.Rows, RowAdd{Fleet, to.row})
			declared[to.row] = true
		}
	case g.friend:
		// off a friend's row onto a machine: a route of its tier, as a deal draws one
		route, _, _, _ := s.routeOf(pr, wc, ri, to.name)
		w, pm := splitRoute(route)
		s.dealDeadline(to.name, w)
		for k, v := range w {
			if v != "" {
				set[k] = v
			}
		}
		prim = pm
	default:
		s.movedDeadline(to.name, wc, set) // machine to machine: its route kept, as the level's
	}
	changes := []Change{change(Fleet, moveEntry(wc, to.row, Ready, set, unset...))}
	if len(prim) > 0 {
		changes = append(changes, change(Work, setEntry(pr, prim)))
	}
	what := fmt.Sprintf("rebalanced %s from %s to %s", wc.ID, g.row, to.row)
	n := happened(NRebalanced, pr.Row, s.Now, pr.ID)
	n.Who, n.What = who, what
	return Unit{Key: pr.ID, Stream: pr.Row, Changes: changes, Notes: []Note{n},
		Moved: fmt.Sprintf("%s gen=%d (its excess; a free lane takes it)", what, wc.Int("gen")+1)}
}

// rebalanceReads is the rebalance's read pass (Rebalance): the excess read cards of every
// unit (reads beyond the halves its working cards leave free) go to a unit with a free lane
// whose reader row serves the read's tier and that has not read the card at this attempt
// (readRefusal), the cheapest read tier first, then round the units from the read deal's
// cursor (read_deal_index_<tier>), as many as the idle lanes take (a read is half a slot:
// readUnit.half and idle are in half slots), in one tick. A read never goes back to a row it
// was rebalanced off (FieldRebalancedFrom). The moved read keeps its attempt: it is dealt
// again under the new reader's identity, its record retired on the row it left.
func rebalanceReads(s *Snapshot, seats []FriendSeat, units []*rebalanceUnit, workMoved map[string]int, p *Plan, ri routeIndexes, who string) []Unit {
	ruses := readUnitsOf(s, seats)
	if len(ruses) == 0 {
		return nil
	}
	unitByRow := make(map[string]*rebalanceUnit, len(units))
	for _, u := range units {
		unitByRow[u.row] = u
	}
	for i := range ruses {
		if u := unitByRow[ruses[i].row]; u != nil {
			// Share the exact half-slot capacity after the work pass. Round-up
			// whole-card load loses a usable half-slot when a row holds one read.
			ruses[i].idle = min(ruses[i].idle, rebalanceReadHalfRoom(s, u, workMoved[u.row]))
			ruses[i].half -= 2 * workMoved[u.row]
		}
	}
	idx := fleetReadIndex(s)
	readRounds := map[string]*round{}
	var out []Unit
	for _, g := range units {
		er := excessReads(s, g)
		if er <= 0 {
			continue
		}
		ready := readyCardsOf(s, g.row, true)
		if er < len(ready) {
			ready = ready[len(ready)-er:]
		}
		for _, wc := range ready {
			pr := s.Work.Placed(wc.F("primary"))
			if pr == nil {
				continue
			}
			attempt := readAttempt(pr)
			worker := attemptUnit(s, pr.ID, attempt)
			cards := idx[pr.ID]
			gone := Split(wc.F(FieldRebalancedFrom))
			tier := s.readTierOf(pr)
			cursor, ok := readRounds[tier]
			if !ok {
				cursor = readDealRound(s, tier, ruses)
				readRounds[tier] = cursor
			}
			bestDist := -1
			for i := range ruses {
				u := &ruses[i]
				if u.row == g.row || slices.Contains(gone, u.row) || u.idle <= 0 || u.half <= 0 {
					continue
				}
				if !mayReadCard(s, *u, pr, attempt, worker, cards) {
					continue
				}
				d := readTierDistance(s, *u, pr)
				if bestDist < 0 || d < bestDist {
					bestDist = d
				}
			}
			if bestDist < 0 {
				continue
			}
			eligible := func(name string) bool {
				for i := range ruses {
					u := &ruses[i]
					if u.name != name || u.row == g.row || slices.Contains(gone, u.row) || u.idle <= 0 || u.half <= 0 {
						continue
					}
					return mayReadCard(s, *u, pr, attempt, worker, cards) && readTierDistance(s, *u, pr) == bestDist
				}
				return false
			}
			name := cursor.scan(eligible)
			if name == "" {
				continue
			}
			var best *readUnit
			for i := range ruses {
				if ruses[i].name == name {
					best = &ruses[i]
					break
				}
			}
			cursor.moved(name)
			id := readCardIDFor(cards, pr.ID, attempt, best.name)
			if id == "" || (s != nil && s.Fleet != nil && s.Fleet.Card(id) != nil) {
				continue
			}
			fields := map[string]string{}
			for k, v := range wc.Fields {
				fields[k] = v
			}
			fields["reader"] = best.name
			fields[FieldRebalancedFrom] = strings.Join(append(Split(wc.F(FieldRebalancedFrom)), g.row), ",")
			if best.friend {
				fields[FieldTier] = s.readTierOf(pr)
				if !s.Fleet.HasRow(best.row) {
					p.Rows = append(p.Rows, RowAdd{Fleet, best.row})
				}
			} else {
				// a member draws a route of the read's tier, as the deal draws one
				// (readCardsAskFull, readRouteOf): the donor's route is stale on the
				// new member, which may not launch it (the owner's route rule).
				if ri != nil {
					maps.Copy(fields, s.readRouteOf(ri, pr, nil))
				} else {
					fields[FieldTier] = s.readTierOf(pr)
				}
				maps.Copy(fields, s.decideFields(pr, false))
			}
			// the moved read is a fresh card on the new reader; the one it left is retired
			// spending nothing of it (the machine took it back)
			best.idle--
			best.half--
			idx[pr.ID] = append(idx[pr.ID], &Card{ID: id, Row: best.row, Col: Ready, Score: wc.Score, Fields: fields})
			out = append(out, Unit{Key: pr.ID, Stream: pr.Row, Changes: []Change{
				change(Fleet, createEntry(id, best.row, Ready, wc.Score, fields)),
				change(Fleet, removeEntry(wc, map[string]string{"retired": stamp(s.Now), "retired_by": RetiredByAway})),
			}, Moved: fmt.Sprintf("rebalanced %s from %s to %s", wc.ID, g.row, best.row)})
		}
	}
	for _, tier := range slices.Sorted(maps.Keys(readRounds)) {
		writeRebalanceRound(p, readRounds[tier])
	}
	return out
}

// rebalanceReadHalfRoom is the exact remaining read capacity of a row after the work
// pass, in half slots. Unlike rebalanceUnit.idle, it preserves one free half-slot after
// an odd number of ready or working reads.
func rebalanceReadHalfRoom(s *Snapshot, u *rebalanceUnit, workMoved int) int {
	work, reads := rowLoad(s, u.row)
	return max(2*u.width-2*work-reads-2*workMoved, 0)
}
