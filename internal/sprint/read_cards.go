package sprint

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// A read is a consumer card (docs/SPEC-SPRINT.md section 6, "A read is a consumer card";
// the owner, 2026-10-06: "reads need to become a type of card"; "send out multiple consumer
// cards in ||"; "Just remove the complexity. just deal it."). The sprint's read_cards
// setting is on by default, so the tick's deal deals every read a primary in review
// still needs AT ONCE, before its work cards, each a read card on the fleet table cut and
// dealt in one step to a different reader: a friend whose roles name reader and whose tiers
// hold the read's tier, or a member up whose reader row (reader-<m>) is neither held nor
// retired and serves the tier; never the attempt's own worker, never a reader that holds or
// closed a read of the attempt (a read the machine took back spends nothing). A read holds
// half a slot of its unit's one width (halfLoad). Its tier is its primary's, its level
// inherited (ReadPriority). The review is unchanged: a read card's verdict closes the read on
// the primary as read --ok|--broken does (friendReadCloseUnit, readCardVerb), and the primary
// leaves review only by the existing rules. The model is tla/ReadCards.tla. With the setting
// off (set --read-cards off), the readers table's ask and the friends' read ask, as before;
// they retire next release.

// PropReadCards is the work table's read-card switch; only off disables it.
const PropReadCards = "read_cards"

// ReadCardsOnWord is the setting's on word; off is the only disabling value.
const ReadCardsOnWord = "on"

// ReadCardsOn says the sprint asks its reads as read cards (PropReadCards).
// Absence and default are on; an explicit off keeps the legacy readers-table flow.
func (s *Snapshot) ReadCardsOn() bool {
	if s == nil || s.Work == nil {
		return true
	}
	v, _ := s.Work.Prop(PropReadCards)
	return v != SwitchOff
}

// FieldReadCard marks a read card the deal cut (read_cards.go), "1": a friend's read asked
// before read cards has none (its inbox job is its card id: Packet.ReadJob).
const FieldReadCard = "read_card"

// ReadCardDeadline is how long a read card is given from its start (its take; a friend's
// read dealt working, from its deal) before the deal retires it late, spending its reader,
// and deals the read to another. A read never started is never late: one left untouched in
// ready past the deal bound (DealtMax) is taken back spending no one (RetiredByUnstarted).
const ReadCardDeadline = 60 * time.Minute

// readCardLoadWindow is how many generations past the highest placed card ReadCardExtras
// loads and the deal may advance through (K=4), besides the generations the primary's
// read top holds (readGenTop). Past both, a reader is skipped.
const readCardLoadWindow = 4

// PropReadGenTop is the fleet property holding the highest read card generation the deal
// cut past the load window for any reader of the primary, "<attempt>:<gen>" (readGenTop).
// It is written in the plan that cuts that generation (readGenTops), so every generation of
// the attempt lies at or below the bound ReadCardExtras loads from the placed records and
// the properties alone, and a reader whose reads the machine took back past the window is
// dealt it again at a fresh one, never refused forever (the owner, 2026-10-06: "I don't
// want any serial reads again"; 2026-10-15, forty review cards and no reads dealt, every
// fleet reader's five generations taken back off a resting route; tla/ReadCards.tla,
// CompleteView and NeverWedged).
func PropReadGenTop(primary string) string { return "read_gen_top." + primary }

