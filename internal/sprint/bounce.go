package sprint

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

// BounceBack (v1.2.6; the model is tla/DealFill.tla, rules B1 to B4). The owner, 2026-10-10:
// "We can't have reviews just disappearing like this." "Cards need to bounce back. We can't
// just have cards disappearing." "If they fail to do the thing they are supposed to do, you
// MUST be notified via push."
//
// That day 43 reads were asked of a friend's reader row whose lanes all failed (exit 126)
// and none was begun; 47 cards were dealt to two friends whose daemons could not take them;
// a friend down held 30 ready cards. Each moved only on a coordinator verb.
//
// One machinery for work and reads (reads are consumer cards), the tick's part PartBounce,
// run in the work table's update before the deal:
//
//   - B1: a work card dealt to a member's row (friend or machine) and not taken within the
//     take bound (set --take-bound, TakeBound) is withdrawn to the pool, and the deal of the
//     same tick deals it again; a read card asked and not begun within the take bound is
//     retired (RetiredByUnstarted, spending no one) and the read-card ask asks it again of
//     the next reader, every tick, until the primary has the reads it needs; a read card
//     begun and past ReadCardDeadline is retired late (RetiredByLate), as before, and
//     pushed. A work card taken and past its deadline is the late rule's (ruleLate).
//   - B2: every return is a note addressed to the coordinator (a pushed note: inbox --push
//     wakes the seat for every note with a To), naming the card, the member, how long, and
//     what was done; every member set aside is a judgment (NMemberAside) while it stands.
//   - B3: a member whose card bounced, or whose last TakeFaults takes ended on a harness
//     fault (HarnessFault) or a lane that could not run (exit 126), is set aside
//     (AsideRows): the deal and the ask pass it by until it takes again, or until the
//     cooldown (BounceCooldown) has passed since it was set aside, when it is dealt again
//     (its probe). A down member is dealt nothing, as always.
//   - B4: the deal (TickDeal) deals over a view with every member set aside held out
//     (withoutAside), so a returned card goes to the next capable live member.

// PartBounce is the bounce-back part of the work table's update, before the deal.
const PartBounce = "bounce"

// NBounced is the note of a card returned to the pool by the bounce-back, addressed to the
// coordinator so that it is pushed; NMemberAside is the judgment of a member set aside.
const (
	NBounced     = "bounced"
	NMemberAside = "member aside"
)

// FieldBouncedFrom is the row a card was last returned from by the bounce-back, and
// FieldBounced when.
const (
	FieldBouncedFrom = "bounced_from"
	FieldBounced     = "bounced"
)

// PropBounced is the fleet table's property of the row's last bounce, a stamp: the row is
// set aside from it until it takes again (AsideRows). A property name is letters, digits,
// _ . and -, so the row follows a dot.
func PropBounced(row string) string { return "bounced." + row }

// BounceCooldown is how long a member set aside waits before the deal tries it again (its
// probe): three take bounds.
func BounceCooldown(s *Snapshot) time.Duration { return 3 * s.TakeBound() }

// Aside is why a row is set aside, and since when.
type Aside struct {
	Since time.Time
	Why   string
}

// rowLastTake is the newest take of a card on the row: its taken or first_taken stamp, its
// progress stamp, or its finish ok; zero when none.
func rowLastTake(s *Snapshot, row string) time.Time {
	var at time.Time
	for _, col := range []string{Working, DoneOK, DoneFailed} {
		for _, c := range s.Fleet.Cell(row, col) {
			for _, k := range []string{"taken", "first_taken", FieldProgress} {
				if t := stampAt(c, k); t.After(at) {
					at = t
				}
			}
			if col == DoneOK {
				if t := stampAt(c, "finished"); t.After(at) {
					at = t
				}
			}
		}
	}
	return at
}

// cannotRunRE is a take whose lane could not run its harness at all (2026-10-10: a friend's
// every lane exited 126, and her reads and cards sat dealt to her). It counts as a take fault
// for the set-aside alone; the card's own rules read the report as they always did.
var cannotRunRE = regexp.MustCompile(`(?i)\bexit(ed)?( with)?( status| code)?[ :=]*12[67]\b|cannot execute`)

// takeFault is the class of a failed take the set-aside counts: a harness fault
// (HarnessFault), or a lane that could not run (cannotRunRE); "" for none.
func takeFault(report string) string {
	if k := HarnessFault(report); k != "" {
		return k
	}
	if cannotRunRE.MatchString(report) {
		return "cannot run"
	}
	return ""
}

