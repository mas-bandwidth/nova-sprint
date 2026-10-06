package sprint

import (
	"cmp"
	"slices"
	"time"
)

// The performance counters (docs/SPEC-SPRINT.md, processor-counters; layer 2 of the
// processor): IPC, the cards landed per slot-hour, and where each landed card's wall time
// went, by stall reason. Both are read from the stage stamps the cards already carry
// (cycletime.go), never from a second store of times, by CycleTimes over the same cards
// in the same CycleWindow. They are the instrument the later layers are gated on: a layer
// that removes a stall is kept only if these show that stall dominating.

// The stall reasons, a card's wall time from its add to its landing split among them.
// Work is no stall (the card is being executed) but is a part, so the parts sum to the
// wall time; the top stall is never work.
const (
	StallNeed     = "need"     // waiting on a need to land
	StallRelease  = "release"  // waiting on a release: admitted held, or behind a sentinel
	StallExternal = "external" // waiting on an external operand (no step has one yet)
	StallRework   = "rework"   // a reworked card's earlier attempts, ready to its final deal
	StallSlot     = "slot"     // waiting for a slot: ready to dealt, dealt to taken
	StallWork     = "work"     // taken to finished
	StallRead     = "read"     // finished to accepted: the wait for a read, the reads, the accept
	StallMerge    = "merge"    // accepted to landed
)

// StallOrder is the reasons in the order a card meets them.
var StallOrder = []string{StallNeed, StallRelease, StallExternal, StallRework, StallSlot, StallWork, StallRead, StallMerge}

// CardStalls is one landed card's wall time, add to landing, and its parts by reason, in
// seconds; the parts sum to the wall time, and a reason the card spent no time in is absent.
type CardStalls struct {
	ID     string             `json:"id"`
	Stream string             `json:"stream"`
	WallS  float64            `json:"wall_s"`
	Stalls map[string]float64 `json:"stalls_s"`
}

// Counter is the counters over a set of landed cards: how many landed on a machine's lane,
// the machine slot-hours in the window, IPC (the one over the other), the stall parts
// summed over every card, and the top stall (the largest part that is not work).
type Counter struct {
	Landed    int                `json:"landed"`
	SlotHours float64            `json:"slot_hours"`
	IPC       float64            `json:"ipc"`
	Stalls    map[string]float64 `json:"stalls_s"`
	Top       string             `json:"top_stall,omitempty"`
}

// Counters is the counters over every stream and per stream, and each card's parts in the
// order the cards landed.
type Counters struct {
	All     Counter            `json:"all"`
	Streams map[string]Counter `json:"streams,omitempty"`
	Cards   []CardStalls       `json:"cards,omitempty"`
}

// countersOf is the counters of the landed primaries cs (each with its landing time) at now.
// A card with no add stamp, or landed before it, has no wall time and is left out.
func countersOf(s *Snapshot, cs []*Card, now time.Time) *Counters {
	type landing struct {
		c  *Card
		at time.Time
	}
	var ls []landing
	for _, c := range cs {
		ls = append(ls, landing{c, stampAt(c, "landed")})
	}
	slices.SortStableFunc(ls, func(a, b landing) int {
		if d := a.at.Compare(b.at); d != 0 {
			return d
		}
		return cmp.Compare(a.c.ID, b.c.ID)
	})
	hours := slotHours(s, now)
	out := &Counters{All: Counter{SlotHours: hours, Stalls: map[string]float64{}}, Streams: map[string]Counter{}}
	for _, l := range ls {
		parts, wall, ok := stallsOf(s, l.c, l.at)
		if !ok {
			continue
		}
		out.Cards = append(out.Cards, CardStalls{ID: l.c.ID, Stream: l.c.Row, WallS: wall, Stalls: parts})
		st, ok := out.Streams[l.c.Row]
		if !ok {
			st = Counter{SlotHours: hours, Stalls: map[string]float64{}}
		}
		for _, k := range []*Counter{&out.All, &st} {
			if onMachineLane(s, l.c) {
				k.Landed++
			}
			for r, d := range parts {
				k.Stalls[r] += d
			}
		}
		out.Streams[l.c.Row] = st
	}
	if len(out.Cards) == 0 {
		return nil
	}
	finish := func(k *Counter) {
		if k.SlotHours > 0 {
			k.IPC = float64(k.Landed) / k.SlotHours
		}
		k.Top = topStall(k.Stalls)
	}
	finish(&out.All)
	for name, st := range out.Streams {
		finish(&st)
		out.Streams[name] = st
	}
	return out
}

