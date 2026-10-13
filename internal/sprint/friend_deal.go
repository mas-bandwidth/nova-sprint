package sprint

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// A friend's card (the owner, 2026-10-03: "Could we try expressing the work left for
// nova-tools-1.1.0 into cards, and doing it via the sprint, but doing parts on friends
// where we would normally do friend work."; docs/SPEC-SPRINT.md section 1, a friend's
// card). A brief whose header carries `WHO: friend` (any friend) or `WHO: friend <name>`
// (cardhdr.ReadWho) is dealt by the tick to a friend instead of a machine: its work card
// is placed on the friend's own fleet row, FriendRow(<name>), ready (friend sync delivers
// it into her inbox) and working once she starts it (friend_start.go), within her room,
// and is finished from her outbox by friend sync. The friend's row is a fleet row no machine
// can be: its name holds a dot, which no member name holds (ValidID), so no fleet verb
// names it, and the fleet's members (Members) leave it out: no presence, rebalance,
// level, sync or deal of the machines touches it, and a friend who goes quiet keeps her
// card (no take-back) while the deadline rule holds it as it holds any work card.

// FieldWho is a primary's worker as its brief's WHO line names it, written by add and
// brief with the brief: WhoFriend for any friend, FriendRow(<name>) for one; absent on a
// machine's card.
const FieldWho = "who"

// WhoFriend is the who of a card dealt to any friend.
const WhoFriend = "friend"

// RoleBuilder is the role a friend's nova-config row names to be dealt work cards: a row
// whose roles do not name it is dealt no work card (friendDealable; docs/SPEC-SPRINT.md
// section 1, a friend's card). RoleReader, in read_cards.go, is the read cards' role.
const RoleBuilder = "builder"

// friendHasRole says the friend's role list names role. An empty list is a row from before
// roles were configured (a legacy snapshot): it keeps the pre-role behavior, both for the
// deal and for the ask (docs/SPEC-SPRINT.md section 1, a friend's card; section 6, Who
// reads). A configured list is authoritative.
func friendHasRole(f FriendSeat, role string) bool {
	return len(f.Roles) == 0 || slices.Contains(f.Roles, role)
}

// friendRowPrefix begins every friend's fleet row: a dot, so no machine's name is one.
const friendRowPrefix = "friend."

// FriendRow is the fleet row, and the member, a friend's cards are dealt to.
func FriendRow(name string) string { return friendRowPrefix + name }

// FriendOfRow is the friend whose row it is; false for a machine's row.
func FriendOfRow(row string) (string, bool) {
	name, ok := strings.CutPrefix(row, friendRowPrefix)
	return name, ok && name != ""
}

// IsFriendRow says the fleet row is a friend's.
func IsFriendRow(row string) bool {
	_, ok := FriendOfRow(row)
	return ok
}

// WhoOfBrief is the who a brief gives its card (FieldWho): "" when its header names no
// friend (or its WHO line does not read: add refuses that brief), else WhoFriend or
// FriendRow(<name>), with an only. prefix for a hard pin.
func WhoOfBrief(brief string) string {
	w, why := cardhdr.ReadWho(brief)
	switch {
	case why != "" || !w.Friend:
		return ""
	case w.Name == "":
		return WhoFriend
	}
	if w.Only {
		return "only." + FriendRow(w.Name)
	}
	return FriendRow(w.Name)
}

// FriendCard says the primary names a friend, and names that friend ("" for any friend).
// A hard pin (only.friend.<name>) names her too.
func FriendCard(c *Card) (name string, ok bool) {
	w := strings.TrimPrefix(c.F(FieldWho), "only.")
	if w == WhoFriend {
		return "", true
	}
	name, ok = FriendOfRow(w)
	if !ok {
		return "", false
	}
	return name, true
}

// pinnedTo says the hard pin pr (OnlyFriend) names this friend: the card is hers to keep. A pin
// released past its bound and dealt to another friend is that friend's ordinary card, not her pin.
func pinnedTo(pr *Card, friend string) bool {
	name, _ := FriendCard(pr)
	return OnlyFriend(pr) && name == friend
}

// OnlyFriend is the hard pin (docs/SPEC-SPRINT.md, WHO preference): the explicit one,
// WHO: only friend <name>, and a WHO: friend <name> card come back by a rework, a return
// or a redo (ReworkPinned), whose next attempt is hers as its first was.
func OnlyFriend(c *Card) bool {
	return strings.HasPrefix(c.F(FieldWho), "only.friend.") || ReworkPinned(c)
}

// ReworkPinned says the card names a friend (WHO: friend <name>) and has come back by a
// rework, a return or a redo (its reworks or returns counted): its next attempt waits for
// her alone, never another friend or a machine (the owner, 2026-10-05: a rework of a
// friend's own rating was dealt to another worker, who could not do it as her). A
// take-back alone (friend take) counts neither, so a preference taken back from her is
// offered on.
func ReworkPinned(c *Card) bool {
	if c.Int("reworks") == 0 && c.Int("returns") == 0 {
		return false
	}
	_, named := FriendOfRow(c.F(FieldWho))
	return named
}

// friendCardWhy is why the machines' deal leaves a hard-pinned card: the tick deals it
// to that friend, never to a machine or to another friend.
const friendCardWhy = "a friend's card (its brief says WHO: only friend, or a WHO: friend <name> card come back by a rework): the tick deals it to that friend up with room, never to a machine"