// readGenTop is the generation the primary's read top holds for the attempt (PropReadGenTop),
// 0 for none or for another attempt.
func readGenTop(s *Snapshot, primary string, attempt int) int {
	if s == nil || s.Fleet == nil {
		return 0
	}
	v, ok := s.Fleet.Prop(PropReadGenTop(primary))
	if !ok {
		return 0
	}
	a, g, found := strings.Cut(v, ":")
	if !found || a != itoa(attempt) {
		return 0
	}
	n, err := strconv.Atoi(g)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// readGenBound is the highest generation of the reader's read card identity that may exist
// at the attempt: the load window past the highest placed card (placedNext), or the
// primary's read top when it is higher. ReadCardExtras loads every generation through one
// past it, so the deal sees every card of the reader and the next identity is known absent.
func readGenBound(placedNext, top int) int { return max(placedNext+readCardLoadWindow, top) }

// readGenTops collects, for one plan, the read top each primary must hold for the cards
// the plan cuts (PropReadGenTop): a generation past the window and past the top it read.
type readGenTops map[string]struct{ attempt, gen int }

// cut notes the plan cuts the reader's id at the attempt; s is the snapshot the plan read.
func (t readGenTops) cut(s *Snapshot, primary string, attempt int, reader, id string) {
	g := readGenOf(id, ReadCardID(primary, attempt, reader))
	if g <= readCardLoadWindow || g <= readGenTop(s, primary, attempt) {
		return
	}
	if was, ok := t[primary]; ok && was.attempt == attempt && was.gen >= g {
		return
	}
	t[primary] = struct{ attempt, gen int }{attempt, g}
}

// writes is the property writes the plan carries for the tops it cut, guarded by the value read.
func (t readGenTops) writes(s *Snapshot) []PropWrite {
	var out []PropWrite
	for _, primary := range slices.Sorted(maps.Keys(t)) {
		v := t[primary]
		name := PropReadGenTop(primary)
		was, had := s.Fleet.Prop(name)
		out = append(out, PropWrite{Table: Fleet, Name: name, Value: itoa(v.attempt) + ":" + itoa(v.gen), Was: was, WasAbsent: !had})
	}
	return out
}

// The retired_by of a read card the read-card ask takes back: past its deadline, or its
// primary no longer in review at the card's attempt (reworked, its brief replaced, dropped,
// accepted); RetiredByReturned is a read handed back with no verdict (read --return).
const (
	RetiredByLate     = "late"
	RetiredByPrimary  = "primary"
	RetiredByReturned = "returned"
	// RetiredByAway is a read card the machine took back off a reader down, away or held,
	// or off a resting route (RetiredByRest): it spends nothing of the reader's.
	RetiredByAway = "away"
	RetiredByRest = "rest"
	// RetiredByCards is a readers-table read asked and not begun, taken back when read cards
	// came on: dealt again as a read card.
	RetiredByCards = "read cards"
	// RetiredByUnstarted is a read card left in ready past the deal bound: the machine's
	// take-back, spending nothing.
	RetiredByUnstarted = "unstarted"
	// RetiredByLost is a read card working on a row whose fresh live set has not named it
	// for LiveGrace (LiveReturns): no child of its member runs it. The machine takes it back
	// to the read deal, spending nothing of the reader's and marking no failure.
	RetiredByLost = "lost"
)

// isRead says the card is a read card.
func isRead(c *Card) bool { return c != nil && c.F("kind") == "read" }

// rowLoad is the work cards and the read cards on a fleet row, ready and working.
func rowLoad(s *Snapshot, row string) (work, reads int) {
	for _, col := range []string{Ready, Working} {
		for _, c := range s.Fleet.Cell(row, col) {
			if isRead(c) {
				reads++
			} else {
				work++
			}
		}
	}
	return work, reads
}

// rowWorking is the work cards and the read cards working on a fleet row.
func rowWorking(s *Snapshot, row string) (work, reads int) {
	for _, c := range s.Fleet.Cell(row, Working) {
		if isRead(c) {
			reads++
		} else {
			work++
		}
	}
	return work, reads
}

// ReadCardCounts is the epoch's read cards on the fleet table: ready, working, and done (ok
// or failed), where --json's read_cards.
type ReadCardCounts struct {
	Ready   int `json:"ready"`
	Working int `json:"working"`
	Done    int `json:"done"`
}

// RowCardFields is the fields of a row's counts (RowCardCounts), highest level first.
var RowCardFields = []string{"blocker_working", "critical_working", "fix_working", "high_working", "reads_working", "normal_working", "low_working", "reads_ready"}

// RowCardCounts is each fleet row's cards as the dashboard's segmented bar draws them,
// highest on the left: its working cards by level, a read card as reads whatever level it
// inherits and a work card at the level its deal wrote (QueuePriority), the fields
// <level>_working summing to its working (RowCardFields); and reads_ready, the read cards
// among its ready. A row with none is absent. all is the epoch's read cards.
func RowCardCounts(s *Snapshot) (rows map[string]map[string]int, all ReadCardCounts) {
	rows = map[string]map[string]int{}
	if s == nil || s.Fleet == nil {
		return rows, all
	}
	for _, row := range s.Fleet.Rows() {
		n := map[string]int{}
		for _, c := range s.Fleet.Cell(row, Working) {
			level := PriorityLadder[priorityRank(QueuePriority(c))]
			if isRead(c) {
				level, all.Working = "reads", all.Working+1
			}
			n[level+"_working"]++
		}
		for _, c := range s.Fleet.Cell(row, Ready) {
			if isRead(c) {
				n["reads_ready"]++
				all.Ready++
			}
		}
		for _, col := range []string{DoneOK, DoneFailed} {
			for _, c := range s.Fleet.Cell(row, col) {
				if isRead(c) {
					all.Done++
				}
			}
		}
		if len(n) > 0 {
			rows[row] = n
		}
	}
	return rows, all
}

// halfLoad is a load counted in slots with a read at half a slot, rounded up: the room a
// work card needs is whole.
// tla/DirtyTick.tla Load, Room and ReadFits (ReadCost "half")
func halfLoad(work, reads int) int { return work + (reads+1)/2 }

// readUnit is one unit the read-card ask may deal a read to: a friend's row or a member's.
type readUnit struct {
	name   string // the reader: her name, or the member's
	row    string // the fleet row
	friend bool
	seat   FriendSeat
	half   int // free room, in half slots: twice its room less twice its work and its reads
	// idle is its free lanes, in half slots: twice its width less twice its work cards and
	// its read cards, ready and working (rowLoad): a lane promised to a queued card is not
	// free (the owner, 2026-10-11: "cards only may accrue per-fleet or friend, if there are
	// not other machines/friends ready to do that work right now!"; tla/WhoPreference.tla,
	// DealFreeLanesFirst)
	idle int
}

// RoleReader is the role a friend's nova-config row names to be dealt read cards; a machine
// reads when its reader row (reader-<m>) is on the readers table, the machine's reader
// identity, whose tiers cell names the tiers it reads (reader set --tiers).
const RoleReader = "reader"

// readUnitsOf is every unit up that may be dealt a read: the friends dealable (friendDealable)
// whose roles name reader, then the members up whose reader row is neither held nor
// retired, in row order; the ask orders them together (readCardsAsk).
func readUnitsOf(s *Snapshot, seats []FriendSeat) []readUnit {
	var out []readUnit
	for _, f := range seats {
		if !friendCanRead(s, f) || !friendHasRole(f, RoleReader) {
			continue
		}
		g := f
		g.ReadsFirst = 0
		_, width := friendRoom(g)
		row := FriendRow(f.Name)
		work, reads := rowLoad(s, row)
		// a friend's read counts against her width as a work card does
		// (friendLiveLanes, friendFreeLanes): her free lanes are what the read ask
		// may fill, so no read is dealt to a friend at her width
		free := friendFreeLanes(width, work, reads)
		out = append(out, readUnit{name: f.Name, row: row, friend: true, seat: f, half: free, idle: free})
	}
	for _, m := range notQuiet(s, s.UpMembers()) { // a member quiet, under its floor or draining reads nothing (drain.go)
		if !memberReads(s, m) {
			continue
		}
		w := s.Width(m)
		work, reads := rowLoad(s, m)
		out = append(out, readUnit{name: m, row: m, half: 2*(DealAhead*w-work) - reads, idle: 2*w - 2*work - reads})
	}
	return out
}

// memberReads says the member has the reader role: its reader row (reader-<m>) is on the
// readers table and is neither held nor retired.
func memberReads(s *Snapshot, m string) bool {
	rd := ReaderPrefix + m
	if s.Readers == nil || !s.Readers.HasRow(rd) {
		return false
	}
	if s.ReaderStates != nil {
		if st := s.ReaderStates[rd]; st == ReaderHeld || st == ReaderRetired {
			return false
		}
	}
	return true
}

// attemptUnit is the unit that worked the primary's attempt: the friend's name or the
// member's, "" when it is not known. A unit never reads its own work.
func attemptUnit(s *Snapshot, primary string, attempt int) string {
	if f := attemptWorker(s, primary, attempt); f != "" {
		return f
	}
	if s == nil || s.Fleet == nil {
		return ""
	}
	if wc := s.Fleet.Card(WorkCardID(primary, attempt)); wc != nil {
		return cmp.Or(wc.Row, wc.F("member"))
	}
	return ""
}

// spentBy is the retired_by of a read card its reader itself ended: it closed it (read) or
// handed it back (returned), or let it pass its deadline (late). A card the machine took
// back (a reader down, away or held, a restart, the coordinator's hold --return, its
// primary moved) spends nothing: its reader may be dealt the read again at the attempt.
var spentBy = []string{"read", RetiredByReturned, RetiredByLate}

// readerCardsAt is the reader's read cards of the primary's attempt, placed or kept, every
// generation (cards: the primary's read cards, fleetReadIndex).
func readerCardsAt(cards []*Card, primary string, attempt int, reader string) []*Card {
	var out []*Card
	for _, c := range cards {
		p, a, rd, ok := ParseReadCard(c.ID)
		if ok && p == primary && a == attempt && rd == reader {
			out = append(out, c)
		}
	}
	return out
}

// readSpent says the reader holds or closed a read card of the attempt: one placed, or one
// retired with a verdict or by itself (spentBy). It is never dealt that read again. A read
// card the machine withdrew (FriendTake for a friend's hold or stall) is history and spends
// nothing; one the seat or her runner handed back (friend take, FriendTakeReq.Spends) is
// withdrawn with retired_by returned and spends her.
func readSpent(cards []*Card) bool { return readSpender(cards) != nil }

// readSpender is the first card that spends its reader (readSpent), nil for none.
func readSpender(cards []*Card) *Card {
	for _, c := range cards {
		if c.Placed() && c.Col != Withdrawn || c.F("verdict") != "" || slices.Contains(spentBy, c.F("retired_by")) {
			return c
		}
	}
	return nil
}

// readGenOf is the generation an id names of the reader's plain read card identity (base):
// 0 for the plain id, n for its .g<n> generation, -1 for another card. The plain identity
// is generation 0; each take-back the read index holds is a further generation.
func readGenOf(id, base string) int {
	id = CardID(id)
	if id == base {
		return 0
	}
	if strings.HasPrefix(id, base+".g") {
		if n, err := strconv.Atoi(id[len(base)+2:]); err == nil && n >= 1 {
			return n
		}
	}
	return -1
}

// ReadCardGenIDs is the identities one reader's read card of an attempt may have: the plain
// one (generation 0) and its generations through gen. With no gen named it is the plain
// identity and the second (ReadCardSecondID), as a re-asked friend read is named; a caller
// that holds the read index names the generation after the highest it holds (readNextGen;
// ReadCardExtras and friendReadCardOf do).
func ReadCardGenIDs(primary string, attempt int, reader string, gen ...int) []string {
	id := ReadCardID(primary, attempt, reader)
	n := 1
	if len(gen) > 0 {
		n = max(gen[0], 0)
	}
	out := []string{id}
	for i := 1; i <= n; i++ {
		out = append(out, id+".g"+itoa(i))
	}
	return out
}

// readNextGen is the generation after the highest the read index holds for one reader's read
// card identity (readGenOf): 0 when it holds none. The read-card deal never caps the
// generation, so a reader whose read the machine took back is dealt it again at a fresh one
// (the owner, 2026-10-06: "I don't want any serial reads again"; a read machine-taken-back
// spends no reader, spentBy). tla/ReadCards.tla Deal.
func readNextGen(cards []*Card, primary string, attempt int, reader string) int {
	base := ReadCardID(primary, attempt, reader)
	next := 0
	for _, c := range cards {
		if g := readGenOf(c.ID, base); g >= next {
			next = g + 1
		}
	}
	return next
}

// readCardIDFor is the id of the reader's next read card of the attempt: the generation
// after the highest the read index holds (readNextGen), the plain identity when it holds
// none. It never runs out, so a reader whose read the machine took back is dealt it again
// at a fresh generation (the owner, 2026-10-06: "I don't want any serial reads again"; a
// read machine-taken-back spends no reader, spentBy). tla/ReadCards.tla Deal.
// It deals the next generation only when it lies inside the loaded window (readCardLoadWindow)
// and the snapshot holds no record of it; past the window it returns "" (readRefusal answers
// "read generations past the load window").
func readCardIDFor(cards []*Card, primary string, attempt int, reader string, top int) string {
	base := ReadCardID(primary, attempt, reader)
	next := 0
	placedNext := 0
	for _, c := range cards {
		if c == nil {
			continue
		}
		if g := readGenOf(c.ID, base); g >= 0 {
			if g >= next {
				next = g + 1
			}
			if c.Placed() && g >= placedNext {
				placedNext = g + 1
			}
		}
	}
	if next > readGenBound(placedNext, top)+1 {
		return ""
	}
	id := base
	if next > 0 {
		id = base + ".g" + itoa(next)
	}
	for _, c := range cards {
		if c != nil && c.ID == id {
			return ""
		}
	}
	return id
}

// ReadCardExtras is the fleet table's read card ids a step that judges reads must read as
// records, placed or kept (a store's snapshot holds only the placed cards and the records a
// step names): every identity of a read card of each primary in review, at its attempt, of
// every reader the fleet table has a row for (a friend, by her name; a member with its
// reader row).
func ReadCardExtras(s *Snapshot) []string {
	if s == nil || s.Work == nil || s.Fleet == nil {
		return nil
	}
	var out []string
	for _, c := range s.Work.Column(Review) {
		attempt := readAttempt(c)
		for _, row := range s.Fleet.Rows() {
			name, ok := FriendOfRow(row)
			if !ok {
				if s.Readers == nil || !s.Readers.HasRow(ReaderPrefix+row) {
					continue // a member with no reader row is dealt no read
				}
				name = row
			}
			// the identities the store must read are those the read index holds and the
			// generation after the highest of them, the one readCardIDFor deals next: no
			// reader is ever refused "no id", however often its read was taken back
			next := readNextGen(s.Fleet.Cards(), c.ID, attempt, name)
			for _, id := range ReadCardGenIDs(c.ID, attempt, name, readGenBound(next, readGenTop(s, c.ID, attempt))+1) {
				if s.Fleet.Placed(id) == nil {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// mayReadCard says the unit is eligible for a read of the primary at the attempt: its tier
// set serves it, it did not work the attempt, it did not refuse staging, and it holds no read
// card of the attempt and closed none (readSpent; readCardsAskFull checks room separately,
// docs/SPEC-SPRINT.md section 1, the read-card deal).
//
// A unit reads at the read's tier or above, never below it (readTierOf; the owner,
// 2026-10-16: "It is completely undefined that a card should run at the wrong tier"); the
// deal orders the units that may by tier distance (readTierDistance), the nearest first.
func mayReadCard(s *Snapshot, u readUnit, pr *Card, attempt int, worker string, cards []*Card) bool {
	return readRefusal(s, u, pr, attempt, worker, cards) == ""
}

// readRefusal is the clause of mayReadCard that refuses the unit the read, "" when none
// does: worker, spent, no id, staging refusal, friends' set, friend tier, reader row (a
// readers-table read of the attempt), fleet set, reader tier, and for a member every route
// of the read's tier resting (ReadRoutesRest; docs/SPEC-SPRINT.md section 1, the read-card
// deal).
func readRefusal(s *Snapshot, u readUnit, pr *Card, attempt int, worker string, cards []*Card) string {
	mine := readerCardsAt(cards, pr.ID, attempt, u.name)
	switch {
	case u.name == worker:
		return "worker"
	case readSpent(mine):
		c := readSpender(mine)
		return "spent(" + c.ID[strings.LastIndex(c.ID, ".")+1:] + " col=" + c.Col + " verdict=" + c.F("verdict") + " retired_by=" + c.F("retired_by") + " head=" + c.F("head") + ")"
	case readCardIDFor(mine, pr.ID, attempt, u.name, readGenTop(s, pr.ID, attempt)) == "":
		return "read generations past the load window"
	}
	if !u.friend {
		for _, c := range mine {
			if slices.Contains(StagingRefusers(c), u.name) {
				return "staging refusal"
			}
		}
	}
	if u.friend {
		// a friend reads her tier or any below it, as her reads always did: a heavy friend
		// reads a flash, a pro and a heavy card, never a frontier one (friendAtOrAbove)
		t := s.readTierOf(pr)
		if !s.FriendsTake(t) {
			return "friends' set" // the friends' tiers leave the read out (set --friends-tiers)
		}
		if friendAtOrAbove(s, u.seat, t) {
			return ""
		}
		return "friend tier"
	}
	// a machine whose reader row holds a read of the attempt on the readers table (asked
	// the old way, before read cards were on) reads it there, never twice
	rd := ReaderPrefix + u.name
	for _, id := range ReadCardIDs(pr.ID, attempt, rd) {
		if c := s.Readers.Card(id); c.Placed() || c != nil && c.F("verdict") != "" {
			return "reader row"
		}
	}
	t := s.readTierOf(pr)
	if !s.FleetTakes(t) {
		return "fleet set" // the fleet's tiers leave the read out (set --fleet-tiers)
	}
	if !s.readerServesTier(rd, t) {
		return "reader tier"
	}
	if s.readRoutesAllRest(pr) {
		return ReadRoutesRest // dealt, it would be taken back off the resting route at once
	}
	return ""
}

// readTierDistance is how far above the read's tier (readTierOf) the unit reads, the
// cheaper first: 0 at the read's own tier, k at k tiers above; a unit that reads only below
// it is past every distance (readRefusal refuses it: a card is never read below its tier).
// A friend reads at the nearest of her tiers at or above it (friendTiers); a member at the
// read's tier, the tier of the route its read is drawn on (readRouteOf).
func readTierDistance(s *Snapshot, u readUnit, pr *Card) int {
	want := slices.Index(capLadder, s.readTierOf(pr))
	gap := func(at int) int {
		if want < 0 || at < want {
			return 2*len(capLadder) + 1
		}
		return at - want
	}
	if !u.friend {
		if !s.readerServesTier(ReaderPrefix+u.name, s.readTierOf(pr)) {
			return gap(-1)
		}
		return 0
	}
	best := gap(-1)
	for _, x := range friendTiers(u.seat) {
		best = min(best, gap(slices.Index(capLadder, x)))
	}
	return best
}

// readCardsStanding is the primary's reads that stand at its attempt, both tables: the
// fleet's read cards placed or retired with a verdict (fleetReadLive), and the readers
// table's live reads (the readers table's reads asked before the setting, which close as
// they always did); broken says one of them found it broken.
func readCardsStanding(s *Snapshot, pr *Card, idx map[string][]*Card) (standing int, broken bool) {
	var placed, oks, brk []*Card
	if idx != nil {
		placed, oks, brk = fleetReadLiveOf(s, pr, idx[pr.ID])
	} else {
		placed, oks, brk = friendReadLive(s, pr)
	}
	standing = len(placed) + len(oks) + len(brk)
	broken = len(brk) > 0
	if s.Readers != nil {
		for _, rc := range liveReadsAt(s, pr, readAttempt(pr)) {
			standing++
			broken = broken || rc.Col == Broken
		}
	}
	return standing, broken
}

// readCardsWanted is how many read cards the deal cuts for the primary now: every read its
// attempt still needs (ReadsNeededIn less the reads that stand), at once; none once a read
// found it broken (its judgment and the rework follow), none for failed work; over a fleet
// read index (fleetReadIndex), nil to read the table. The owner, 2026-10-11 ~19:25Z:
// "Multiple reads now go out in ||"; "reads should keep going, every second, even if there
// is no capacity. It repeats over and over until there is"; 2026-10-06: "I don't want any
// serial reads again. That was silly." tla/ReadCards.tla (read) Deal.
func readCardsWanted(s *Snapshot, pr *Card, idx map[string][]*Card) int {
	if pr == nil || pr.Col != Review || IsSentinel(pr) || pr.F("result") == "failed" || readBranchMissingOpen(s, pr) {
		return 0
	}
	standing, broken := readCardsStanding(s, pr, idx)
	if broken {
		return 0
	}
	return max(0, ReadsNeededIn(s, pr)-standing)
}

// readCardsWaiting is every primary in review that wants read cards now, by the level of
// its reads (readOrder), work order within one.
func readCardsWaiting(s *Snapshot, idx map[string][]*Card) []*Card {
	var out []*Card
	for _, pr := range s.Work.Column(Review) {
		if readCardsWanted(s, pr, idx) > 0 {
			out = append(out, pr)
		}
	}
	return readOrder(out)
}

// readCardsTakeBack is the read cards the ask retires before it asks: each placed read card
// past its deadline (RetiredByLate), and each whose primary is no longer in review at the
// card's attempt (RetiredByPrimary), by primary.
//
// The readers table's reads asked the old way and not begun (asked, or handed back) are
// taken back too, once read cards are on: they are dealt again as read cards, and a read
// begun there finishes there and stands (readCardsStanding), so turning read cards on reads
// no primary twice.
func readCardsTakeBack(s *Snapshot) map[string][]Change {
	out := map[string][]Change{}
	if s.Readers != nil {
		for _, c := range s.Readers.Column(Asked) {
			out[c.F("primary")] = append(out[c.F("primary")], change(Readers, removeEntry(c, map[string]string{"retired": stamp(s.Now), "retired_by": RetiredByCards})))
		}
	}
	for _, c := range s.Fleet.Column(Ready, Working) {
		if !isRead(c) {
			continue
		}
		by := ""
		pr := s.Work.Placed(c.F("primary"))
		switch {
		case pr == nil || pr.Col != Review && !DecisionParkedReview(pr) || readAttempt(pr) != c.Int("attempt"):
			by = RetiredByPrimary
		case c.F(FieldReadCard) != "" && c.Col == Working && !readStart(c).IsZero() && !s.Now.Before(readStart(c).Add(ReadCardDeadline)):
			by = RetiredByLate
		case c.F(FieldReadCard) != "" && c.Col == Ready && !s.Now.Before(stampAt(c, "asked").Add(s.DealtMax())):
			by = RetiredByUnstarted
		case c.Col == Ready && func() bool { _, ok := cardRest(s, c); return ok }():
			by = RetiredByRest
		default:
			continue
		}
		out[c.F("primary")] = append(out[c.F("primary")], change(Fleet, removeEntry(c, map[string]string{"retired": stamp(s.Now), "retired_by": by})))
	}
	return out
}

// ReadRoutesRest is readRefusal's clause for a fleet reader while every route of the read's
// tier rests (readRoutesAllRest): the read waits for a route, cutting no generation.
const ReadRoutesRest = "every route of its read tier rests"

// NoReaderMayRead begins the wait of a primary no unit up may be dealt its read card: the
// read-card ask raises cannot ask on it (readCardsAskPart).
const NoReaderMayRead = "no reader up may read it"

// readCardsAsk is the read-card ask: the cards it takes back (readCardsTakeBack), then for
// every primary that wants reads (readCardsWaiting) every read it wants, at once, each to a
// different unit that may read it (mayReadCard) with half a slot free, the cheapest first:
// a unit with an idle lane before every unit with none (a read waits in a ready queue only
// when every reader that may take it is busy), then by tier distance (readTierDistance: the
// read's own tier, then the tiers above, nearest first; never one below), then the persisted
// ring cursor (read_deal_index_<tier>, docs/SPEC-SPRINT.md section 1, the read-card deal)
// round every unit with a free lane, a full or ineligible reader skipped; friends and
// members alike. A free lane counts the unit's queue (readUnit.idle: ready and working),
// so a read waits in a ready queue only when no unit that may take it has a free lane. waits
// is why
// each primary that wants more reads than it was dealt waits. With ri nil the reads draw
// no route (the deal's dry run: what the reads would take).
func readCardsAsk(s *Snapshot, seats []FriendSeat, ri routeIndexes) (p Plan, waits map[string]string) {
	return readCardsAskWhy(s, seats, ri, nil)
}

// ReadCardsWhy is tick --shadow's account of the read-card ask (readCardsAsk): first the
// units it deals reads to, each with its half slots and idle lanes, and the reader friends
// it leaves out; then a line per primary that waits, its read tier, the reads it wants, why
// it waits and each unit not dealt it with the clause that refuses it (readRefusal), or
// half<=0 for one at its room as the ask reached the primary. Nothing with read cards off.
func ReadCardsWhy(s *Snapshot, seats []FriendSeat) []string {
	if !s.ReadCardsOn() {
		return nil
	}
	if seats == nil {
		seats = withoutDirs(s.Friends)
	}
	var why []string
	readCardsAskWhy(s, seats, nil, &why)
	return why
}

// readCardsAskWhy is readCardsAsk, and with why non-nil, ReadCardsWhy's lines.
func readCardsAskWhy(s *Snapshot, seats []FriendSeat, ri routeIndexes, why *[]string) (p Plan, waits map[string]string) {
	return readCardsAskFull(s, seats, ri, why, nil)
}

// readCardsAskFull is readCardsAskWhy, and with full non-nil, for each primary that waits because
// every unit that may read it is at its room, the names of those units (the frontier-room
// judgment's cause, frontierRoomCondsOfWaits). It advances and writes one guarded fleet-table
// cursor per read tier with the cards it deals (docs/SPEC-SPRINT.md section 1, the read-card
// deal).
func readCardsAskFull(s *Snapshot, seats []FriendSeat, ri routeIndexes, why *[]string, full map[string][]string) (p Plan, waits map[string]string) {
	return readCardsAskSelected(s, seats, ri, why, full, nil)
}

// readCardsAskSelected uses the same deal rule for a coordinator ask of named primaries.
func readCardsAskSelected(s *Snapshot, seats []FriendSeat, ri routeIndexes, why *[]string, full map[string][]string, only map[string]bool) (p Plan, waits map[string]string) {
	waits = map[string]string{}
	if s == nil || s.Work == nil || s.Fleet == nil {
		return p, waits
	}
	back := readCardsTakeBack(s)
	if only != nil {
		for id := range back {
			if !only[id] {
				delete(back, id)
			}
		}
	}
	// the cards taken back stand no more: the wants are counted without them
	view := s
	if len(back) > 0 {
		v := *s
		v.Fleet, v.Readers = s.Fleet.Frozen(), s.Readers.Frozen()
		for _, chs := range back {
			for _, ch := range chs {
				tb := v.T(ch.Table)
				c := *tb.Card(ch.Entry.ID)
				c.Row, c.Col = "", ""
				c.Fields = maps.Clone(c.Fields)
				maps.Copy(c.Fields, ch.Entry.Set) // retired_by: whether it spent its reader
				tb.Put(&c)
			}
		}
		view = &v
	}
	// the read's route is never a resting one (readRouteOf, s.resting): settle the
	// routes' rests here, once for the whole ask, when the snapshot holds none, so no
	// read scans the fleet table (docs/SPEC-SPRINT.md section 6, the reads)
	if view.rests == nil {
		view, _ = view.withRests()
	}
	units := readUnitsOf(view, seats)
	idx := fleetReadIndex(view)
	unitByName := make(map[string]int, len(units))
	for i, u := range units {
		unitByName[u.name] = i
	}
	readRounds := map[string]*round{}
	tops := readGenTops{}
	if why != nil {
		var us, out []string
		for _, u := range units {
			us = append(us, u.name+" half="+itoa(u.half)+" idle="+itoa(u.idle))
		}
		for _, f := range seats {
			if friendHasRole(f, RoleReader) && !friendCanRead(view, f) {
				out = append(out, f.Name+" ("+f.Status+")")
			}
		}
		*why = append(*why, "units: "+strings.Join(us, ", ")+"; reader friends not dealable: "+strings.Join(out, ", "))
	}
	declared := map[string]bool{}
	done := map[string]bool{}
	for _, pr := range readCardsWaiting(view, idx) {
		if only != nil && !only[pr.ID] {
			continue
		}
		done[pr.ID] = true
		want := readCardsWanted(view, pr, idx)
		attempt := readAttempt(pr)
		worker := attemptUnit(view, pr.ID, attempt)
		cards := idx[pr.ID]
		var may []int
		resting := false
		for i, u := range units {
			r := readRefusal(view, u, pr, attempt, worker, cards)
			if r == "" {
				may = append(may, i)
			} else if r == ReadRoutesRest {
				resting = true
			} else if r == "read generations past the load window" {
				if !slices.ContainsFunc(p.Refused, func(ref Refusal) bool {
					return ref.Key == u.name && ref.Why == r
				}) {
					p.refuse(u.name, r)
				}
			}
		}
		tier := view.readTierOf(pr)
		cursor, ok := readRounds[tier]
		if !ok {
			cursor = readDealRound(view, tier, units)
			readRounds[tier] = cursor
		}
		var picked []int
		chosen := map[int]bool{}
		for len(picked) < want {
			// an idle lane before a busy one: a read waits in a ready queue only when
			// every reader that may take it is busy (docs/SPEC-SPRINT.md section 1, the
			// read-card deal)
			hasIdle := false
			for _, i := range may {
				if !chosen[i] && units[i].half > 0 && units[i].idle > 0 {
					hasIdle = true
					break
				}
			}
			// the cheapest tier first (readTierDistance), then the persisted ring cursor
			// (read_deal_index_<tier>) round every unit with a free lane, never the most
			// idle and never the name: a burst of reads rolls round the free lanes of the
			// whole fleet, a narrow member's too, never past a reader's room (the owner,
			// 2026-10-11: "Deal logic should be using ((index++) % n) to roll around, not
			// hitting the same fleet machine or friend each deal"; tla/WhoPreference.tla,
			// DealFreeLanesFirst).
			bestDistance := 2*len(capLadder) + 1
			for _, i := range may {
				if !chosen[i] && units[i].half > 0 && (!hasIdle || units[i].idle > 0) {
					bestDistance = min(bestDistance, readTierDistance(view, units[i], pr))
				}
			}
			eligible := func(name string) bool {
				i, exists := unitByName[name]
				return exists && !chosen[i] && units[i].half > 0 && (!hasIdle || units[i].idle > 0) && mayReadCard(view, units[i], pr, attempt, worker, cards) && readTierDistance(view, units[i], pr) == bestDistance
			}
			name := cursor.scan(eligible)
			if name == "" {
				break
			}
			i := unitByName[name]
			picked = append(picked, i)
			chosen[i] = true
			cursor.moved(name)
		}
		u := Unit{Key: pr.ID, Stream: pr.Row, Changes: back[pr.ID]}
		delete(back, pr.ID)
		if len(picked) < want {
			switch {
			case len(may) == 0 && resting:
				// the route's rest is noted once by itself (NRouteRested): no cannot-ask
				// judgment for a read that waits only for its route to serve
				waits[pr.ID] = ReadRoutesRest
			case len(may) == 0:
				waits[pr.ID] = NoReaderMayRead + ": no friend whose tiers reach its read tier, and no member whose reader row serves its tier, besides its own worker"
			default:
				waits[pr.ID] = "every reader up who may read it is at its room"
				if full != nil {
					for _, i := range may {
						if units[i].half <= 0 {
							full[pr.ID] = append(full[pr.ID], units[i].name)
						}
					}
				}
			}
			if why != nil {
				var refused []string
				for i, un := range units {
					r := readRefusal(view, un, pr, attempt, worker, cards)
					if r == "" && un.half <= 0 {
						r = "half<=0"
					}
					if r != "" && !slices.Contains(picked, i) {
						refused = append(refused, un.name+"="+r)
					}
				}
				*why = append(*why, pr.ID+" tier="+view.readTierOf(pr)+" wants="+itoa(want)+" waits: "+waits[pr.ID]+"; refused: "+strings.Join(refused, ", "))
			}
		}
		if len(picked) == 0 {
			if len(u.Changes) > 0 {
				u.Moved = pr.ID + ": its read cards taken back"
				p.Units = append(p.Units, u)
			}
			continue
		}
		standing, _ := readCardsStanding(view, pr, idx)
		branch, start, head := attemptEnds(view, pr.ID, attempt)
		head = cmp.Or(head, pr.F("head"))
		var names []string
		for k, i := range picked {
			un := &units[i]
			un.half--
			un.idle--
			id := readCardIDFor(cards, pr.ID, attempt, un.name, readGenTop(view, pr.ID, attempt))
			if id == "" || (view != nil && view.Fleet != nil && view.Fleet.Card(id) != nil) {
				continue
			}
			tops.cut(view, pr.ID, attempt, un.name, id)
			fields := map[string]string{
				"kind": "read", "primary": pr.ID, "stream": pr.Row, "reader": un.name,
				"attempt": itoa(attempt), "head": head, "asked": stamp(s.Now), "gen": "1",
				FieldReadCard: "1",
			}
			if branch != "" {
				fields["branch"] = branch
			}
			if start != "" {
				fields["start"] = start
			}
			priorityOnRead(fields, pr)
			col := Ready
			if un.friend {
				// a friend's read is hers on her row, working once she has a lane free, as
				// her reads were (askOneFriend); she brings her own model
				fields[FieldTier] = view.readTierOf(pr)
				if un.idle >= 0 {
					col = Working
				}
				if !view.Fleet.HasRow(un.row) && !declared[un.row] {
					p.Rows = append(p.Rows, RowAdd{Fleet, un.row})
					declared[un.row] = true
				}
			} else {
				if ri != nil {
					maps.Copy(fields, view.readRouteOf(ri, pr, nil))
				} else {
					fields[FieldTier] = view.readTierOf(pr)
				}
				// the first read of a flash card's attempt is a decide read, as the readers'
				// ask places it (decideFields)
				maps.Copy(fields, view.decideFields(pr, standing == 0 && k == 0))
			}
			u.Changes = append(u.Changes, change(Fleet, createEntry(id, un.row, col, pr.Score, fields)))
			names = append(names, un.name)
		}
		u.Moved = pr.ID + " asked of " + strings.Join(names, ", ") + " (read cards)"
		u.Closes = closesFor(s.Open, []string{NCannotAsk, NStranded, NStalled}, pr.ID)
		u.Closes = append(u.Closes, closesFor(s.Acked, []string{NCannotAsk}, pr.ID)...)
		// TickDeal applies before the ask part: preserve its eligible units in the answer.
		for _, o := range u.Closes {
			if o.Note.Type == NCannotAsk {
				u.Notes = append(u.Notes, decided(o, "a reader may read "+pr.ID+" now ("+strings.Join(names, ", ")+")", MachineActor, s.Now, pr.ID))
			}
		}
		p.Units = append(p.Units, u)
	}
	if why != nil {
		for _, pr := range view.Work.Column(Review) {
			if !IsSentinel(pr) && readCardsWanted(view, pr, idx) == 0 && !acceptable(view, pr) {
				*why = append(*why, pr.ID+" wants 0: "+readsWantZeroWhy(view, pr, idx))
			}
		}
	}
	// the cards taken back whose primary asks nothing now
	for _, key := range slices.Sorted(maps.Keys(back)) {
		if done[key] {
			continue
		}
		p.Units = append(p.Units, Unit{Key: key, Stream: cmp.Or(fieldOf(s.Work.Card(key), "stream"), ""), Changes: back[key], Moved: key + ": its read cards taken back"})
	}
	for _, tier := range slices.Sorted(maps.Keys(readRounds)) {
		r := readRounds[tier]
		if r.value() == r.read {
			continue
		}
		p.Props = append(p.Props, PropWrite{Table: r.table, Name: r.name, Value: r.value(), Was: r.read, WasAbsent: !r.had})
	}
	p.Props = append(p.Props, tops.writes(view)...)
	return Lawful(p), waits
}

// fieldOf is the card's field, "" for no card.
func fieldOf(c *Card, name string) string {
	if c == nil {
		return ""
	}
	if name == "stream" && c.Row != "" {
		return c.Row
	}
	return c.F(name)
}

// readCardsAskPart is the tick's ask while read cards are on (friendAskPart): the deal
// deals the read cards (TickDeal, withReadCards), and the ask asks nothing; it writes the
// waiting mark on each primary that wants more reads than it holds (markWaiting), counted
// due so the no-stall rule holds it, clears the mark of one that waits no more, and keeps
// the readers' standing judgments: the readers behind and the read tier to raise; cannot
// ask and fewer than two readers up close.
func readCardsAskPart(s *Snapshot, r TickReq, seats []FriendSeat) (Plan, int) {
	var p Plan
	full := map[string][]string{}
	_, waits := readCardsAskFull(s, seats, nil, nil, full)
	for _, c := range s.Work.Column(Review) {
		if _, wait := waits[c.ID]; !wait && c.F(FieldWaitingReader) != "" {
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, setEntry(c, nil, FieldWaitingReader))},
				Moved: c.ID + " waits for a reader no more"})
		}
	}
	markWaiting(&p, s, map[string]int{}, waits)
	var conds []cond
	if s.Readers != nil {
		conds = append(conds, readersBehindCond(s)...)
	}
	conds = append(conds, raiseReadTierConds(s)...)
	conds = append(conds, readsWindowConds(s, r)...)
	// a read no unit up may take is cannot ask: one judgment an episode, the primary in
	// review waiting for a reader at one attempt (cannotAskEpisodes)
	refused, held, ended := cannotAskEpisodes(s, r, waits)
	view := *s
	view.Open = append(slices.Clone(s.Open), held...)
	ending := func(o Open) bool {
		return !slices.ContainsFunc(refused, func(f Refusal) bool { return f.Key == o.Subject() }) && slices.ContainsFunc(ended, func(e Open) bool { return e.Key == o.Key })
	}
	view.Open = slices.DeleteFunc(view.Open, ending)
	view.Acked = slices.DeleteFunc(slices.Clone(s.Acked), ending)
	conds = append(conds, cannotAskCond(&view, refused)...)
	// notify already reads acknowledgements; do not present held waits twice.
	view.Open = slices.DeleteFunc(slices.Clone(s.Open), ending)
	conds = append(conds, frontierRoomCondsOfWaits(s, waits, full)...)
	p.Closes = append(p.Closes, ended...)
	// Eligibility is independent of free room. Record the units in the closing answer.
	units := readUnitsOf(s, seats)
	idx := fleetReadIndex(s)
	for _, c := range s.Work.Column(Review) {
		var names []string
		for _, un := range units {
			if mayReadCard(s, un, c, max(c.Int("attempt"), 1), attemptUnit(s, c.ID, readAttempt(c)), idx[c.ID]) {
				names = append(names, un.name)
			}
		}
		if len(names) == 0 {
			continue
		}
		for _, o := range append(slices.Clone(s.Open), s.Acked...) {
			if o.Note.Type == NCannotAsk && o.Subject() == c.ID {
				p.Notes = append(p.Notes, decided(o, "a reader may read "+c.ID+" now ("+strings.Join(names, ", ")+")", MachineActor, s.Now, c.ID))
			}
		}
	}
	due := notify(&p, &view, conds, []string{NCannotAsk, NFewReaders, NReadersBehind, NRaiseReadTier, NBrokenReadsOutrun, NReaderBreaks, NNoFrontierRoom}, r)
	return p, due + len(waits)
}

// cannotAskEpisodes implements SprintRules.EpTick and CannotAskClosedWhenEligible.
// NoReaderMayRead holds one judgment while no eligible unit exists at the attempt;
// room is a different wait and ends that episode on this tick. An eligibility change
// back to no reader can start one new episode, never another on each unchanged tick.
func cannotAskEpisodes(s *Snapshot, r TickReq, waits map[string]string) (refused []Refusal, held, ended []Open) {
	on := map[string][]Open{} // the primary's open cannot-ask judgments and live waits
	for _, o := range s.Open {
		if o.Note.Type == NCannotAsk && !o.Note.StreamLevel {
			on[o.Subject()] = append(on[o.Subject()], o)
		}
	}
	for _, o := range s.Acked {
		if o.Note.Type != NCannotAsk || o.Note.StreamLevel {
			continue
		}
		if !o.Note.Review.IsZero() {
			base := o.Note.ReviewSet // as notify bases a wait
			if base.IsZero() {
				base = o.Note.At
			}
			if DueNow(s.Now, o.Note.Review, base, r.Stopped) {
				continue // run out: notify closes it, and raises it again while no unit may read it
			}
		}
		on[o.Subject()] = append(on[o.Subject()], o)
		held = append(held, o)
	}
	for _, id := range slices.Sorted(maps.Keys(waits)) {
		if !strings.HasPrefix(waits[id], NoReaderMayRead) {
			ended = append(ended, on[id]...)
			continue // eligible readers at room end cannot ask too
		}
		if c := s.Work.Placed(id); c != nil && len(on[id]) > 0 && c.F(FieldWaitingReader) != "" && waitingAt(c) != max(c.Int("attempt"), 1) {
			// its attempt moved on: the judgment is of the episode before, ended here (the
			// primary stays a subject this tick, so notify neither closes it twice nor
			// raises the next episode before it is closed)
			ended = append(ended, on[id]...)
		}
		refused = append(refused, Refusal{Key: id, Why: waits[id]})
	}
	held = slices.DeleteFunc(held, func(o Open) bool { return slices.ContainsFunc(ended, func(e Open) bool { return e.Key == o.Key }) })
	return refused, held, ended
}

// readCardsWaitingCount is the reads waiting while read cards are on (ReadsWaiting): the
// reads wanted and not yet dealt (readCardsWanted), and the read cards dealt and not
// started (ready on a row).
func readCardsWaitingCount(s *Snapshot) int {
	if s == nil || s.Work == nil || s.Fleet == nil {
		return 0
	}
	idx := fleetReadIndex(s)
	n := 0
	for _, pr := range s.Work.Column(Review) {
		n += readCardsWanted(s, pr, idx)
	}
	for _, c := range s.Fleet.Column(Ready) {
		if isRead(c) {
			n++
		}
	}
	return n
}

// withReadCards is the read-card deal (TickDeal): it deals every read the primaries in
// review want (readCardsAsk), advances a persisted fleet-table cursor per read tier, and
// places the reads before work so the room the deal counts holds them at half a slot each
// (docs/SPEC-SPRINT.md section 1, the read-card deal; README.md, free capacity gets another
// ready card). Its snapshot includes the cards it places and takes back, which the work of
// the same deal is dealt on. With read cards off, nothing and the snapshot as it is.
// tla/ReadCards.tla Deal
// tla/DirtyTick.tla Pump (reads before work under ReadCost "half"; ReadsBeforeWork)
func (s *Snapshot) WithReadCards(seats []FriendSeat) (*Snapshot, Plan) {
	return s.withReadCards(seats)
}

func (s *Snapshot) withReadCards(seats []FriendSeat) (*Snapshot, Plan) {
	if !s.ReadCardsOn() || s.Fleet == nil {
		return s, Plan{}
	}
	if seats == nil {
		seats = s.Friends
	}
	var ri routeIndexes
	if len(s.Routes) > 0 {
		ri = routeIndexesOf(s)
	}
	p, _ := readCardsAsk(s, seats, ri)
	if len(p.Units) == 0 {
		return s, p
	}
	v := *s
	v.Fleet = s.Fleet.Frozen()
	for _, ra := range p.Rows {
		if ra.Table == Fleet && !v.Fleet.HasRow(ra.Row) {
			v.Fleet.SetRows(append(slices.Clone(v.Fleet.Rows()), ra.Row))
		}
	}
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			e := ch.Entry
			switch {
			case ch.Table != Fleet:
			case e.Create != nil:
				v.Fleet.Put(&Card{ID: e.ID, Row: e.Create.Row, Col: e.Create.Col, Score: e.Create.Score, Fields: maps.Clone(e.Set)})
			case e.Remove:
				if c := v.Fleet.Card(e.ID); c != nil {
					off := *c
					off.Row, off.Col = "", ""
					v.Fleet.Put(&off)
				}
			}
		}
	}
	return &v, p
}

