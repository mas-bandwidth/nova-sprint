package sprint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// A friend's card taken back (docs/SPEC-SPRINT.md section 1, a friend's card taken back;
// the owner, 2026-10-04, on cards dealt to a friend who would not start them, which could
// only be dropped and added again: "sounds bad, we should fix this"). The coordinator takes
// back a card dealt to a friend that she has not started (friend take), and the hold of a
// friend (friend down) takes back every one of hers she has not started, as a held machine's
// cards are withdrawn: the work card is withdrawn on her row (never a failure: no redeal of
// its bound is spent, FieldTakeEnded is not set), its primary goes back to ready, and the
// friends' deal (friendDealPass) places the same card again at its next generation, on its own
// branch and job. A card she has started stays with her and finishes under the coordinator's
// take: one she pushed to, one her beat names running (the caller reads both: Started), and
// one she finished (in review or later, so no longer ready or working on her row). With cards
// named, each one she may give up is taken and each one she keeps is refused, one line each
// (section 1, the card friend-take-partial.w1); AllOrNothing takes none when any is refused,
// as the take did before. A hold keeps no card (the owner, 2026-10-04, on a held friend still
// showing two working cards: "nonono"): it takes her started cards too, and a started card
// with a push carries its pushed head and the generation whose branch holds it
// (FieldCarryHead, FieldCarryGen), so the next taker starts from that work and none is lost.

const (
	// FieldTakenBack is why a friend's work card was taken back, on the withdrawn card until
	// the deal places it again: "taken back by the coordinator: <reason>", or the hold's words.
	FieldTakenBack = "taken_back"
	// FieldTakenFrom is the friend's row the coordinator took the card back from (friend
	// take, not the hold): the deal that places it again never gives it back to her.
	FieldTakenFrom = "taken_from"
	// NTakenBack is the happened note of a friend's card taken back to ready.
	NTakenBack = "a friend's card taken back to ready"
	// FieldCarryHead is the head a started card had pushed when a hold took it back, and
	// FieldCarryGen the generation whose branch holds it (BranchOf at that generation): the
	// next generation starts from it. Both stay on the card through its next deal.
	FieldCarryHead = "carry_head"
	FieldCarryGen  = "carry_gen"
)

// pushPrefix opens the words friend take and friend down give a started card with a push
// (cmd/nova-sprint friendStarted): "a push on its branch <branch> at <tip>".
const pushPrefix = "a push on its branch "

// PushedTip is the tip a started card's words name ("a push on its branch <branch> at
// <tip>"), "" when they name no push at a full sha (her beat names it running, or the tip
// could not be read): only a push read at its sha is carried.
func PushedTip(why string) string {
	if !strings.HasPrefix(why, pushPrefix) {
		return ""
	}
	_, tip, ok := strings.Cut(strings.TrimPrefix(why, pushPrefix), " at ")
	if !ok || !typedrec.IsFullSha(tip) {
		return ""
	}
	return tip
}

// FriendTakeReq is friend take (IDs or All) or the hold's withdrawal (Hold, All): the
// friend, the cards named (each a primary or its work card), every card of hers she has
// not started (All), why, and Started, the work cards she has started as the caller read
// them (a push on the card's branch, her beat naming it running), each with its why.
// AllOrNothing takes none of the cards named when any one is refused. Begun, with Hold and
// All, takes only the cards she has begun (working, or read as started), leaving her ready
// cards not begun on her row; a friend's hold no longer asks for it, since a held friend
// keeps no card at all (the owner, 2026-10-09).
type FriendTakeReq struct {
	Friend       string
	IDs          []string
	All          bool
	Hold         bool
	Begun        bool
	AllOrNothing bool
	Reason       string
	Started      map[string]string
	Who          string
	// Spends is a take the seat or her runner asked for (friend take): a read card it takes
	// spends its reader (retired_by returned, readSpent), as read --return does; the
	// machine's take-backs (a hold, a stall) spend nothing.
	Spends bool
	// Working lets a card in flight be taken back (FriendCardInFlight: working on her row,
	// or carrying a take, a progress, a report or a carried head). Without it a named card
	// in flight is refused and All takes none (unless Hold). friend take --working sets it
	// for named cards only; the machine's take-backs (the late rule, the re-tier, the stall
	// ladder's rung 4) set it.
	Working bool
}