// FriendSeat is one friend as the tick deals to her: her name, her width (the jobs she
// works at once, her friends row's), her status (FriendStatus: up, held or down), her
// class (the tiers her nova-config row says she can do, sorted and comma joined), her
// delivery mode (Mode: batch or one-shot, default batch), her tiers (config.friends
// tiers: flash, frontier, pro; when given, the deal and the level read them before her
// class: friendTiers), Dir, her working directory when the ask writes the read brief
// itself (empty: friend sync writes it), and Running, the cards her last beat names
// running (FriendReport.Running: work card ids, job names or primaries), which the tick's
// level never moves (friendStarted).
type FriendSeat struct {
	Pronouns string // Explicit configured choice; unstated is they/them.
	Name     string
	Width    int
	Status   string
	Class    string
	Mode     string
	Tiers    []string
	// Roles is her nova-config row's roles: a read card is dealt only to a friend whose
	// roles name reader (RoleReader, read_cards.go).
	Roles []string
	Dir   string
	// Streams and Kinds are her work restriction (her nova-config row's streams and
	// kinds, carried by friend sync): the stream globs and card KIND values the deal
	// may hand her; empty is no restriction (docs/SPEC-SPRINT.md section 1, a friend's
	// card).
	Streams []string
	Kinds   []string
	Running []string
	// Why is why her Status is not up, as FriendDownWhy says it (held, or the session
	// evidence she lacks), "" while she is up: the words a take refused for her names.
	Why string
	// Active is the newest write under her working directory as her daemon's last beat
	// found it (FriendReport.Active), zero when it said none: a friend who wrote within the
	// start bound is at work, and the tick moves none of her cards for want of a start
	// (friendUnstartedLevel).
	Active time.Time
	// Proof is her session's last proof as her beat carries it (Beat.Proof: a SESSION CHECK
	// it answered, or a bus message of its own), and Finished the store's record of her
	// last finish, working to done; each zero when there is none. The stall ladder reads
	// them as evidence of her work (FriendWorked).
	Proof    time.Time
	Finished time.Time
	// Answered is when her session last answered the coordinator's wake ping (her
	// FriendHealth observation, up), zero when it has not.
	Answered time.Time
	// ReadsFirst is the room her reads take before any work card in this deal (reads are a
	// card priority, reads_priority.go friendReadsFirst): set by the tick's deal on the
	// seats it deals work to, never read from her row; zero leaves her room as it is.
	ReadsFirst int
	// Beat, Evidence, DaemonOnly and Current are what the status transitions read
	// (StatusTransitions, judgments_status.go): her last beat (zero when she never beat),
	// what her status rests on (FriendEvidence), whether she is down while the coordinator's
	// last observation is her daemon's pong alone (DaemonPong: her daemon answers, her
	// session does not), and the build the server runs, which her daemon's
	// (Beat.Friend.Build) is compared with.
	Beat       Beat
	Evidence   string
	DaemonOnly bool
	Current    string
}

// FieldFriendsLeft is the friends a friend's work card has left, comma joined: each the
// level moved it off (FriendLevel), and the one the coordinator took it back from
// (FieldTakenFrom) once it is dealt again. Neither the deal nor the level places it on any
// of them again (docs/SPEC-SPRINT.md section 1, friend-deal-idle-lanes-first.w1), with one
// exception: a card withdrawn off a friend held or down that no other friend up may take
// is dealt back to a friend the level moved it off (never the one it was withdrawn or
// taken back from), rather than stranded ready (friendDealPass, withdrawnFrom).
const FieldFriendsLeft = "friends_left"

// friendTiers is the tiers the friend can do: her Tiers, else her class's.
func friendTiers(f FriendSeat) []string {
	if len(f.Tiers) > 0 {
		return f.Tiers
	}
	return Split(f.Class)
}

// friendDealable says the deal may fill the friend's row (docs/SPEC-SPRINT.md section 1, a
// friend's card): her status is up by the friends' rule (FriendStatus: her session's
// evidence, never a beat alone; not held, not down), the coordinator's row of
// her (her fleet control card) says neither down nor held, and the stall ladder has not
// marked her down (PropFriendStallDown: released only by her activity). On 2026-10-06 a
// friend whose row read down, her lanes paused and her daemon beating, was dealt 18 cards
// twice; a row that cannot work is filled by no deal. While the friends' work is off
// (FriendsOff, nova-sprint set --friends off) no friend's row is. A friend whose roles do
// not name builder (RoleBuilder) is dealt no work card: a reader-only row reads and builds
// nothing (the dogfood of 2026-10-10 evening, a reader-only friend was dealt 16 work cards
// once it came up). A friend whose dealt cards were returned to the pool for not taking
// (PropFriendDealtNoMore) is dealt no more until she takes one (friendDealtReturn;
// tla/FriendDealtReturn.tla DealtNoMoreHolds).
func friendDealable(s *Snapshot, f FriendSeat) bool {
	return !s.FriendsOff() && friendCanRead(s, f) && friendHasRole(f, RoleBuilder) && !friendDealtNoMore(s, f.Name)
}

// PropFriendDealtNoMore is the fleet property marking a friend whose dealt cards were not
// taken within the dealt bound and were returned to the pool (friendDealtReturn): the deal
// hands her no more work until she takes one, and her start clears the mark
// (friendDealPass). The value is when she was marked.
func PropFriendDealtNoMore(friend string) string { return "friend_dealt_no_more." + friend }

// friendDealtNoMore says the friend has been dealt cards she did not take within the bound:
// she is dealt no more until she takes one.
func friendDealtNoMore(s *Snapshot, name string) bool {
	if s == nil || s.Fleet == nil {
		return false
	}
	v, _ := s.Fleet.Prop(PropFriendDealtNoMore(name))
	return v != ""
}

// friendHasTaken says the friend has taken a card: her beat names a job running
// (f.Running), or any card on her row, ready or working, is started at its generation
// (startedNow) or carries her start or her progress (friendStarted, on a work card). It is
// the tick's one predicate for "she has taken one", read by the return's leave-alone gate
// (friendDealtReturn) and by the mark clear (friendTakenClear). The model is
// tla/FriendDealtReturn.tla, whose Return requires no card of hers in started.
func friendHasTaken(s *Snapshot, f FriendSeat) bool {
	if len(f.Running) > 0 {
		return true
	}
	for _, col := range []string{Ready, Working} {
		for _, c := range s.Fleet.Cell(FriendRow(f.Name), col) {
			if startedNow(c) || c.F("kind") == "work" && friendStarted(s, f, c) {
				return true
			}
		}
	}
	return false
}

// friendTakenClear is the clear of a friend's dealt-no-more mark (PropFriendDealtNoMore)
// when she has taken a card (friendHasTaken): a marked friend who takes one is dealt again
// (friendDealtReturn; tla/FriendDealtReturn.tla Start removes f from dealtNoMore). It
// answers the clear, guarded on the value it read, when she is marked and has taken, else
// nothing. friendDealPass, friendUnstartedWorking and FriendStart each call it at the point
// her start is read.
func friendTakenClear(s *Snapshot, f FriendSeat) []PropWrite {
	if !friendDealtNoMore(s, f.Name) || !friendHasTaken(s, f) {
		return nil
	}
	was, _ := s.Fleet.Prop(PropFriendDealtNoMore(f.Name))
	return []PropWrite{{Table: Fleet, Name: PropFriendDealtNoMore(f.Name), Value: "", Was: was, WasAbsent: false}}
}

