package sprint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// A member is a fleet machine, and its width is the machine's child cap: the
// most cards it runs at once, every card in its working column counting one, a
// read card the same as a work card (the owner, 2026-10-18: every fleet machine
// and every friend follows the single definition of width, in nova-config, with
// no exceptions). It holds up to DealAhead times its width,
// ready and working together: its width working and as many again ready
// behind them, so the sprint feeds the fleet at width and keeps it working at
// that width until done. The width is nova-config's: the member's machine row
// (nova-config machine set <m> --width <n>, then nova-config apply). The
// control card's width field is a refreshed copy of that read, written only
// from it (fleet sync, and every store step that holds the config read:
// WidthRefresh); fleet up <m> --width <n> and init --members <m>:<n> are
// refused a width that is not nova-config's, and fleet up <m> --width 0 (the
// drain) stays. It is shown in the fleet table's width column. The width is
// held at the one writer on every path that puts a card into working
// (HoldWidths; tla/WidthHard.tla WidthHeld). The deal fills every member free lanes
// first (a member below its width takes the card, so a lane that is free takes
// the next card at once), then, only when no member that may take the card has
// one, the 2x room, DealAhead times its width, round the fleet from the
// cursor (round.go nextFree; T3, R6); the level, once at the start of every
// tick, moves ready cards from a member that cannot start them to one with
// free lanes, never past DealAhead times its width (T4, R7). The model is
// tla/SprintEvents.tla: PlanDeal's room (its Cap), which counts the ready
// cards of a member; the room here counts ready and working.
const (
	// FieldWidth is the control card's field, and the fleet table's column,
	// holding the member's width.
	FieldWidth = "width"
	// DefaultWidth is the width of a member whose control card names none.
	DefaultWidth = 64
	// MaxWidth is the widest a member may be: a width over it is refused
	// before anything is written.
	MaxWidth = 1024
	// TickMaxDeal is the most cards one deal moves (T3, R6). The deal is one
	// plan; the store's apply cuts it into parts under the table layer's
	// entry bounds, so the deal refuses nothing by size under this.
	TickMaxDeal = 2000
)

// ParseWidth is a width as the verbs take it: a whole number from 1 to
// MaxWidth.
func ParseWidth(text string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 1 || n > MaxWidth {
		return 0, fmt.Errorf("a width wants a whole number from 1 to %d, got %q", MaxWidth, text)
	}
	return n, nil
}

// MemberWidth is the width a member's control card names: DefaultWidth when
// it names none or one that is not a width; 0 is a member that drains (fleet
// up --width 0): no room for a deal or the level, its working cards finish.
func MemberWidth(ctl *Card) int {
	if strings.TrimSpace(ctl.F(FieldWidth)) == DrainWidth {
		return 0
	}
	if n, err := ParseWidth(ctl.F(FieldWidth)); err == nil {
		return n
	}
	return DefaultWidth
}

// DrainWidth is the width cell of a member that drains: fleet up <m> --width 0
// writes it, and it is no member of the deal, the level or a take by count
// (every room is DealAhead times 0) while its working cards finish, unlike
// fleet down, which deals them again elsewhere (the comfort list of
// 2026-10-03, item 5).
const DrainWidth = "0"

// WidthText is the fleet table's width cell of a member's control card.
func WidthText(ctl *Card) string { return strconv.Itoa(MemberWidth(ctl)) }

// Width is the member's width.
func (s *Snapshot) Width(member string) int { return MemberWidth(s.MemberCtl(member)) }

// MemberSpec is a member as init --members names it: <name>[:<width>], a
// bare name keeping the default (Width 0).
type MemberSpec struct {
	Name  string
	Width int
}

// ParseMembers is init --members: a comma list of <name>[:<width>].
func ParseMembers(list string) ([]MemberSpec, error) {
	var out []MemberSpec
	for _, x := range Split(list) {
		name, w, hasWidth := strings.Cut(x, ":")
		spec := MemberSpec{Name: name}
		if !ValidID(name) {
			return nil, fmt.Errorf("a member name wants letters, digits, _ and -: %q", x)
		}
		if hasWidth {
			n, err := ParseWidth(w)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", name, err)
			}
			spec.Width = n
		}
		out = append(out, spec)
	}
	return out, nil
}

