package sprint

import (
	"fmt"
	"strconv"
	"strings"
)

// A member is a fleet machine, and its width is the machine's child cap: the
// most work cards it runs at once. It holds up to DealAhead times its width,
// ready and working together: its width working and as many again ready
// behind them, so the sprint feeds the fleet at width and keeps it working at
// that width until done. nova-config is THE one singular definition of width
// (docs/SPEC-SPRINT.md section 5; tla/Level.tla, Width): a member's width is
// the width field of its machine:<m> hash as nova-config apply wrote it, read
// with the snapshot on every tick (store.Widths, Snapshot.Widths); nothing is
// cached across ticks, so a config change applies at the next tick with no
// restart and no sync. The deal fills every member free lanes first (a member
// below its width takes the card, so a lane that is free takes the next card
// at once), then, only when no member that may take the card has one, the 2x
// room, DealAhead times its width, round the fleet from the cursor (round.go
// nextFree; T3, R6); the level, once at the start of every tick, moves ready
// cards from a member that cannot start them to one with free lanes, never
// past DealAhead times its width (T4, R7). The model is
// tla/SprintEvents.tla: PlanDeal's room (its Cap), which counts the ready
// cards of a member; the room here counts ready and working.
const (
	// FieldWidth is the fleet table's width column, holding the member's
	// width as nova-config set it. It is never a field the sprint writes:
	// a width is nova-config's alone.
	FieldWidth = "width"
	// MaxWidth is the widest a member may be: a width over it is refused
	// before anything is written.
	MaxWidth = 1024
	// TickMaxDeal is the most cards one deal moves (T3, R6). The deal is one
	// plan; the store's apply cuts it into parts under the table layer's
	// entry bounds, so the deal refuses nothing by size under this.
	TickMaxDeal = 2000
)

// ParseWidth is a width as nova-config's machine row holds it: a whole number
// from 1 to MaxWidth. Width 0 is the drain, held and read separately
// (DrainWidth).
func ParseWidth(text string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 1 || n > MaxWidth {
		return 0, fmt.Errorf("a width wants a whole number from 1 to %d, got %q", MaxWidth, text)
	}
	return n, nil
}

// DrainWidth is the width cell of a member that drains: nova-config machine
// set <m> --width 0 writes it, and it is no member of the deal, the level or
// a take by count (every room is DealAhead times 0) while its working cards
// finish, unlike fleet down, which deals them again elsewhere (the comfort
// list of 2026-10-03, item 5).
const DrainWidth = "0"

// Width is the member's width as nova-config's machine:<m> hash wrote it, read
// with the snapshot (Snapshot.Widths). A member whose hash is missing, whose
// width field is empty or not a whole number, and a member that drains (width
// 0) all read 0: dealt nothing (WidthMissing says whether a judgment names
// nova-config).
func (s *Snapshot) Width(member string) int {
	if s == nil {
		return 0
	}
	w := strings.TrimSpace(s.Widths[member])
	if w == DrainWidth {
		return 0
	}
	if n, err := ParseWidth(w); err == nil {
		return n
	}
	return 0
}

// WidthMissing says the member's width is not in nova-config: its machine:<m>
// hash is missing, or its width field is empty or not a whole number. The
// deal and the level deal it nothing and the tick writes one judgment per
// member and episode, naming the remedy.
func (s *Snapshot) WidthMissing(member string) bool {
	if s == nil {
		return true
	}
	w, ok := s.Widths[member]
	if !ok {
		return true
	}
	w = strings.TrimSpace(w)
	if w == DrainWidth {
		return false
	}
	_, err := ParseWidth(w)
	return err != nil
}

// WidthRemedy is the word of the width-not-in-nova-config judgment.
func WidthRemedy(member string) string {
	return "nova-config machine set " + member + " --width <n> --actor <name>, then nova-config apply"
}

// NWidthNotInConfig is the one judgment the tick raises per member and episode
// whose machine:<m> width is missing, empty or not a whole number: the deal
// deals it nothing until the width is in nova-config (WidthMissing).
const NWidthNotInConfig = "width not in nova-config"

// widthNotInConfigConds is the condition of each up member whose width is not
// in nova-config, held while it is: notify writes its judgment once per member
// and episode (an open one is not written again; a wait parks it and raises it
// again when it runs out) and closes it the tick the width is back.
func widthNotInConfigConds(s *Snapshot) []cond {
	var out []cond
	for _, m := range s.UpMembers() {
		if !s.WidthMissing(m) {
			continue
		}
		out = append(out, cond{typ: NWidthNotInConfig, stream: MemberSubject(m), streamLevel: true,
			decisions: TickDecisions[NWidthNotInConfig],
			what:      "member " + m + " has its width not in nova-config: the deal deals it nothing; run: " + WidthRemedy(m)})
	}
	return out
}

// WidthText is the fleet table's width cell of a member: the config width as
// nova-config wrote it, or "" when it is missing (dealt nothing).
func WidthText(ctl *Card) string {
	if ctl == nil {
		return ""
	}
	return strings.TrimSpace(ctl.F(FieldWidth))
}

// MemberSpec is a member as init --members names it: <name> only. A width is
// nova-config's alone: init takes no width (Width is kept only so old callers
// compile, and is always 0).
type MemberSpec struct {
	Name  string
	Width int
}

// ParseMembers is init --members: a comma list of <name>. A width after a
// colon is refused: "a width is nova-config's alone".
func ParseMembers(list string) ([]MemberSpec, error) {
	var out []MemberSpec
	for _, x := range Split(list) {
		name, w, hasWidth := strings.Cut(x, ":")
		if !ValidID(name) {
			return nil, fmt.Errorf("a member name wants letters, digits, _ and -: %q", x)
		}
		if hasWidth {
			return nil, fmt.Errorf("a width is nova-config's alone: %s --width %s; run: nova-config machine set %s --width %s", name, w, name, w)
		}
		out = append(out, MemberSpec{Name: name})
	}
	return out, nil
}

// memberLoads is each up member's work cards held: its ready and its working
// cells, the count its width bounds.
//
// A read card on the row holds half a slot (read_cards.go, halfLoad): a row of width w
// holds w work cards or 2w reads or any mix.
func memberLoads(s *Snapshot, up []string) map[string]int {
	q := map[string]int{}
	for _, m := range up {
		q[m] = halfLoad(rowLoad(s, m))
	}
	return q
}

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
// before it holds DealAhead times its width. The width is the config's as this
// tick read it (Snapshot.Width; tla/Level.tla, its room and Width): a width
// lowered under a member's working cards leaves them where they are and the
// room at 0 until working < width (docs/SPEC-SPRINT.md section 5).
func widthRoom(s *Snapshot, up []string) int {
	room := 0
	for _, m := range up {
		room += max(0, DealAhead*s.Width(m)-halfLoad(rowLoad(s, m)))
	}
	return room
}