// FriendCardInFlight says why the friend's card is hers in flight, "" when it is only dealt: in the
// working column, or carrying a take (taken, first_taken), a progress stamp, a report, or a
// head carried from a push. A card in flight is never taken by friend take --all-unstarted
// (nova-sprint, 2026-10-14: --all-unstarted withdrew two friends' working cards mid-flight).
func FriendCardInFlight(c *Card) string {
	switch {
	case c.Col == Working:
		return "it is working on the friend's row"
	case c.F("taken") != "" || c.F("first_taken") != "":
		return "a take is recorded on it"
	case c.F(FieldProgress) != "":
		return "progress is recorded on it"
	case c.F(FieldReported) != "":
		return "a report is recorded on it"
	case c.F(FieldCarryHead) != "":
		return "it carries a pushed head " + c.F(FieldCarryHead)
	}
	return ""
}

// takenBackWhy is the words a taken card carries (FieldTakenBack).
func (r FriendTakeReq) takenBackWhy() string {
	if r.Hold {
		return "taken back by the hold of friend " + r.Friend
	}
	if r.Reason == "" {
		return "taken back by the coordinator"
	}
	return "taken back by the coordinator: " + r.Reason
}

// FriendTake takes back the friend's cards she has not started: each one withdrawn on her
// row, its primary ready for the friends' deal. A card named that is not dealt to her, or
// that she has started, is refused, one refusal each, and so is a card named in flight
// (FriendCardInFlight) unless Working; the rest are taken; with AllOrNothing one refusal takes none,
// each card that would have been taken refused too. All (not Hold) takes ONLY the cards
// dealt to her and not taken: ready on her row, not in flight, not started; each card it
// leaves is said ("friend <f> keeps <id>: <why>"). A working card taken (Working, or the
// hold) frees her lane: her oldest ready card not taken is taken into working in the same
// unit, as her finish takes it (friendNext), and the plan says one WARNING line counting
// the working cards withdrawn.
// tla/WhoPreference.tla TakeBack.
func FriendTake(s *Snapshot, r FriendTakeReq) Plan {
	var p Plan
	row := FriendRow(r.Friend)
	mine := append(append([]*Card(nil), s.Fleet.Cell(row, Ready)...), s.Fleet.Cell(row, Working)...)
	SortCards(mine)
	var take []*Card
	if r.All {
		for _, c := range mine {
			// a hold takes started cards too: a card sitting on a held friend blocks every card
			// that needs it (the owner, 2026-10-04: "Held friends cards need to be
			// redistributed automatically"); the coordinator's own take keeps a started one
			switch {
			case r.Hold && r.Begun:
				if c.Col == Working || r.Started[c.ID] != "" {
					take = append(take, c)
				}
			case r.Hold:
				take = append(take, c)
			case r.Started[c.ID] != "":
				p.Said = append(p.Said, fmt.Sprintf("friend %s keeps %s: %s", r.Friend, c.ID, r.Started[c.ID]))
			case !r.Working && FriendCardInFlight(c) != "":
				// --all-unstarted is dealt and not taken, only: a card in flight is never
				// withdrawn by it (name it with --working)
				p.Said = append(p.Said, fmt.Sprintf("friend %s keeps %s: %s", r.Friend, c.ID, FriendCardInFlight(c)))
			default:
				take = append(take, c)
			}
		}
	}
	for _, id := range r.IDs {
		c := s.Fleet.Placed(id)
		if c == nil || c.F("kind") != "work" {
			if pr := s.Work.Placed(id); pr != nil {
				c = s.Fleet.Placed(WorkCardID(pr.ID, pr.Int("attempt")))
			}
		}
		switch {
		case c == nil:
			p.refuse(id, "no card "+id+" is dealt to friend "+r.Friend)
		case c.Row != row:
			p.refuse(id, fmt.Sprintf("%s is not dealt to friend %s: it is at %s", c.ID, r.Friend, placeOf(c)))
		case c.Col != Ready && c.Col != Working:
			p.refuse(id, fmt.Sprintf("%s has started: friend %s finished it (%s), and its primary %s is %s", c.ID, r.Friend, c.Col, c.F("primary"), orDash(s.StateOf(c.F("primary")))))
		case r.Started[c.ID] != "":
			p.refuse(id, fmt.Sprintf("%s has started: %s; it stays with friend %s and finishes", c.ID, r.Started[c.ID], r.Friend))
		case !r.Working && !r.Hold && FriendCardInFlight(c) != "":
			p.refuse(id, fmt.Sprintf("%s is in flight with friend %s: %s; withdrawing it mid-flight wants --working", c.ID, r.Friend, FriendCardInFlight(c)))
		case !slices.Contains(take, c):
			take = append(take, c)
		}
	}
	if r.AllOrNothing && len(p.Refused) > 0 {
		n := len(p.Refused)
		for _, c := range take {
			p.refuse(c.ID, fmt.Sprintf("not taken: --all-or-nothing, and %d of the cards named %s refused", n, map[bool]string{true: "was", false: "were"}[n == 1]))
		}
		return p
	}
	// her ready cards not taken, oldest first: each working card taken frees a lane one fills
	var next []*Card
	for _, c := range mine {
		// a held friend takes nothing into a lane her hold frees
		if c.Col == Ready && !slices.Contains(take, c) && !(r.Hold && r.Begun) {
			next = append(next, c)
		}
	}
	why := r.takenBackWhy()
	if !r.Hold {
		var working []string
		for _, c := range take {
			if FriendCardInFlight(c) != "" {
				working = append(working, c.ID)
			}
		}
		if len(working) > 0 {
			p.Said = append(p.Said, fmt.Sprintf("WARNING: withdraws %d working card(s) from friend %s mid-flight: %s", len(working), r.Friend, strings.Join(working, ",")))
		}
	}
	for _, c := range take {
		// the take, not the hold, keeps her from it; a card never taken from her has its whole
		// dealt bound again from now (untaken_since), its first take unset
		set, unset := map[string]string{FieldTakenBack: why, "untaken_since": stamp(s.Now)}, []string{"first_taken"}
		if r.Spends && isRead(c) {
			set["retired_by"] = RetiredByReturned // handed back: it spends her (readSpent)
		}
		if r.Hold {
			unset = append(unset, FieldTakenFrom)
		} else {
			set[FieldTakenFrom] = row
		}
		what := why + " from friend " + r.Friend
		if started := r.Started[c.ID]; started != "" {
			// only a hold takes a started card: its pushed head is carried to the next taker
			what += "; started (" + started + ")"
			if tip := PushedTip(started); tip != "" {
				set[FieldCarryHead], set[FieldCarryGen] = tip, strconv.Itoa(c.Int("gen"))
				what += fmt.Sprintf(": the next generation starts from its pushed head %s (gen %d)", tip, c.Int("gen"))
			} else {
				what += ": no push to carry"
			}
		}
		u := withdrawUnit(s, c, set, unset, NTakenBack, r.Who, what)
		if !r.Hold {
			// the mark the take writes (FieldTakenFrom): say it, so a quiet row is not a
			// mystery, and friend give is the way back
			u.Moved += fmt.Sprintf("; %s: back to ready, not dealt to %s again until friend give", c.ID, r.Friend)
		}
		if c.Col == Working && len(next) > 0 {
			n := next[0]
			next = next[1:]
			set, unset := friendTaken(s, n, r.Friend)
			u.Changes = append(u.Changes, change(Fleet, moveEntry(n, row, Working, set, unset...)))
			u.Moved += snapshotFriendPronouns(s, r.Friend).Sprintf("; %s ready -> working ({their} next, taken now)", n.ID)
		}
		p.Units = append(p.Units, u)
	}
	if !r.Hold && len(take) > 0 {
		p.Said = append(p.Said, fmt.Sprintf("marked=%d", len(take)))
	}
	return p
}