// friendCanRead says the friend's row may be dealt a read card: friendDealable but for the
// friends' work switch, which stops her work and never her reads (set --friends off: "her
// reads still flow"; read_cards.go).
func friendCanRead(s *Snapshot, f FriendSeat) bool {
	if f.Status != Up {
		return false
	}
	if s == nil || s.Fleet == nil {
		return true
	}
	row := FriendRow(f.Name)
	if ctl := s.MemberCtl(row); ctl != nil {
		if st := ctl.F("status"); st == Down || st == Held || ctl.F("held") != "" {
			return false
		}
	}
	if down, _ := s.Fleet.Prop(PropFriendStallDown(f.Name)); down != "" {
		return false
	}
	return true
}

// friendTakes says the friend may be given a card of the tier: it is one of her tiers
// (friendTiers), never her class as a whole, and the friends' tiers hold it (FriendsTake,
// set --friends-tiers). A friend whose row names no tier takes none, and every friend
// deal and move is gated on it, whatever the card's WHO line, so a frontier card never
// reaches a friend without frontier.
func friendTakes(s *Snapshot, f FriendSeat, tier string) bool {
	return slices.Contains(friendTiers(f), tier) && s.FriendsTake(tier)
}

// cardLeft is friendsLeft of the work card wc with the friends its primary c has left: the
// primary carries FieldFriendsLeft when the failed rule reworked it off a friend whose lane
// ran it empty (ruleHarness, ClassEmptyRun), and no later attempt is dealt to her.
func cardLeft(c, wc *Card) []string {
	left := friendsLeft(wc)
	if c == nil {
		return left
	}
	for _, f := range Split(c.F(FieldFriendsLeft)) {
		if !slices.Contains(left, f) {
			left = append(left, f)
		}
	}
	return left
}

// friendsLeft is the friends the work card itself has left (FieldFriendsLeft), with the one
// it was taken back from while it is withdrawn. A reader that decides where an attempt may
// go reads cardLeft, which adds the friends its primary has left for good.
func friendsLeft(wc *Card) []string {
	if wc == nil {
		return nil
	}
	left := Split(wc.F(FieldFriendsLeft))
	if from, ok := FriendOfRow(wc.F(FieldTakenFrom)); ok && !slices.Contains(left, from) {
		left = append(left, from)
	}
	return left
}

// friendsFor is the friends up (friendDealable) whose tiers hold the tier who may still be
// dealt the primary c: never one the card has left for good (cardLeft: an empty run), nor
// one its withdrawn attempt was withdrawn or taken back from (withdrawnFrom), as the
// friends' deal places it (friendDealPass). None, while friends
// alone serve the tier (tierServed), is a card no worker is left for: the tick's judgment
// of the tier names it (TickDeal).
func (s *Snapshot) friendsFor(c *Card, tier string) []FriendSeat {
	gone := cardLeft(c, nil) // the friends a rework moved it off for good
	if wc := s.Fleet.Placed(WorkCardID(c.ID, c.Int("attempt"))); wc != nil && wc.Col == Withdrawn {
		gone = append(gone, withdrawnFrom(wc)...)
	}
	var out []FriendSeat
	for _, f := range s.Friends {
		if friendDealable(s, f) && friendTakes(s, f, tier) && !slices.Contains(gone, f.Name) {
			out = append(out, f)
		}
	}
	return out
}

// withdrawnFrom is the friends a withdrawn work card must never go back to: the friend
// whose row it was withdrawn on, and the one the coordinator took it back from
// (FieldTakenFrom), never the friends the level moved it off.
func withdrawnFrom(wc *Card) []string {
	var out []string
	if f, ok := FriendOfRow(wc.Row); ok {
		out = append(out, f)
	}
	if f, ok := FriendOfRow(wc.F(FieldTakenFrom)); ok && !slices.Contains(out, f) {
		out = append(out, f)
	}
	return out
}

// friendStarted says the friend has started the work card, by the store's own data: her
// beat names it running (the card, its job or its primary), or it carries a progress stamp
// (FieldProgress). The tick's level never moves a started card.
// tla/FriendDealtReturn.tla Progress and Run (the model's take paths: Taken).
func friendStarted(s *Snapshot, f FriendSeat, c *Card) bool {
	if c.F(FieldProgress) != "" {
		return true
	}
	job := StoredID(c.ID, s.Epoch)
	if g := c.Int("gen"); g > 1 {
		job += ".g" + itoa(g)
	}
	for _, r := range f.Running {
		if r == c.ID || r == job || (r != "" && r == c.F("primary")) {
			return true
		}
	}
	return false
}

// laneRunsIt is the friend whose last beat names the primary c running, by its own id, its
// current attempt's work card, or the withdrawn work card wc and its job; "" when no beat
// does. The deal never places a card on a second row while a lane runs it
// (docs/SPEC-SPRINT.md section 1, one lane per card): a card taken back or handed back
// while her lane still runs it waits ready until her beat stops naming it.
func laneRunsIt(s *Snapshot, seats []FriendSeat, c, wc *Card) string {
	names := []string{c.ID, WorkCardID(c.ID, c.Int("attempt"))}
	if wc != nil {
		job := StoredID(wc.ID, s.Epoch)
		if g := wc.Int("gen"); g > 1 {
			job += ".g" + itoa(g)
		}
		names = append(names, wc.ID, job)
	}
	for _, f := range seats {
		for _, r := range f.Running {
			if r != "" && slices.Contains(names, r) {
				return f.Name
			}
		}
	}
	return ""
}

// friendRoom is the friend's room and her lanes: DealAhead times her width and her width,
// in batch mode and in one-shot mode alike (docs/SPEC-SPRINT.md section 1, "A friend's
// card"): a one-shot friend runs one lane per unit of width, each card a fresh run (her
// daemon's one-shot lanes, docs/SPEC-FRIEND.md), so she is dealt to her width as a batch
// friend is; the room less what her reads take first in this deal (FriendSeat.ReadsFirst).
func friendRoom(f FriendSeat) (room, width int) {
	return DealAhead*f.Width - f.ReadsFirst, f.Width
}

// preferredFriend is the friend of names a card goes to (docs/SPEC-SPRINT.md section 1,
// friend-deal-idle-lanes-first.w1): a friend with an idle lane (lanes > 0) before every
// friend with none, the most idle lanes first, then the most room free, then the first by
// name; "" when names is empty. The caller gives only the friends the card may go to.
func preferredFriend(names []string, lanes, free map[string]int) string {
	best := ""
	for _, f := range names {
		switch {
		case best == "":
			best = f
		case max(lanes[f], 0) != max(lanes[best], 0):
			if lanes[f] > lanes[best] {
				best = f
			}
		case free[f] != free[best]:
			if free[f] > free[best] {
				best = f
			}
		case f < best:
			best = f
		}
	}
	return best
}