// memberLoads is each up member's cards held: its ready and its working
// cells, the count its width bounds.
//
// A read card on the row holds a whole slot, as a work card does (slotLoad): a row of
// width w runs w cards, of either kind (tla/WidthHard.tla WidthHeld).
func memberLoads(s *Snapshot, up []string) map[string]int {
	q := map[string]int{}
	for _, m := range up {
		q[m] = slotLoad(rowLoad(s, m))
	}
	return q
}

// slotLoad is a load counted in slots, one a card whatever its kind: the one definition
// of a slot (tla/WidthHard.tla, a read counts whole; MCWidthHardBrokenHalfRead is the
// half-slot count it replaced).
func slotLoad(work, reads int) int { return work + reads }

// DealAhead is how many widths of cards a member may hold, ready and working
// together: its width working and as many again ready behind them, so a lane
// that frees takes its next card at once and never waits for a tick. The
// member itself runs at most its width (the fleet row's, pkg/member);
// the rest wait in its ready column.
const DealAhead = 2

// memberWidths is each up member's room for every placement (the deal, a
// redeal, the level, a down member's cards): DealAhead times its width.
func memberWidths(s *Snapshot, up []string) map[string]int {
	w := map[string]int{}
	for _, m := range up {
		w[m] = DealAhead * s.Width(m)
	}
	return w
}

// memberLanes is each up member's width: its lanes, the bound of the deal's
// first scan (nextFree): a member below it has a lane free and takes the card
// at once, before any member that only has room (below DealAhead times its
// width, memberWidths).
func memberLanes(s *Snapshot, up []string) map[string]int {
	l := map[string]int{}
	for _, m := range up {
		l[m] = s.Width(m)
	}
	return l
}

// widthRoom is the sum over the up members of the cards each can still take
// before it holds DealAhead times its width.
func widthRoom(s *Snapshot, up []string) int {
	room := 0
	for _, m := range up {
		room += max(0, DealAhead*s.Width(m)-slotLoad(rowLoad(s, m)))
	}
	return room
}

// WidthOfCores is the default width of a machine with these logical cores, half
// of them (the owner, 2026-10-02: "default is CPUs/2"), at least 1 and at most
// MaxWidth; 0 when the cores are not known (no beat has reported them). It is
// what fleet sync writes for a machine row with no width (config.MachineWidth,
// Default).
func WidthOfCores(cores int) int {
	if cores <= 0 {
		return 0
	}
	return min(MaxWidth, max(1, cores/2))
}

// RowWidth is a fleet row's width as the step holds it, one definition for members and
// friends alike: a member's is its nova-config machine row's width (cfg, the applied
// config the store read for the step, by member; config.Widths), else its control
// card's refreshed copy of it (MemberWidth); a member that drains (fleet up --width 0)
// is 0 whatever cfg says; a friend's is her friend row's width (FriendSeat.Width, which
// friend sync reads from her nova-config friend row). ok is false when the step does not
// hold it: a friend whose seat the snapshot has not read, or a row with no control card.
// tla/WidthHard.tla width[r] (OneSource).
func RowWidth(s *Snapshot, row string, cfg map[string]int) (width int, ok bool) {
	if s == nil || s.Fleet == nil {
		return 0, false
	}
	if f, friend := FriendOfRow(row); friend {
		i := slices.IndexFunc(s.Friends, func(x FriendSeat) bool { return x.Name == f })
		if i < 0 {
			return 0, false
		}
		_, w := friendRoom(s.Friends[i])
		return w, true
	}
	ctl := s.MemberCtl(row)
	if ctl == nil {
		return 0, false
	}
	w := MemberWidth(ctl)
	if w == 0 {
		return 0, true // the drain: no card is taken while its working cards finish
	}
	if n, read := cfg[row]; read && n >= 1 && n <= MaxWidth {
		return n, true
	}
	return w, true
}