// readStart is when a working read card started: its take, else its deal (a friend's read
// dealt straight to working).
func readStart(c *Card) time.Time {
	if t := stampAt(c, "taken"); !t.IsZero() {
		return t
	}
	return stampAt(c, "asked")
}

// readsWantZeroWhy says why a primary in review that is not acceptable wants no read card
// (ReadCardsWhy): failed work, or the reads that stand (readCardsStanding), each named.
func readsWantZeroWhy(s *Snapshot, pr *Card, idx map[string][]*Card) string {
	if pr.F("result") == "failed" {
		return "its work came back failed (result=failed)"
	}
	placed, oks, brk := fleetReadLiveOf(s, pr, idx[pr.ID])
	var out []string
	say := func(kind string, c *Card) {
		out = append(out, kind+" "+c.ID+" col="+orDash(c.Col)+" verdict="+orDash(c.F("verdict"))+" start="+stamp(readStart(c)))
	}
	for _, c := range placed {
		say("placed", c)
	}
	for _, c := range oks {
		say("ok", c)
	}
	for _, c := range brk {
		say("broken", c)
	}
	if s.Readers != nil {
		for _, c := range liveReadsAt(s, pr, readAttempt(pr)) {
			say("readers-table", c)
		}
	}
	return "attempt=" + itoa(readAttempt(pr)) + " stands: " + strings.Join(out, "; ")
}

// frontierRoomCondsOfWaits is the frontier-room judgment of the read-card ask: a frontier read
// that waits because every unit that may read it is at its room (full: those units), one
// judgment naming each read and who is full, as the friend ask's does (frontierRoomJudgment).
// A read no unit may read at all is cannot ask's (cannotAskEpisodes), judged by it, not here.
func frontierRoomCondsOfWaits(s *Snapshot, waits map[string]string, full map[string][]string) []cond {
	var judged, causes []string
	for _, id := range slices.Sorted(maps.Keys(waits)) {
		pr := s.Work.Placed(id)
		if pr == nil || s.readTierOf(pr) != cardhdr.RouteFrontier || strings.HasPrefix(waits[id], NoReaderMayRead) {
			continue
		}
		why := "fewer units may read it than it wants"
		if names := full[id]; len(names) > 0 {
			why = strings.Join(slices.Sorted(slices.Values(names)), ", ") + " full"
		}
		judged = append(judged, id)
		causes = append(causes, id+" waits: "+why)
	}
	return frontierRoomConds(judged, causes)
}