// machineTakesAtOnce says an up machine has an idle lane and can draw a deal of c (an
// enabled route of its tier it can launch): the friend deal leaves a card whose chosen
// friend has no idle lane to the machines, which start it at once, rather than queueing
// it behind her working lanes (docs/SPEC-SPRINT.md section 1,
// friend-deal-idle-lanes-first.w1; a friend with no free lane is not dealt work while
// another member that serves the card's tier has a free lane).
func (s *Snapshot) machineTakesAtOnce(c *Card) bool {
	if len(s.Routes) == 0 {
		return false // no routes: the machines serve no tier, and the friends are the deal
	}
	members := s.UpMembers()
	loads := memberLoads(s, members)
	for _, m := range members {
		if s.Width(m)-loads[m] <= 0 {
			continue // no idle lane
		}
		if _, why, byFriend := s.routeServed(c, nil, m); why == "" && !byFriend {
			return true
		}
	}
	return false
}

// Members is the fleet's machines: its rows but the friends' (FriendRow), in row order.
func (s *Snapshot) Members() []string {
	var out []string
	for _, m := range s.Fleet.Rows() {
		if !IsFriendRow(m) {
			out = append(out, m)
		}
	}
	return out
}

// friendLoad is the cards a friend holds: ready and working on her row, her work cards and
// her reads together. One width bounds her row (docs/SPEC-SPRINT.md section 1, a friend's
// card; the owner's rule): her reads hold her lanes and her room as her work does, in
// batch and one-shot mode alike.
//
// While read cards are on (read_cards.go) a read holds half a slot of her width, as a
// member's does (halfLoad): her row holds her width of work or twice it of reads.
func friendLoad(s *Snapshot, name string) int {
	row := FriendRow(name)
	if s.ReadCardsOn() {
		return halfLoad(rowLoad(s, row))
	}
	return s.Fleet.Count(row, Ready) + s.Fleet.Count(row, Working)
}

// friendDealtOrder is the one order a friend's dealt set is filled and taken in
// (docs/SPEC-SPRINT.md, rank and take): the priority ladder (rankOf) highest first; within a
// level the stream turns (one card of each stream in turn from the round's index, the streams
// in name order when it is nil), within a stream in work order (SortCards: score then id, so
// a rank --first card leads its stream). streamOf names the stream a card belongs to: a
// primary's own row, a friend's work card's "stream" field. The friend deal offers its
// primaries in this order (TickDeal, friendDealByLadder) and a lane of hers takes her ready
// cards in it too (takeOne, FriendLaneOrder), so the lane take is the deal's order and never
// the order the cards happened to be dealt in.
func friendDealtOrder(cards []*Card, streamOf func(*Card) string, rankOf func(*Card) int, r *round) []*Card {
	out := streamTurnsBy(cards, streamOf, r)
	sort.SliceStable(out, func(i, j int) bool { return rankOf(out[i]) < rankOf(out[j]) })
	return out
}

// FriendLaneOrder is the order a lane of a friend takes her dealt set in: the same order the
// deal fills it (friendDealtOrder), over a work card's own stream field, with the streams in
// name order (no deal round): the ladder, the stream turns, then work order within a stream.
// It is what laneLine prints the held card's rank position in (lanetake.go).
func FriendLaneOrder(cards []*Card) []*Card {
	return friendDealtOrder(cards, func(c *Card) string { return c.F("stream") }, cardRank, nil)
}

// friendWorkRank is a friend's dealt work card's place on the ladder: a work card's own level
// is written at its deal (priorityOnWork) and may be stale after a priority change, so its
// primary's current level is read (the lane take's order; takeOne).
func (s *Snapshot) friendWorkRank(c *Card) int {
	if c.F("kind") == "work" && s != nil && s.Work != nil {
		if pr := s.Work.Card(c.F("primary")); pr != nil {
			return cardRank(pr)
		}
	}
	return cardRank(c)
}

// streamTurnsBy is the cards in stream turns over the stream streamOf names for a card (a
// primary's row, a friend's work card's stream field), as streamTurns (round.go) is over
// c.Row: one card of each stream in turn from the round's index (name order when it is nil),
// within a stream in work order (SortCards), a stream with no card left skipped; a card of a
// stream the round does not name takes its turn after them, its streams in name order.
func streamTurnsBy(cards []*Card, streamOf func(*Card) string, r *round) []*Card {
	by := map[string][]*Card{}
	known := map[string]bool{}
	var order []string
	if r != nil {
		n := len(r.order)
		for i := 0; i < n; i++ {
			st := r.order[(r.start()+i)%n]
			order = append(order, st)
			known[st] = true
		}
	}
	var extra []string
	for _, c := range cards {
		st := streamOf(c)
		if !known[st] {
			known[st] = true
			extra = append(extra, st)
		}
		by[st] = append(by[st], c)
	}
	slices.Sort(extra)
	order = append(order, extra...)
	for _, st := range order {
		SortCards(by[st])
	}
	out := make([]*Card, 0, len(cards))
	for turn := 0; len(out) < len(cards); turn++ {
		for _, st := range order {
			if turn < len(by[st]) {
				out = append(out, by[st][turn])
			}
		}
	}
	return out
}