// intoWorking is the fleet row a change puts a card into the working column of: a move
// into working of a card not already working there, or a create in working; ok false for
// every other change (tla/WidthHard.tla Take, ReadTake, FriendNext).
func intoWorking(s *Snapshot, ch Change) (row string, ok bool) {
	if ch.Table != Fleet {
		return "", false
	}
	e := ch.Entry
	switch {
	case e.Create != nil:
		return e.Create.Row, e.Create.Col == Working && e.Create.Row != ""
	case e.Move != nil && e.Move.Col == Working:
		c := s.Fleet.Card(e.ID)
		if c.Placed() && c.Row == e.Move.Row && c.Col == Working {
			return "", false // already working there: a score or a field, no take
		}
		return e.Move.Row, e.Move.Row != ""
	}
	return "", false
}

// outOfWorking is the fleet row a change takes a working card out of: a remove, or a move
// to another column or row; ok false otherwise (tla/WidthHard.tla Finish).
func outOfWorking(s *Snapshot, ch Change) (row string, ok bool) {
	if ch.Table != Fleet {
		return "", false
	}
	c := s.Fleet.Card(ch.Entry.ID)
	if !c.Placed() || c.Col != Working {
		return "", false
	}
	e := ch.Entry
	if e.Remove || (e.Move != nil && (e.Move.Row != c.Row || e.Move.Col != Working)) {
		return c.Row, true
	}
	return "", false
}

// PlanWorking is a row's working count once the units apply: the snapshot's working
// column, less every card the units take out of it, plus every card they move or create
// into it. friendNext reads it over the units of her finish (tla/WidthHard.tla
// FriendNext).
func PlanWorking(s *Snapshot, row string, units []Unit) int {
	n := len(s.Fleet.Cell(row, Working))
	for _, u := range units {
		for _, ch := range u.Changes {
			if r, ok := outOfWorking(s, ch); ok && r == row {
				n--
			}
			if r, ok := intoWorking(s, ch); ok && r == row {
				n++
			}
		}
	}
	return n
}

// MovesIntoFriendWorking says a unit of the plan puts a card into a friend row's working
// column: the store reads the friends' seats for the width check when the step has not.
func MovesIntoFriendWorking(s *Snapshot, p Plan) bool {
	if s == nil || s.Fleet == nil {
		return false
	}
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if row, ok := intoWorking(s, ch); ok && IsFriendRow(row) {
				return true
			}
		}
	}
	return false
}

// HoldWidths is THE WIDTH IS HARD at the sprint's one writer, on every path at once: the
// store applies it to every step's plan (store.Store.Run), so no take (by count, by id or
// by lane), friend's next, start receipt, read create or any later path can pass a row's
// width. Working is counted as the step's snapshot holds it, less every card the plan
// takes out of working (the plan applies as one), plus each card an earlier change of the
// plan put in. A change that would put a card into a row's working at its width (working
// >= RowWidth) is skipped alone, with one refusal line naming the row, its working count
// and its width: a move is dropped (the card stays where it is and is taken when a card
// there finishes); a create is made in ready instead; a unit left with no change is
// dropped. Every other unit applies: nothing is refused for the fleet as a whole, no card
// is failed and no attempt is spent. A row whose width the step does not hold (RowWidth ok
// false) is not cut here; takeSeat refuses its takes. cfg is the applied config's member
// widths (nil when the store holds no config read). tla/WidthHard.tla WidthHeld (the
// reversed witnesses: MCWidthHardBrokenHalfRead, MCWidthHardBrokenFriendNext,
// MCWidthHardBrokenSecondSource).
func HoldWidths(s *Snapshot, p Plan, cfg map[string]int) Plan {
	if s == nil || s.Fleet == nil || len(p.Units) == 0 {
		return p
	}
	working := map[string]int{}
	seen := func(row string) {
		if _, ok := working[row]; !ok {
			working[row] = len(s.Fleet.Cell(row, Working))
		}
	}
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if row, ok := outOfWorking(s, ch); ok {
				seen(row)
				working[row]--
			}
		}
	}
	units := make([]Unit, 0, len(p.Units))
	for _, u := range p.Units {
		var keep []Change
		var dropped, readied []string
		cut := false
		for _, ch := range u.Changes {
			row, ok := intoWorking(s, ch)
			if !ok {
				keep = append(keep, ch)
				continue
			}
			width, known := RowWidth(s, row, cfg)
			if !known {
				keep = append(keep, ch)
				continue
			}
			seen(row)
			if working[row] < width {
				working[row]++
				keep = append(keep, ch)
				continue
			}
			cut = true
			why := fmt.Sprintf("%s is at its width (%d working of %d): %s is not moved to working; it waits and is taken when a card there finishes (the width is hard, every card working counts one: tla/WidthHard.tla WidthHeld)", row, working[row], width, ch.Entry.ID)
			p.Refused = append(p.Refused, Refusal{Key: ch.Entry.ID, Why: why})
			if ch.Entry.Create != nil {
				e := ch.Entry
				cr := *e.Create
				cr.Col = Ready
				e.Create = &cr
				keep = append(keep, Change{Table: ch.Table, Entry: e})
				readied = append(readied, fmt.Sprintf("%s made ready, not working: %s is at its width (%d of %d)", ch.Entry.ID, row, working[row], width))
				continue
			}
			dropped = append(dropped, ch.Entry.ID)
		}
		if !cut {
			units = append(units, u)
			continue
		}
		if len(keep) == 0 {
			continue // the unit was the move alone
		}
		u.Changes = keep
		u.Moved = movedWithout(u.Moved, dropped)
		if len(readied) > 0 {
			u.Moved = strings.Join(append([]string{u.Moved}, readied...), "; ")
		}
		units = append(units, u)
	}
	p.Units = units
	return p
}