// takeFaults is when the row's last n finishes all ended on a take fault (takeFault): the
// newest such finish, zero when they did not.
func takeFaults(s *Snapshot, row string, n int) (time.Time, string) {
	var done []*Card
	for _, col := range []string{DoneOK, DoneFailed} {
		done = append(done, s.Fleet.Cell(row, col)...)
	}
	if len(done) < n {
		return time.Time{}, ""
	}
	slices.SortStableFunc(done, func(a, b *Card) int { return stampAt(b, "finished").Compare(stampAt(a, "finished")) })
	var class string
	for _, c := range done[:n] {
		if c.Col != DoneFailed {
			return time.Time{}, ""
		}
		k := takeFault(c.F("report"))
		if k == "" {
			return time.Time{}, ""
		}
		class = cmp.Or(class, k)
	}
	return stampAt(done[0], "finished"), class
}

// AsideRows is every fleet row set aside now (B3, tla/DealFill.tla Dealable): a bounce
// (PropBounced) or its last TakeFaults finishes on a harness fault, after its newest take,
// and within the cooldown. A row whose newest take is after both is not.
func AsideRows(s *Snapshot) map[string]Aside {
	out := map[string]Aside{}
	if s == nil || s.Fleet == nil {
		return out
	}
	cool := BounceCooldown(s)
	n := s.TakeFaults()
	for _, row := range s.Fleet.Rows() {
		took := rowLastTake(s, row)
		var a Aside
		if v, ok := s.Fleet.Prop(PropBounced(row)); ok && v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil && t.After(took) {
				a = Aside{Since: t, Why: "a card dealt to it was not taken within the take bound " + s.TakeBound().String()}
			}
		}
		if t, class := takeFaults(s, row, n); !t.IsZero() && t.After(took) && t.After(a.Since) {
			a = Aside{Since: t, Why: fmt.Sprintf("its last %d takes ended on a harness fault (%s)", n, class)}
		}
		if a.Since.IsZero() || !s.Now.Before(a.Since.Add(cool)) {
			continue
		}
		out[row] = a
	}
	return out
}

// friendRunning is the cards a friend's beat names running, by her name or her row (the
// stall ladder reads the same, friend_stall.go): ids, job names or primaries.
func friendRunning(s *Snapshot, r TickReq, row string) map[string]bool {
	out := map[string]bool{}
	f, ok := FriendOfRow(row)
	if !ok {
		return out
	}
	for _, key := range []string{f, row} {
		if b, ok := r.Beats[key]; ok && b.Friend != nil {
			for _, x := range b.Friend.Running {
				out[x] = true
			}
		}
	}
	for _, seats := range [][]FriendSeat{r.Friends, s.Friends} {
		for _, seat := range seats {
			if seat.Name == f {
				for _, x := range seat.Running {
					out[x] = true
				}
			}
		}
	}
	return out
}

// cardBegun says the card was taken or begun: a take stamp, a progress stamp or a report on
// it, or its friend's beat names it (or its primary) running.
func cardBegun(c *Card, running map[string]bool) bool {
	return c.F("taken") != "" || c.F(FieldProgress) != "" || c.F(FieldReported) != "" || running[c.ID] || running[c.F("primary")]
}

// rowHasRoom says the row has a lane its begun cards leave free, in half slots (a read is
// half a slot, as the ask counts it: readUnitsOf): a machine's width, a friend's seat's. A
// card waiting dealt behind a member whose every lane works is the member's queue (DealAhead),
// never a member that does not take (store dealt_bound_test.go).
func rowHasRoom(s *Snapshot, r TickReq, row string, running map[string]bool) bool {
	width := 0
	if f, ok := FriendOfRow(row); ok {
		for _, seats := range [][]FriendSeat{r.Friends, s.Friends} {
			for _, seat := range seats {
				if seat.Name == f && seat.Width > 0 && width == 0 {
					width = seat.Width
				}
			}
		}
	} else {
		width = s.Width(row)
	}
	filled := 0
	for _, c := range s.Fleet.Cell(row, Working) {
		switch {
		case !cardBegun(c, running):
		case isRead(c):
			filled++
		default:
			filled += 2
		}
	}
	return filled < 2*max(width, 1)
}