// friendDealPass is the tick's friend deal (TickDeal), run before the machines' deal: friends
// first (docs/SPEC-SPRINT.md, WHO preference; tla/WhoPreference.tla checks the selection,
// not the room). It offers every ready card given (in the order given, the deal's stream
// turns) to the friends up, each within her room, DealAhead times her width, as the
// machines' deal fills a member (the owner, 2026-10-04: "Do it just like the fleet, you keep
// people busy by having 2X width queued up in ready per-friend"). A card whose WHO line
// names a friend goes to her first while she is up, below her room, not one it has left,
// and her tiers hold its tier; else (or with no WHO line, or WHO: friend) it goes to a
// friend up whose tiers hold its tier (friendTakes, every friend deal's gate; a card with
// no tier is the dealer's default, flash: cardTierOf), below her room, and never one it has
// left (friendsLeft), chosen by preferredFriend: an idle lane first, the most idle lanes,
// then the most room, then by name (docs/SPEC-SPRINT.md section 1,
// friend-deal-idle-lanes-first.w1). A card no friend takes stays for the fleet's deal,
// unless it says WHO: only friend <name> (OnlyFriend), the one hard pin: it waits ready for
// her, and so does one whose friend's tiers do not hold its tier. A card a friend's beat
// names running (laneRunsIt) is placed on no row while it does. A withdrawn attempt at its
// redeal bound at its ceiling or its attempt cap (AtRedealBound), or refused at staging by
// every member up, stays with the machines' deal and its judgment; one at its redeal bound
// below its ceiling is offered at the tier it escalates to (escalating), as the machines
// would escalate it, and a friend's deal of it is a new attempt on that tier, the bound
// attempt retired (friendEscalateUnit). A friend at or over her room is never
// dealt. In batch and one-shot mode alike (docs/SPEC-SPRINT.md section 1, "A friend's
// card"), a friend's room is DealAhead times her width and her lanes are her width. A lane of hers is idle while no card on her row
// holds it, started or not.
// Each is its next attempt's work card, created on the friend's row at generation 1, ready
// whatever her lanes, untaken and with no deadline running (docs/SPEC-SPRINT.md section 1,
// a friend's card is working once she starts it), carrying the primary's fix, finding and
// why as a machine's deal does; its primary moves ready -> working. Before it deals, the
// pass moves each card on a friend's row that she has started into working, its deadline
// from then (friendStartUnits). The friend's row is declared by the plan the first time she
// is dealt to. It answers the cards it places on each friend's row and how many cards on it
// go into working on her start: the tick levels the friends after it. A named pin (WHO: friend <name>,
// not a hard pin) placed on a different friend's row carries a judgment on that
// unit (pinIgnoredNote): why she did not take it, and whose row holds the card.
// The pass keeps that judgment (pinConds) until the card is back on her row or
// leaves ready and working.
// With reclaim set it also reclaims the fleet's dealt-ahead cards (friendReclaim): a deal in
// passes (a pass of the cards above reads, then the reads, then the rest) reclaims once, in
// its last pass, after the reads.
func friendDealPass(s *Snapshot, cards []*Card, seats []FriendSeat, reclaim bool) (p Plan, dealt, dealtWorking map[string]int) {
	// the one order her dealt set is filled in (friendDealtOrder; docs/SPEC-SPRINT.md, rank
	// and take): the ladder, the deal's stream turns, then work order (rank) — the same order
	// a lane of hers takes in (takeOne, FriendLaneOrder)
	cards = friendDealtOrder(cards, rowOf, cardRank, streamRound(s, PropStreamIndex))
	free, lanes, seat := map[string]int{}, map[string]int{}, map[string]FriendSeat{}
	dealt, dealtWorking = map[string]int{}, map[string]int{}
	var up []string
	for _, f := range seats {
		if friendDealable(s, f) {
			room, width := friendRoom(f)
			free[f.Name] = room - friendLoad(s, f.Name)
			// a lane is idle while no card on her row holds it, started or not
			lanes[f.Name] = width - friendLoad(s, f.Name)
			seat[f.Name] = f
			up = append(up, f.Name)
		}
	}
	slices.Sort(up)
	// her start receipts first: a card ready on her row that she has started goes to working,
	// its deadline from now (friendStartUnits; docs/SPEC-SPRINT.md section 1, a friend's card
	// is working once she starts it), in batch mode and in one-shot mode alike
	// and working on her row means started: a card her finish's next or a take-back's next
	// moved there with no start of hers goes back ready (friendUnstartedWorking)
	// a lane gone unheard past its bound: its card bounces back ready (lane_hold.go)
	p.Units = append(p.Units, laneGoneUnits(s)...)
	unstarted, unstartedCleared := friendUnstartedWorking(s, seats)
	p.Units = append(p.Units, unstarted...)
	p.Props = append(p.Props, unstartedCleared...)
	starts, started := friendStartUnits(s, seats)
	p.Units = append(p.Units, starts...)
	maps.Copy(dealtWorking, started)
	// a friend who takes a card is dealt again: her start clears the dealt-no-more mark
	// (friendTakenClear; friendDealtReturn, PropFriendDealtNoMore)
	for _, f := range seats {
		p.Props = append(p.Props, friendTakenClear(s, f)...)
	}
	members := s.UpMembers()
	declared := map[string]bool{}
	for _, c := range cards {
		if c.Col != Ready || IsSentinel(c) {
			continue
		}
		if !ConsumerTrunkReady(s, c) {
			continue
		}
		// a primary whose named needs have not landed is dealt to no friend
		// (docs/SPEC-SPRINT.md, needs and the deal; WaitsFor)
		if len(WaitsFor(s, c, nil)) > 0 {
			continue
		}
		// a card taken back from a friend (friend take) is placed again, never on her (FriendTake)
		wc := s.Fleet.Placed(WorkCardID(c.ID, c.Int("attempt")))
		if wc != nil && wc.Col != Withdrawn {
			wc = nil
		}
		// a failed attempt's judgment (at its ceiling or its attempt cap), and a staging
		// refusal by every member up, stay with the machines' deal; an attempt at its
		// redeal bound below its ceiling is offered at the tier it escalates to
		if AtRedealBound(s, c) != nil {
			continue
		}
		if held, _ := AtStagingBound(s, c, members); held != nil {
			continue
		}
		if laneRunsIt(s, seats, c, wc) != "" {
			continue // one live lane per card: no second row while her lane runs it
		}
		escalated := wc != nil && redealBound(wc)
		tier := s.DealTier(escalating(s, c))
		left := cardLeft(c, wc)
		pinned, pinnedCard := FriendCard(c)
		leftAtPin := slices.Clone(left)
		name := pinned
		if !pinnedCard {
			name = ""
		}
		if name != "" && (free[name] <= 0 || slices.Contains(left, name) || !friendTakes(s, seat[name], tier) || !friendRestrictionAllows(seat[name], c)) {
			name = "" // the friend it names is not up with room, it has left her, not her tier, or outside her restriction
		}
		// a hard pin whose friend has been not up past its bound is a preference (PinReleased)
		hard := OnlyFriend(c) && !PinReleased(s, c, seats)
		if name == "" && !hard {
			var may []string
			for _, f := range up {
				if free[f] > 0 && !slices.Contains(left, f) && friendTakes(s, seat[f], tier) && friendRestrictionAllows(seat[f], c) {
					may = append(may, f)
				}
			}
			if len(may) == 0 && wc != nil {
				// a card withdrawn off a friend held or down (or taken back) whom no friend
				// it has not left may take: the friends the level moved it off may have it
				// back, so it is not stranded ready while one is up with room (the owner's
				// rule: a held or down friend's cards go to the up friends' ready queues);
				// never the friend it was withdrawn from or taken back from, nor one
				// it has left for good (cardLeft: an empty run)
				gone := append(withdrawnFrom(wc), cardLeft(c, nil)...)
				for _, f := range up {
					if free[f] > 0 && !slices.Contains(gone, f) && friendTakes(s, seat[f], tier) && friendRestrictionAllows(seat[f], c) {
						may = append(may, f)
					}
				}
				left = slices.DeleteFunc(slices.Clone(left), func(f string) bool { return !slices.Contains(gone, f) })
			}
			name = preferredFriend(may, lanes, free)
		}
		if name == "" || slices.Contains(left, name) || free[name] <= 0 || !friendRestrictionAllows(seat[name], c) {
			continue // no friend it may go to is up with room: the fleet's, or (only) it waits ready
		}
		// a friend with no idle lane gives way to a machine that serves the card's tier and
		// has one: the machine starts it at once, where a queue behind her working lanes
		// would leave it dealt and untaken (docs/SPEC-SPRINT.md section 1,
		// friend-deal-idle-lanes-first.w1; a friend with no free lane is not dealt work while
		// another member that serves its tier has a free lane). A hard pin waits for its
		// friend alone, never a machine (OnlyFriend).
		if !hard && lanes[name] <= 0 && s.machineTakesAtOnce(c) {
			continue
		}
		card := WorkCardID(c.ID, c.Int("attempt")+1)
		if (wc == nil || escalated) && s.Fleet.Card(card) != nil {
			p.refuse(c.ID, "work card "+card+" exists already")
			continue
		}
		free[name]--
		dealt[name]++
		lanes[name]--
		row := FriendRow(name)
		if !s.Fleet.HasRow(row) && !declared[row] {
			p.Rows = append(p.Rows, RowAdd{Fleet, row})
			declared[row] = true
		}
		var u Unit
		switch {
		case escalated:
			u = friendEscalateUnit(s, c, wc, card, row, tier)
		case wc != nil:
			u = friendRedealUnit(s, c, wc, row, tierNowSet(c, tier))
		default:
			u = friendDealUnit(s, c, card, row, Ready, tierNowSet(c, tier))
		}
		if pinnedCard && pinned != "" && !hard && name != pinned {
			placedID := card
			if wc != nil && !escalated {
				placedID = wc.ID
			}
			u.Notes = append(u.Notes, pinIgnoredNote(s, c, placedID, pinned, pinSkipWhy(s, seats, pinned, leftAtPin, tier, free), row, Ready))
		}
		p.Units = append(p.Units, u)
	}
	// then her idle lanes take what the fleet dealt ahead and no lane has taken yet
	if reclaim {
		p.Units = append(p.Units, friendReclaim(s, seats, up, seat, free, lanes, dealt, declared, &p)...)
	}
	return Lawful(p), dealt, dealtWorking
}