// movedWithout is a unit's moved line less the later parts ("; " separated) that begin
// with a card a width cut dropped: a finish's line keeps its finish and loses the next it
// named.
func movedWithout(moved string, ids []string) string {
	if len(ids) == 0 {
		return moved
	}
	parts := strings.Split(moved, "; ")
	out := parts[:0]
	for i, part := range parts {
		drop := false
		for _, id := range ids {
			if i > 0 && strings.HasPrefix(part, id+" ") {
				drop = true
			}
		}
		if !drop {
			out = append(out, part)
		}
	}
	return strings.Join(out, "; ")
}

// WidthRefresh makes each member's control-card width nova-config's again: the one width
// source (tla/WidthHard.tla Apply, OneSource). cfg is the applied config's member widths
// as the store read them for the step (config.Widths: a machine row's own width, 1 or
// more); a member whose copy differs gets one unit setting it, unless the plan already
// changes its control card (that step's own write stands; the next step refreshes it) or
// it drains (fleet up --width 0 stays until fleet up lifts it). A row with the default
// width or width 0 is fleet sync's (it resolves the cores and the holds). It never widens
// nova-config: the read of nova-config is the fix.
func WidthRefresh(s *Snapshot, p Plan, cfg map[string]int) Plan {
	if s == nil || s.Fleet == nil || len(cfg) == 0 {
		return p
	}
	touched := map[string]bool{}
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Table == Fleet {
				touched[ch.Entry.ID] = true
			}
		}
	}
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		n, read := cfg[m]
		if ctl == nil || !read || n < 1 || n > MaxWidth || touched[ctl.ID] {
			continue
		}
		have := MemberWidth(ctl)
		if have == 0 || strings.TrimSpace(ctl.F(FieldWidth)) == itoa(n) {
			continue
		}
		p.Units = append(p.Units, Unit{Key: ctl.ID,
			Changes: []Change{change(Fleet, setEntry(ctl, map[string]string{FieldWidth: itoa(n)}))},
			Moved:   fmt.Sprintf("%s width=%d (was %d): read from nova-config, the one width (nova-config machine list)", m, n, have)})
	}
	return p
}

// WidthDrift is each member whose control-card width is not nova-config's (cfg), one line
// each with both values, in member order: what nova-sprint doctor reports as a FAIL and
// WidthRefresh writes. A member that drains is no drift.
func WidthDrift(s *Snapshot, cfg map[string]int) []string {
	if s == nil || s.Fleet == nil {
		return nil
	}
	var out []string
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		n, read := cfg[m]
		if ctl == nil || !read || n < 1 || MemberWidth(ctl) == 0 {
			continue
		}
		if have := MemberWidth(ctl); have != n {
			out = append(out, fmt.Sprintf("%s: sprint width %d, nova-config width %d (the one width is nova-config's: nova-config machine list)", m, have, n))
		}
	}
	return out
}
