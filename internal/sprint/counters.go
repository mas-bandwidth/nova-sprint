package sprint

import (
	"strings"
	"time"
)

// Layer 2 of the processor (docs/SPEC-ISA.md, docs/SPEC-SPRINT.md): performance counters,
// built on cycle-time-breakdown's per-card stage stamps (cycletime.go).
// From those stamps, where --json gets per stream and overall:
// - IPC as landed cards per slot-hour (slots from the fleet widths over the window)
// - for each card the time it stalled by reason (waiting on a need, waiting on a release,
//   waiting on an external operand, waiting for a slot, in read, in merge, in rework),
//   whose parts sum to its wall time.
// The dashboard gets one row: IPC and the top stall reason.

const (
	StallNeed     = "waiting on a need"
	StallRelease  = "waiting on a release"
	StallExternal = "waiting on an external operand"
	StallSlot     = "waiting for a slot"
	StallRead     = "in read"
	StallMerge    = "in merge"
	StallRework   = "in rework"
)

// StallOrder is the canonical order of the 7 stall reasons.
var StallOrder = []string{
	StallNeed,
	StallRelease,
	StallExternal,
	StallSlot,
	StallRead,
	StallMerge,
	StallRework,
}

// CardCounters is one landed card's wall time and stall breakdown.
type CardCounters struct {
	ID       string             `json:"id"`
	Stream   string             `json:"stream"`
	WallS    float64            `json:"wall_s"`
	WorkS    float64            `json:"work_s,omitempty"`
	Stalls   map[string]float64 `json:"stalls"`
	TopStall string             `json:"top_stall,omitempty"`
}

// StreamCounters is one stream's counters over the window.
type StreamCounters struct {
	IPC      float64            `json:"ipc"`
	TopStall string             `json:"top_stall"`
	Stalls   map[string]float64 `json:"stalls"`
	Cards    []CardCounters     `json:"cards,omitempty"`
}

// CountersRecord is the performance counters over all streams and per stream.
type CountersRecord struct {
	IPC      float64                   `json:"ipc"`
	TopStall string                    `json:"top_stall"`
	Stalls   map[string]float64        `json:"stalls"`
	Cards    []CardCounters            `json:"cards,omitempty"`
	Streams  map[string]StreamCounters `json:"streams,omitempty"`
}

// TotalFleetSlots is the total width of up fleet members; default DefaultWidth when none.
func TotalFleetSlots(s *Snapshot) int {
	slots := 0
	if s != nil {
		for _, m := range s.UpMembers() {
			slots += s.Width(m)
		}
	}
	if slots == 0 {
		slots = DefaultWidth
	}
	return slots
}