// rowQuiet says the row has taken, begun or finished nothing within the take bound: its
// newest take, progress or finish (rowLastTake, and a failed finish) is that old, or it has
// none. A member that finished a card a moment ago and has not yet taken the next is not
// quiet.
func rowQuiet(s *Snapshot, row string, elapsed func(string) (time.Duration, bool)) bool {
	at := rowLastTake(s, row)
	for _, c := range s.Fleet.Cell(row, DoneFailed) {
		if t := stampAt(c, "finished"); t.After(at) {
			at = t
		}
	}
	if at.IsZero() {
		return true
	}
	d, ok := elapsed(stamp(at))
	return ok && d >= s.TakeBound()
}

// notTakenPast says the card, dealt to its row (a work card in ready) or asked of it (a read
// card in ready, or dealt straight to working on a friend's row and never begun there), was
// not taken or begun within the take bound while the row had a lane free and took nothing
// (B1, tla/DealFill.tla Late): how long since it was dealt or asked. running is its friend's
// beat (friendRunning); elapsed measures in running time.
func notTakenPast(s *Snapshot, r TickReq, c *Card, running map[string]bool, elapsed func(string) (time.Duration, bool)) (time.Duration, bool) {
	var at string
	switch {
	case isRead(c):
		if c.F(FieldReadCard) == "" || cardBegun(c, running) {
			return 0, false
		}
		at = c.F("asked")
	case c.Col != Ready || running[c.ID] || running[c.F("primary")]:
		return 0, false // taken, or her beat names it running: started (the stall ladder's)
	default:
		// the work card's own deal to this row, the stamp WorkDeadline names (one clock:
		// the take bound is a second limit on it, never a second stamp)
		_, _, _, own := WorkDeadline(s, c)
		at = c.F(own)
	}
	d, ok := elapsed(at)
	if !ok || d < s.TakeBound() || !rowHasRoom(s, r, c.Row, running) || !rowQuiet(s, c.Row, elapsed) {
		return d, false
	}
	return d, true
}

// bounced is one return the part makes.
type bounced struct {
	card, primary, stream, row, what string
	change                           Change
	unit                             *Unit
}