// FriendReadyMax is how long a work card may sit ready on a friend's row while she has a
// lane free before it is a judgment (a work card past its deadline, dealt and never
// taken): the deal takes it into the lane (in batch and one-shot mode alike), or her
// daemon or her session takes it (take --as friend.<name>), and a card none took is no one's
// (tla/FriendReadyTake.tla, NeverStrandedSilently).
const FriendReadyMax = 10 * time.Minute

// friendLaneIdle says the card is ready on a friend's row while she has a lane free (her
// working cards fewer than her lanes, friendRoom; seats is the tick's read of the friends),
// and names her. A card ready behind her lanes, all of them working, is her queue, held to
// the dealt bound as a member's is.
func friendLaneIdle(s *Snapshot, seats []FriendSeat, c *Card) (friend string, idle bool) {
	friend, ok := FriendOfRow(c.Row)
	if !ok || c.Col != Ready {
		return friend, false
	}
	for _, f := range seats {
		if f.Name == friend {
			_, lanes := friendRoom(f)
			return friend, s.Fleet.Count(c.Row, Working) < lanes
		}
	}
	return friend, false
}

// NGivenBack is the happened note of a card taken back from a friend given back to her.
const NGivenBack = "a card taken back from a friend given back"

// FriendGiveReq is friend give: the friend, the cards named (each a primary or its work
// card), and why.
type FriendGiveReq struct {
	Friend string
	IDs    []string
	Reason string
	Who    string
}