// stallsOf is a landed card's parts by reason and its wall time, in seconds. The card's
// stamps, in the order wall time passes them, each end a span of one reason: released (a
// held card's release), ready_at (the reason it waited, waitReason), dealt_at (the slot,
// or the rework of a reworked card), taken_at (the slot), finished_at (work), accepted
// (read) and its landing (merge). A stamp that is missing or out of order ends no span:
// its time goes to the next stamp's reason, so the parts always sum to the wall time.
func stallsOf(s *Snapshot, c *Card, landed time.Time) (map[string]float64, float64, bool) {
	admitted := stampAt(c, "admitted")
	if admitted.IsZero() || landed.IsZero() || landed.Before(admitted) {
		return nil, 0, false
	}
	ready := stampAt(c, FieldReadyAt)
	deal := StallSlot
	if c.F(FieldReworkS) != "" {
		deal = StallRework
	}
	marks := []struct {
		at  time.Time
		why string
	}{
		{stampAt(c, "released"), StallRelease},
		{ready, waitReason(s, c, ready)},
		{stampAt(c, FieldDealtAt), deal},
		{stampAt(c, FieldTakenAt), StallSlot},
		{stampAt(c, FieldFinishedAt), StallWork},
		{stampAt(c, "accepted"), StallRead},
		{landed, StallMerge},
	}
	parts := map[string]float64{}
	prev := admitted
	for _, m := range marks {
		if m.at.IsZero() || m.at.Before(prev) || m.at.After(landed) {
			continue
		}
		if d := m.at.Sub(prev).Seconds(); d > 0 {
			parts[m.why] += d
		}
		prev = m.at
	}
	return parts, landed.Sub(admitted).Seconds(), true
}

// waitReason is why the card waited from its add to ready: the last of what it waited
// for to land before ready (a need it names, or a sentinel of its stream before it in
// line) is a sentinel, released by the coordinator, or a card, a need.
func waitReason(s *Snapshot, c *Card, ready time.Time) string {
	var last *Card
	var at time.Time
	consider := func(x *Card) {
		if l := stampAt(x, "landed"); !l.IsZero() && !l.After(ready) && (last == nil || l.After(at)) {
			last, at = x, l
		}
	}
	for _, n := range Split(c.F("needs")) {
		if x := s.Work.Placed(n); x != nil {
			consider(x)
		}
	}
	for _, x := range s.Work.Cell(c.Row, Landed) {
		if IsSentinel(x) && x.Score < c.Score {
			consider(x)
		}
	}
	if last != nil && IsSentinel(last) {
		return StallRelease
	}
	return StallNeed
}

// onMachineLane says the card's landed attempt ran on a machine's lane, not a friend's:
// IPC counts the cards landed on the slots it counts.
func onMachineLane(s *Snapshot, c *Card) bool {
	if w := s.Fleet.Placed(c.F("work")); w != nil {
		return !IsFriendRow(w.Row)
	}
	_, friend := FriendCard(c)
	return !friend
}

// slotHours is the machines' slot-hours in the CycleWindow before now: each machine up
// now, its width times the hours from when it came up (its since, the window's start at
// the earliest). A machine down now counts none: its earlier time up is on no card.
// Friends' lanes are not the fleet table's widths and are not counted.
func slotHours(s *Snapshot, now time.Time) float64 {
	start := now.Add(-CycleWindow)
	h := 0.0
	for _, m := range s.UpMembers() {
		from := stampAt(s.MemberCtl(m), "since")
		if from.Before(start) {
			from = start
		}
		if from.Before(now) {
			h += float64(s.Width(m)) * now.Sub(from).Hours()
		}
	}
	return h
}

// topStall is the largest part that is not work, the first in StallOrder on a tie; ""
// when there is none.
func topStall(parts map[string]float64) string {
	top := ""
	for _, r := range StallOrder {
		if r != StallWork && parts[r] > 0 && (top == "" || parts[r] > parts[top]) {
			top = r
		}
	}
	return top
}