// TickBounce is the bounce-back (PartBounce; tla/DealFill.tla Tick, B1 to B3): every work
// card dealt and not taken within the take bound withdrawn, every read card asked and not
// begun within it retired, and every read card begun and past its deadline retired late,
// each with a note pushed to the coordinator; every row it returns a card from marked
// (PropBounced), and one judgment for each row set aside.
func TickBounce(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if s == nil || s.Fleet == nil || s.Work == nil {
		return p, 0
	}
	bound := s.TakeBound()
	elapsed := func(at string) (time.Duration, bool) { return r.running(s.Now, at) }
	var out []bounced
	runningOf := map[string]map[string]bool{}
	running := func(row string) map[string]bool {
		if m, ok := runningOf[row]; ok {
			return m
		}
		runningOf[row] = friendRunning(s, r, row)
		return runningOf[row]
	}
	for _, c := range s.Fleet.Column(Ready, Working) {
		switch {
		case isRead(c):
			by, d := "", time.Duration(0)
			if dd, ok := notTakenPast(s, r, c, running(c.Row), elapsed); ok {
				by, d = RetiredByUnstarted, dd
			} else if c.F(FieldReadCard) != "" && c.Col == Working && cardBegun(c, running(c.Row)) && !readStart(c).IsZero() && !s.Now.Before(readStart(c).Add(ReadCardDeadline)) {
				by, d = RetiredByLate, s.Now.Sub(readStart(c))
			}
			if by == "" {
				continue
			}
			what := fmt.Sprintf("read %s of %s asked of %s %s ago and not begun within the take bound %s while it had a lane free: retired, asked again of the next reader", c.ID, c.F("primary"), c.Row, d.Round(time.Second), bound)
			if by == RetiredByLate {
				what = fmt.Sprintf("read %s of %s on %s begun %s ago, past its deadline %s with no verdict: retired late, asked again of the next reader", c.ID, c.F("primary"), c.Row, d.Round(time.Second), ReadCardDeadline)
			}
			out = append(out, bounced{card: c.ID, primary: c.F("primary"), stream: c.F("stream"), row: c.Row, what: what,
				change: change(Fleet, removeEntry(c, map[string]string{"retired": stamp(s.Now), "retired_by": by, FieldBouncedFrom: c.Row, FieldBounced: stamp(s.Now)}))})
		case c.Col == Ready:
			d, ok := notTakenPast(s, r, c, running(c.Row), elapsed)
			if !ok {
				continue
			}
			what := fmt.Sprintf("%s (of %s) dealt to %s %s ago and not taken within the take bound %s while it had a lane free: returned to the pool and dealt to the next capable member", c.ID, c.F("primary"), c.Row, d.Round(time.Second), bound)
			u := withdrawUnit(s, c, map[string]string{FieldBouncedFrom: c.Row, FieldBounced: stamp(s.Now)}, nil, NBounced, r.who(), what)
			u.Notes = nil // the part's own note, addressed (below)
			out = append(out, bounced{card: c.ID, primary: c.F("primary"), stream: c.F("stream"), row: c.Row, what: what, unit: &u})
		}
	}
	// one unit for the read cards of each primary (the ask's own grouping, readCardsAsk)
	reads := map[string]*Unit{}
	rows := map[string]bool{}
	for _, b := range out {
		rows[b.row] = true
		n := happened(NBounced, b.stream, s.Now, b.primary)
		n.Who, n.To, n.What = r.who(), s.Coordinator, b.what
		if b.unit != nil {
			b.unit.Notes = append(b.unit.Notes, n)
			p.Units = append(p.Units, *b.unit)
			continue
		}
		u, ok := reads[b.primary]
		if !ok {
			u = &Unit{Key: b.primary, Stream: b.stream}
			reads[b.primary] = u
		}
		u.Changes = append(u.Changes, b.change)
		u.Notes = append(u.Notes, n)
		u.Moved = strings.TrimPrefix(u.Moved+"; "+b.card+" retired off "+b.row+" (bounce)", "; ")
	}
	for _, k := range slices.Sorted(maps.Keys(reads)) {
		p.Units = append(p.Units, *reads[k])
	}
	for _, row := range slices.Sorted(maps.Keys(rows)) {
		was, had := s.Fleet.Prop(PropBounced(row))
		p.Props = append(p.Props, PropWrite{Table: Fleet, Name: PropBounced(row), Value: stamp(s.Now), Was: was, WasAbsent: !had})
	}
	// B2: a judgment for each row set aside, standing while it is (notify closes it when it
	// takes again or its cooldown passes); a row this part sets aside is judged at once
	aside := AsideRows(s)
	for row := range rows {
		if _, ok := aside[row]; !ok {
			aside[row] = Aside{Since: s.Now, Why: "a card dealt to it was not taken within the take bound " + bound.String()}
		}
	}
	var conds []cond
	for _, row := range slices.Sorted(maps.Keys(aside)) {
		a := aside[row]
		conds = append(conds, cond{typ: NMemberAside, stream: MemberSubject(row), streamLevel: true,
			what:      fmt.Sprintf("%s set aside since %s: %s; the deal and the ask pass it by until it takes again, or for %s, then try it again", row, stamp(a.Since), a.Why, BounceCooldown(s)),
			decisions: asideDecisions(row)})
	}
	due := notify(&p, s, conds, []string{NMemberAside}, r)
	return p, due
}

// asideDecisions is what the coordinator may do for a row set aside: look at the member or
// friend, take it down, or wait for its probe.
func asideDecisions(row string) []string {
	if f, ok := FriendOfRow(row); ok {
		return []string{"friend down " + f + " --reason '<why>'", "ack", "wait"}
	}
	return []string{"fleet down " + row, "ack", "wait"}
}

// withoutAside is the deal's view (B4, tla/DealFill.tla Deal): every member set aside held
// out of it, its control card's status "aside" on a frozen copy of the fleet table, and
// every friend set aside "aside" among the seats; s and seats themselves unchanged.
func withoutAside(s *Snapshot, seats []FriendSeat) (*Snapshot, []FriendSeat) {
	aside := AsideRows(s)
	if len(aside) == 0 {
		return s, seats
	}
	v := *s
	v.Fleet = s.Fleet.Frozen()
	for row := range aside {
		if _, friend := FriendOfRow(row); friend {
			continue
		}
		if ctl := s.MemberCtl(row); ctl != nil {
			c := *ctl
			c.Fields = maps.Clone(ctl.Fields)
			c.Fields["status"] = StatusAside
			v.Fleet.Put(&c)
		}
	}
	mark := func(in []FriendSeat) []FriendSeat {
		if in == nil {
			return nil
		}
		out := slices.Clone(in)
		for i := range out {
			if a, ok := aside[FriendRow(out[i].Name)]; ok {
				out[i].Status, out[i].Why = StatusAside, "set aside: "+a.Why
			}
		}
		return out
	}
	v.Friends = mark(s.Friends)
	return &v, mark(seats)
}

// StatusAside is the status the deal's view gives a member or friend set aside (withoutAside):
// neither up nor down; never written to a table.
const StatusAside = "aside"