// friendSeatOf is the seat of the friend named in the snapshot's friends list; false when
// the snapshot carries no seat for her (the tier gate is then not judged).
func friendSeatOf(s *Snapshot, name string) (FriendSeat, bool) {
	for _, f := range s.Friends {
		if f.Name == name {
			return f, true
		}
	}
	return FriendSeat{}, false
}

// friendGiveEligibility is what the friend deal holds for the friend a give names, "" when the
// card may be handed to her: her tiers (friendTakes), her restriction (friendRestrictionAllows),
// the card's needs that have not landed (WaitsFor, its place in line too), and a hard pin
// (OnlyFriend, unless released: PinReleased) to another friend. A friend not up is not judged: a
// coordinator's give waits on her row, as it always did. The deal's checks, one helper; the card
// a-pinned-card-is-dealt-only-to-its-friend (same stream) writes the one predicate every
// placement path shares, and this helper is what it replaces here.
func friendGiveEligibility(s *Snapshot, seat FriendSeat, haveSeat bool, pr *Card, tier, friend string) string {
	if haveSeat && !friendTakes(s, seat, tier) {
		return snapshotFriendPronouns(s, friend).Sprintf("%s is a %s card, and friend %s's row does not serve tier %s ({their} tiers: %s)", pr.ID, tier, friend, tier, orDash(strings.Join(friendTiers(seat), ",")))
	}
	if haveSeat {
		if why := FriendRestrictionWhy(seat.Streams, seat.Kinds, pr.F("stream"), BriefKind(pr.F("brief"))); why != "" {
			return fmt.Sprintf("%s is outside friend %s's restriction: %s", pr.ID, friend, why)
		}
	}
	if pinned, _ := FriendCard(pr); OnlyFriend(pr) && pinned != friend && !PinReleased(s, pr, s.Friends) {
		return fmt.Sprintf("%s is pinned to friend %s (WHO: only friend): nothing is handed to friend %s; release the pin with nova-sprint unpin %s", pr.ID, orDash(pinned), friend, pr.ID)
	}
	if w := WaitsFor(s, pr, nil); len(w) > 0 {
		return fmt.Sprintf("%s needs %s, not landed: nothing is handed to friend %s", pr.ID, strings.Join(w, ","), friend)
	}
	return ""
}