// friendDealtReturn is the tick's return of a friend's dealt cards she has not taken within
// the dealt bound (s.DealtMax, docs/SPEC-SPRINT.md section 1, a friend's card;
// tla/FriendDealtReturn.tla Return and DealtNoMoreHolds): each work card ready on her row
// past the bound, that she has not started (friendStarted) and that is not her hard pin
// (pinnedTo), is withdrawn — its primary back to ready, no redeal of its bound spent (a
// return is no failure, so FieldTakeEnded is not set) — so the machines' deal places it on
// the next capable member. A friend who has taken a card (friendHasTaken: her beat names a
// job running, or a card on her row, ready or working, is started or carries her progress)
// is left alone: her daemon is at work. Each friend returned is marked dealt no more
// (PropFriendDealtNoMore) until she takes one, which her start clears (friendDealPass). The
// returned work-card ids are answered (skip) so the start-bound level and the level do not
// move them a second time in the same tick.
func friendDealtReturn(s *Snapshot, seats []FriendSeat, since func(string) (time.Duration, bool)) (p Plan, skip map[string]bool) {
	skip = map[string]bool{}
	bound := s.DealtMax()
	marked := map[string]bool{}
	for _, f := range seats {
		if f.Status != Up {
			continue
		}
		row := FriendRow(f.Name)
		if friendHasTaken(s, f) {
			continue // she has taken a card: her beat names a job running, or a card of hers is started
		}
		for _, c := range s.Fleet.Cell(row, Ready) {
			if c.F("kind") != "work" {
				continue
			}
			pr := s.Work.Placed(c.F("primary"))
			if pr == nil || pinnedTo(pr, f.Name) {
				continue
			}
			field, _, _, own := WorkDeadline(s, c)
			if own == "" {
				continue
			}
			if d, ok := since(max(c.F(own), c.F(field))); !ok || d <= bound {
				continue
			}
			set := map[string]string{FieldTakenBack: "returned to the pool: dealt to friend " + f.Name + " and not taken within the dealt bound", "untaken_since": stamp(s.Now)}
			u := withdrawUnit(s, c, set, []string{"first_taken"}, NTakenBack, MachineActor,
				fmt.Sprintf("%s %s:ready -> withdrawn (dealt and not taken within the dealt bound %s)", c.ID, row, bound))
			p.Units = append(p.Units, u)
			skip[c.ID] = true
			marked[f.Name] = true
		}
	}
	for name := range marked {
		if !friendDealtNoMore(s, name) {
			// the mark is gated on the value, not the presence: a friend whose first clear
			// wrote "" (the key stays present) is re-marked when she stops taking again
			// (friendDealPass's clear; tla/FriendDealtReturn.tla Return re-adds f)
			was, had := s.Fleet.Prop(PropFriendDealtNoMore(name))
			p.Props = append(p.Props, PropWrite{Table: Fleet, Name: PropFriendDealtNoMore(name), Value: stamp(s.Now), Was: was, WasAbsent: !had})
		}
	}
	return p, skip
}

// FriendsDealtFleet is each friend's count of the fleet's cards she holds (where --json
// dealt_fleet; docs/SPEC-SPRINT.md section 1, a friend's card): the work cards on her row,
// ready and working, whose primary carries no WHO line (a bare WHO: friend, a named and a
// hard pin are friends' cards). A friend with none is absent.
func FriendsDealtFleet(s *Snapshot) map[string]int {
	out := map[string]int{}
	if s == nil || s.Fleet == nil || s.Work == nil {
		return out
	}
	for _, row := range s.Fleet.Rows() {
		name, ok := FriendOfRow(row)
		if !ok {
			continue
		}
		for _, col := range []string{Ready, Working} {
			for _, wc := range s.Fleet.Cell(row, col) {
				if wc.F("kind") != "work" {
					continue
				}
				// a WHO line of any friend, the bare WHO: friend too, makes it a friend's card
				if pr := s.Work.Card(wc.F("primary")); pr != nil && pr.F(FieldWho) == "" {
					out[name]++
				}
			}
		}
	}
	return out
}

