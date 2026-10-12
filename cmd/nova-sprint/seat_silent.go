package main

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// The seat is not answering (internal/sprint/seat_answers.go; docs/SPEC-SPRINT.md section 8,
// "A judgment that holds a card"): the judgment reaches the seat's hook as every judgment
// does, and the owner on the bus too, so a seat whose hook has lapsed, or that is busy
// elsewhere, never leaves a card held unseen. The run loop looks after each tick, at most
// every seatSilentLook, and sends each raise of the judgment (its first, and each raise
// again: Group.Before) once, to the owner's name.

// seatSilentLook is the least time between two looks for the judgment.
const seatSilentLook = 5 * time.Second

// seatSilentTold is each raise of the judgment told to the owner, and the last look.
type seatSilentTold struct {
	mu     sync.Mutex
	told   map[string]bool
	lookAt time.Time
}

// tellSeatSilent sends each raise of NSeatSilent not yet told to the sprint's owner on the
// bus, and says it on the server's log; a sprint with no owner is said on the log alone.
func (a *app) tellSeatSilent(ctx context.Context, st *store.Store) {
	now := a.now()
	a.seatSilent.mu.Lock()
	if !a.seatSilent.lookAt.IsZero() && now.Sub(a.seatSilent.lookAt) < seatSilentLook && now.After(a.seatSilent.lookAt) {
		a.seatSilent.mu.Unlock()
		return
	}
	a.seatSilent.lookAt = now
	a.seatSilent.mu.Unlock()
	groups, err := st.OpenGroups(ctx)
	if err != nil {
		a.serveSay("SEAT SILENT: the open judgments could not be read: %s", oneline.Escape(err.Error()))
		return
	}
	for _, g := range groups {
		if g.Type != sprint.NSeatSilent {
			continue
		}
		key := g.ID + "#" + strconv.Itoa(g.Before)
		a.seatSilent.mu.Lock()
		if a.seatSilent.told == nil {
			a.seatSilent.told = map[string]bool{}
		}
		told := a.seatSilent.told[key]
		a.seatSilent.told[key] = true
		a.seatSilent.mu.Unlock()
		if told {
			continue
		}
		a.serveSay("SEAT SILENT %s raise=%d: %s", g.ID, g.Before, oneline.Quote(g.What))
		owner, err := a.owner(ctx, st)
		if err != nil || owner == "" {
			a.serveSay("SEAT SILENT %s: the sprint names no owner: told on this line only", g.ID)
			continue
		}
		m := bus.Message{From: "nova-sprint", To: []string{owner}, Kind: bus.KindBlocker, Subject: sprint.NSeatSilent, Body: g.What}
		if err := a.bus(ctx, m, func(l string) { a.serveSay("%s", l) }); err != nil {
			a.serveSay("SEAT SILENT %s: the push to %s failed: %s", g.ID, owner, oneline.Escape(err.Error()))
		}
	}
}