// FriendGive is the seat's verb to a friend (docs/SPEC-SPRINT.md section 1, a friend's card
// taken back; tla/FriendGive.tla): on a card taken back from her (friend take) it clears the
// mark of the friend it was taken from (FieldTakenFrom), so the deal may deal it to her again;
// on a ready card whose current work card is not live (none, or Done as a brief-replaced bounce
// leaves it: only Ready or Working on a row is dealt, as friendDealPass judges) it is the hand
// the deal could not be told to make: the card is dealt to the friend named at its next
// attempt, not the queue head. A card not ready, one live on a row, one taken back and not from
// her, one whose eligibility she lacks (friendGiveEligibility) is refused, naming why; the rest
// are given, one refusal each.
func FriendGive(s *Snapshot, r FriendGiveReq) Plan {
	var p Plan
	row := FriendRow(r.Friend)
	reason := r.Reason
	if reason == "" {
		reason = "given back by the coordinator"
	}
	seat, haveSeat := friendSeatOf(s, r.Friend)
	declared := false
	seen := map[string]bool{}
	for _, id := range r.IDs {
		pr := s.Work.Placed(id)
		if c := s.Fleet.Placed(id); pr == nil && c != nil && c.F("kind") == "work" {
			pr = s.Work.Placed(c.F("primary"))
		}
		if pr == nil {
			p.refuse(id, "no card "+id)
			continue
		}
		if seen[pr.ID] {
			continue
		}
		seen[pr.ID] = true
		card := WorkCardID(pr.ID, pr.Int("attempt"))
		wc := s.Fleet.Placed(card)
		from := "no friend"
		if wc != nil && wc.F(FieldTakenFrom) != "" {
			from = "friend " + strings.TrimPrefix(wc.F(FieldTakenFrom), friendRowPrefix)
		}
		taken := wc != nil && wc.F(FieldTakenFrom) == row
		wcLive := wc != nil && (wc.Col == Ready || wc.Col == Working)
		tier := s.DealTier(pr)
		giveWhy := friendGiveEligibility(s, seat, haveSeat, pr, tier, r.Friend)
		switch {
		case pr.Col != Ready && pr.Col != Waiting:
			p.refuse(id, fmt.Sprintf("%s is %s, not ready or waiting", pr.ID, pr.Col))
			continue
		case taken:
			// the undo (friend take): clear the mark, as it did; the deal fills the rest
			// (docs/SPEC-SPRINT.md section 1, a friend's card taken back)
			n := happened(NGivenBack, pr.Row, s.Now, pr.ID)
			n.Who, n.What = r.Who, fmt.Sprintf("%s may be dealt to %s again (%s)", pr.ID, r.Friend, reason)
			p.Units = append(p.Units, Unit{Key: pr.ID, Stream: pr.Row,
				Changes: []Change{change(Fleet, setEntry(wc, nil, FieldTakenFrom))}, Notes: []Note{n}, Moved: n.What})
			p.Said = append(p.Said, pr.ID+": handed to "+r.Friend)
		case pr.Col != Ready:
			p.refuse(id, fmt.Sprintf("%s is %s, not ready: nothing is handed to friend %s", pr.ID, pr.Col, r.Friend))
		case wcLive:
			// only a live work card is dealt, as the friend deal judges it (friendDealPass: only a
			// Withdrawn one is a redeal, a Done one is dealt over at its next attempt)
			p.refuse(id, fmt.Sprintf("%s is already dealt: %s is %s, live; nothing is handed to friend %s (take it back first: nova-sprint friend take <friend> %s)", pr.ID, card, placeOf(wc), r.Friend, pr.ID))
		case wc != nil && wc.Col == Withdrawn:
			p.refuse(id, fmt.Sprintf("%s was never taken back from friend %s (taken from %s)", card, r.Friend, from))
		case giveWhy != "":
			p.refuse(id, giveWhy)
		default:
			// never taken back and never dealt: the seat hands a named ready card to her,
			// where the deal draws the queue head instead (docs/SPEC-SPRINT.md section 1,
			// a friend's card taken back)
			cardNext := WorkCardID(pr.ID, pr.Int("attempt")+1)
			if s.Fleet.Card(cardNext) != nil {
				p.refuse(id, fmt.Sprintf("%s: work card %s exists already", pr.ID, cardNext))
				continue
			}
			if !s.Fleet.HasRow(row) && !declared {
				p.Rows = append(p.Rows, RowAdd{Fleet, row})
				declared = true
			}
			u := friendDealUnit(s, pr, cardNext, row, Ready, tierNowSet(pr, tier))
			u.Moved += fmt.Sprintf("; %s: handed to %s", pr.ID, r.Friend)
			p.Units = append(p.Units, u)
			p.Said = append(p.Said, pr.ID+": handed to "+r.Friend)
		}
	}
	return p
}