// tierNowSet is the primary's tier field a friend's first deal of it writes: the tier the
// deal drew (dealTierOf), as a machine's deal writes it, so its reads and its escalation
// go by the tier she ran it on; none for a pinned tier, whose ceiling is its tier.
func tierNowSet(c *Card, tier string) map[string]string {
	m, _ := modelOf(c)
	if tier == "" || pinnedTier(c, m) || c.F(FieldTierNow) != "" {
		return nil
	}
	return map[string]string{FieldTierNow: tier}
}

// friendReclaim is the friends' reclaim of the fleet's dealt-ahead cards (docs/SPEC-SPRINT.md
// section 1, a friend's card): a work card the machines' deal placed ready on a machine row
// ahead of its lanes, which no lane has taken (it is still ready there), goes to a friend up
// with an idle lane and room whose tiers hold the tier it was dealt on (dealTierOf), never
// one it has left, its friend chosen as the deal chooses (preferredFriend: the most idle
// lanes, then the most room, then by name; the friend its WHO line names first). It moves at
// its next generation, its fleet route off (a friend runs her own model), ready on her row
// until she starts it; its primary stays working on it, the tier it was dealt on written
// when it names none (tierNowSet). The friend deal runs before the machines' deal each
// tick, so a ready card goes to a friend first and a card dealt ahead to a machine comes
// back to her while her lanes are idle; a machine's take of the old generation is refused.
// It is bounded each tick: a friend reclaims at most her idle lanes, and a machine gives at
// most half its dealt-ahead queue (rounded down), the machines taking turns in row order,
// so no friend drains the fleet's queues in one tick. A held stream, a bench card, a hard
// pin and a card whose route rests (cardRest: the same plan withdraws it,
// restWithdrawals, and a card moved to two places refuses the whole tick) stay where they
// are. A reclaimed card she does not start goes the way of any card of hers: the start
// bound's level or a take-back, after which the machines' deal may place it again.
func friendReclaim(s *Snapshot, seats []FriendSeat, up []string, seat map[string]FriendSeat, free, lanes, dealt map[string]int, declared map[string]bool, p *Plan) []Unit {
	idle := false
	for _, f := range up {
		idle = idle || (lanes[f] > 0 && free[f] > 0)
	}
	if !idle {
		return nil
	}
	var machines []string
	queue, quota := map[string][]*Card{}, map[string]int{}
	for _, m := range s.Members() {
		var prims []*Card
		byPrimary := map[string]*Card{}
		ahead := 0
		for _, wc := range s.Fleet.Cell(m, Ready) {
			if wc.F("kind") != "work" {
				continue // ready on a machine row: no lane has taken it (a take moves it to working)
			}
			ahead++
			pr := s.Work.Placed(wc.F("primary"))
			if pr == nil || pr.Col != Working || pr.F("work") != wc.ID || IsSentinel(pr) || OnlyFriend(pr) ||
				StreamHeld(s, pr.Row) || len(Bench(pr)) > 0 || laneRunsIt(s, seats, pr, wc) != "" {
				continue
			}
			if _, rests := cardRest(s, wc); rests {
				continue // its route rests: the same plan withdraws it (restWithdrawals)
			}
			byPrimary[pr.ID] = wc
			prims = append(prims, pr)
		}
		if quota[m] = ahead / 2; quota[m] == 0 || len(prims) == 0 {
			continue
		}
		for _, pr := range dealOrder(s, prims) {
			queue[m] = append(queue[m], byPrimary[pr.ID])
		}
		machines = append(machines, m)
	}
	var units []Unit
	// one card from each machine in turn, each machine's in the deal order, until no friend
	// may take a card any machine has left to give
	for moved := true; moved; {
		moved = false
		for _, m := range machines {
			for quota[m] > 0 && len(queue[m]) > 0 {
				wc := queue[m][0]
				queue[m] = queue[m][1:]
				u, ok := reclaimUnit(s, wc, up, seat, free, lanes, dealt, declared, p)
				if ok {
					units = append(units, u)
					quota[m]--
					moved = true
					break
				}
			}
		}
	}
	return units
}

// reclaimUnit is the dealt-ahead work card wc moved to the friend the deal would choose for
// it (friendReclaim), her room, lanes and count taken; false when no friend up may take it.
func reclaimUnit(s *Snapshot, wc *Card, up []string, seat map[string]FriendSeat, free, lanes, dealt map[string]int, declared map[string]bool, p *Plan) (Unit, bool) {
	pr := s.Work.Placed(wc.F("primary"))
	tier, left := s.DealTier(pr), cardLeft(pr, wc)
	var may []string
	for _, f := range up {
		if lanes[f] > 0 && free[f] > 0 && !slices.Contains(left, f) && friendTakes(s, seat[f], tier) && friendRestrictionAllows(seat[f], pr) {
			may = append(may, f)
		}
	}
	name := preferredFriend(may, lanes, free)
	if pinned, ok := FriendCard(pr); ok && pinned != "" && slices.Contains(may, pinned) {
		name = pinned
	}
	if name == "" {
		return Unit{}, false
	}
	free[name]--
	lanes[name]--
	dealt[name]++
	row := FriendRow(name)
	if !s.Fleet.HasRow(row) && !declared[row] {
		p.Rows = append(p.Rows, RowAdd{Fleet, row})
		declared[row] = true
	}
	set := nextGen(wc, row, s.Now)
	unset := []string{FieldRoute, FieldModel, FieldTokens, FieldUSD, FieldHarness, FieldDeadline, FieldFriendDeadline}
	changes := []Change{change(Fleet, moveEntry(wc, row, Ready, set, unset...))}
	if t := tierNowSet(pr, tier); t != nil {
		changes = append(changes, change(Work, setEntry(pr, t)))
	}
	pro := parseFriendPronouns(seat[name].Pronouns)
	return Unit{Key: pr.ID, Stream: pr.Row, Changes: changes,
		Moved: pro.Sprintf("%s %s:ready -> %s:%s gen=%d (reclaimed: dealt ahead to %s and untaken; {their} idle lane takes it, friend sync delivers it to {their} inbox)", wc.ID, wc.Row, row, Ready, wc.Int("gen")+1, wc.Row)}, true
}

// friendEscalateUnit is a withdrawn attempt at its redeal bound below its ceiling dealt to
// a friend, as the machines' deal escalates it (escalate): a new attempt's work card on her
// row (friendDealUnit), its primary on the tier it escalates to (FieldTierNow), and the
// bound attempt's work card retired, its record kept.
func friendEscalateUnit(s *Snapshot, c, prev *Card, card, row, tier string) Unit {
	from, _ := CardTiers(c)
	u := friendDealUnit(s, c, card, row, Ready, map[string]string{FieldTierNow: tier})
	u.Changes = append([]Change{change(Fleet, removeEntry(prev, map[string]string{"retired": stamp(s.Now), "retired_by": "escalation"}))}, u.Changes...)
	u.Moved += fmt.Sprintf("; escalated %s -> %s: %s at its redeal bound", from, tier, prev.ID)
	return u
}