// CardStallBreakdown computes the stall breakdown and wall time for one landed card.
func CardStallBreakdown(s *Snapshot, c *Card) CardCounters {
	landed := stampAt(c, "landed")
	admitted := stampAt(c, "admitted")
	wall := 0.0
	if !landed.IsZero() && !admitted.IsZero() && landed.After(admitted) {
		wall = landed.Sub(admitted).Seconds()
	}

	stalls := map[string]float64{
		StallNeed:     0,
		StallRelease:  0,
		StallExternal: 0,
		StallSlot:     0,
		StallRead:     0,
		StallMerge:    0,
		StallRework:   0,
	}

	ready := cardReadyAt(s, c)
	dealt := cardDealtAt(s, c)
	taken := cardTakenAt(s, c)
	finished := cardFinishedAt(s, c)
	accepted := stampAt(c, "accepted")
	queued := cardQueuedAt(s, c)

	// Waiting stage: admitted to ready
	if !ready.IsZero() && !admitted.IsZero() && ready.After(admitted) {
		waitSec := ready.Sub(admitted).Seconds()
		switch {
		case c.F("wait_for") == "release" || c.F(FieldHeld) != "" || (c.F("kind") == "wait" && c.F("wait_for") == "release"):
			stalls[StallRelease] = waitSec
		case c.F("wait_for") == "external" || strings.HasPrefix(c.F("needs"), "external:") || strings.HasPrefix(c.F("wait_for"), "external:"):
			stalls[StallExternal] = waitSec
		default:
			stalls[StallNeed] = waitSec
		}
	}

	// In rework:
	reworkS := cardReworkS(s, c)
	if reworkS > 0 {
		stalls[StallRework] = float64(reworkS)
	}

	// Waiting for a slot: ready to taken (or dealt if taken not stamped), minus rework
	if reworkS > 0 {
		if !dealt.IsZero() && !taken.IsZero() && taken.After(dealt) {
			stalls[StallSlot] = taken.Sub(dealt).Seconds()
		} else if !taken.IsZero() && !ready.IsZero() && taken.After(ready) {
			slotSec := taken.Sub(ready).Seconds() - float64(reworkS)
			if slotSec > 0 {
				stalls[StallSlot] = slotSec
			}
		}
	} else {
		startSlot := ready
		if startSlot.IsZero() {
			startSlot = admitted
		}
		endSlot := taken
		if endSlot.IsZero() {
			endSlot = dealt
		}
		if !endSlot.IsZero() && !startSlot.IsZero() && endSlot.After(startSlot) {
			stalls[StallSlot] = endSlot.Sub(startSlot).Seconds()
		}
	}

	// Work duration (if any)
	workSec := 0.0
	if !finished.IsZero() && !taken.IsZero() && finished.After(taken) {
		workSec = finished.Sub(taken).Seconds()
	}

	// In read: finished to accepted
	if !accepted.IsZero() && !finished.IsZero() && accepted.After(finished) {
		stalls[StallRead] = accepted.Sub(finished).Seconds()
	}

	// In merge: queued to landed
	if !landed.IsZero() && !queued.IsZero() && landed.After(queued) {
		stalls[StallMerge] = landed.Sub(queued).Seconds()
	}

	// Determine top stall for this card
	topReason, maxSec := "-", 0.0
	for _, reason := range StallOrder {
		if sec := stalls[reason]; sec > maxSec {
			maxSec = sec
			topReason = reason
		}
	}

	return CardCounters{
		ID:       c.ID,
		Stream:   c.Row,
		WallS:    wall,
		WorkS:    workSec,
		Stalls:   stalls,
		TopStall: topReason,
	}
}

// Counters computes IPC and stall reasons overall and per stream for landed cards in window.
func Counters(s *Snapshot, now time.Time, opts ...time.Duration) CountersRecord {
	window := CycleWindow
	if len(opts) > 0 && opts[0] > 0 {
		window = opts[0]
	}

	slots := TotalFleetSlots(s)
	windowHours := window.Hours()
	slotHours := float64(slots) * windowHours

	overallStalls := map[string]float64{}
	for _, r := range StallOrder {
		overallStalls[r] = 0
	}
	byStreamStalls := map[string]map[string]float64{}
	byStreamCards := map[string][]CardCounters{}
	var allCards []CardCounters

	if s != nil {
		for _, c := range s.Work.Column(Landed) {
			if IsSentinel(c) {
				continue
			}
			landed := stampAt(c, "landed")
			if landed.IsZero() || landed.After(now) || now.Sub(landed) > window {
				continue
			}
			cc := CardStallBreakdown(s, c)
			allCards = append(allCards, cc)
			st := c.Row
			if byStreamStalls[st] == nil {
				byStreamStalls[st] = map[string]float64{}
				for _, r := range StallOrder {
					byStreamStalls[st][r] = 0
				}
			}
			byStreamCards[st] = append(byStreamCards[st], cc)

			for r, sec := range cc.Stalls {
				overallStalls[r] += sec
				byStreamStalls[st][r] += sec
			}
		}
	}

	findTop := func(stalls map[string]float64) string {
		top, maxS := "-", 0.0
		for _, r := range StallOrder {
			if s := stalls[r]; s > maxS {
				maxS = s
				top = r
			}
		}
		return top
	}

	overallIPC := 0.0
	if slotHours > 0 {
		overallIPC = float64(len(allCards)) / slotHours
	}

	streams := map[string]StreamCounters{}
	for st, cards := range byStreamCards {
		streamIPC := 0.0
		if slotHours > 0 {
			streamIPC = float64(len(cards)) / slotHours
		}
		streams[st] = StreamCounters{
			IPC:      streamIPC,
			TopStall: findTop(byStreamStalls[st]),
			Stalls:   byStreamStalls[st],
			Cards:    cards,
		}
	}

	return CountersRecord{
		IPC:      overallIPC,
		TopStall: findTop(overallStalls),
		Stalls:   overallStalls,
		Cards:    allCards,
		Streams:  streams,
	}
}