// friendDealUnit is one friend's card dealt: its work card on her row, ready whatever her
// lanes, untaken and with no deadline running until she starts it (friendStartUnits;
// docs/SPEC-SPRINT.md section 1, a friend's card is working once she starts it), and its
// primary ready -> working on it. The column argument is not read: every friend's deal is
// ready (the attempt cap's deal, brief_bound.go, still passes one). set rides the primary's
// move beside the deal's own fields (the attempt cap's default answer writes the WHO line
// and the count reset, brief_bound.go).
func friendDealUnit(s *Snapshot, c *Card, card, row, _ string, set map[string]string) Unit {
	attempt := c.Int("attempt") + 1
	now := stamp(s.Now)
	fields := map[string]string{"kind": "work", "primary": c.ID, "stream": c.Row, "attempt": itoa(attempt), "gen": "1", "member": row,
		"dealt": now, "first_dealt": now, "untaken_since": now}
	for _, k := range []string{"fix", "finding", "why"} {
		if v := c.F(k); v != "" {
			fields[k] = v
		}
	}
	// the friends the primary has left for good (ClassEmptyRun, ruleHarness) ride on the new
	// work card, as on a machine's (deal), and every reader of a work card's friends left (the
	// rebalance, a lane's start, the level, the coordinator's pass) reads cardLeft, the work
	// card's and its primary's, so the attempt is kept off them wherever it was dealt
	if left := Split(c.F(FieldFriendsLeft)); len(left) > 0 {
		fields[FieldFriendsLeft] = strings.Join(left, ",")
	}
	priorityOnWork(fields, c)
	prim := map[string]string{"attempt": itoa(attempt), "work": card}
	for k, v := range set {
		prim[k] = v
	}
	friend, _ := FriendOfRow(row)
	p := snapshotFriendPronouns(s, friend)
	return Unit{Key: c.ID, Stream: c.Row, Changes: []Change{
		change(Fleet, createEntry(card, row, Ready, c.Score, fields)),
		change(Work, moveEntry(c, c.Row, Working, prim, "result")),
	}, Moved: p.Sprintf("%s work %s -> working card=%s member=%s %s (a friend's card: friend sync delivers it to {their} inbox)", c.ID, c.Col, card, row, Ready)}
}

// friendRedealUnit is a friend's card taken back (FriendTake) placed again: the same work
// card, withdrawn, on her row at its next generation (its own branch, and its own job:
// friendJobOf), ready until she starts it (friendStartUnits), and its primary ready ->
// working on it, its attempt as it was: a take-back is no attempt and spends no bound.
func friendRedealUnit(s *Snapshot, c, wc *Card, row string, set map[string]string) Unit {
	// a card the fleet held before carries its fleet route; a friend runs her own model, so
	// the route comes off as a first deal to her writes none (2026-10-04: a resting route
	// kept on a friend's card withdrew it every tick, and each deal again was a new inbox copy)
	// set rides the primary's move: the tier the deal drew when it names none (tierNowSet)
	prim := map[string]string{"work": wc.ID}
	maps.Copy(prim, set)
	set, unset := nextGen(wc, row, s.Now), []string{"withdrawn", FieldTakenBack, FieldTakenFrom,
		FieldRoute, FieldModel, FieldTokens, FieldUSD, FieldHarness, FieldDeadline}
	if left := friendsLeft(wc); len(left) > 0 {
		set[FieldFriendsLeft] = strings.Join(left, ",") // the friend it was taken from, kept past the take
	}
	if wc.F(FieldTakeEnded) != "" {
		// a machine's attempt whose take ended: the friend's deal is its next redeal, counted
		// against the bound as the machines' deal counts it
		set["redeals"] = itoa(wc.Int("redeals") + 1)
		unset = append(unset, FieldTakeEnded, FieldProviderError, FieldDecided, FieldDecidedUsed)
	}
	unset = append(unset, FieldFriendDeadline) // set when she starts it
	friend, _ := FriendOfRow(row)
	p := snapshotFriendPronouns(s, friend)
	return Unit{Key: c.ID, Stream: c.Row, Changes: []Change{
		change(Fleet, moveEntry(wc, row, Ready, set, unset...)),
		change(Work, moveEntry(c, c.Row, Working, prim, "result")),
	}, Moved: p.Sprintf("%s work %s -> working card=%s member=%s gen=%d %s (taken back, dealt again: friend sync delivers it to {their} inbox)", c.ID, c.Col, wc.ID, row, wc.Int("gen")+1, Ready)}
}

// FriendRestrictionWhy applies one friend's configured stream globs and KIND values to a
// card's stream and kind: "" when the card is within the restriction (either list empty is
// no restriction, Split reads the comma lists nova-config carries), else the sentence the
// add, the brief and the deal refuse or skip with (docs/SPEC-SPRINT.md section 1, a
// friend's card).
func FriendRestrictionWhy(streams, kinds []string, stream, kind string) string {
	if len(streams) > 0 {
		matched := false
		for _, glob := range streams {
			if ok, err := path.Match(strings.TrimSpace(glob), stream); err == nil && ok {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Sprintf("stream %q is outside this friend's streams restriction (%s)", stream, strings.Join(streams, ","))
		}
	}
	if len(kinds) > 0 {
		for _, allowed := range kinds {
			if strings.TrimSpace(allowed) == kind {
				return ""
			}
		}
		return fmt.Sprintf("KIND %q is outside this friend's kinds restriction (%s)", kind, strings.Join(kinds, ","))
	}
	return ""
}

// friendRestrictionAllows says the deal may hand the card c to the seat: its stream and
// KIND are within her configured restriction (FriendRestrictionWhy). A nil card is no
// restriction to judge.
func friendRestrictionAllows(seat FriendSeat, c *Card) bool {
	if c == nil {
		return true
	}
	return FriendRestrictionWhy(seat.Streams, seat.Kinds, c.F("stream"), BriefKind(c.F("brief"))) == ""
}

// BriefKind reads the task kind from a card brief's typed header (the KIND: line the card
// lint reads): "" when it names none, and a KIND: line in the body grants nothing.
func BriefKind(brief string) string {
	value, ok := swarm.CardHeaderValue([]byte(brief), "KIND")
	if !ok {
		return ""
	}
	return value
}
