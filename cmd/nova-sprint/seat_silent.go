package main

import (
	"context"
	"strconv"
	"strings"
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
// again: Group.Before) to the owner's name. A raise is marked told only once its send
// succeeded: a failed send is tried again at the next look, under the same bus token
// (seatSilentToken), so a send whose response was lost, or a server restarted between a
// send and its mark, never puts a second message on the bus (pkg/bus/token.go: the bus
// answers a token it has seen with the message it sent). Raises of judgments no longer open
// are forgotten at each look.

// seatSilentLook is the least time between two looks for the judgment.
const seatSilentLook = 5 * time.Second

// seatSilentTold is each raise of the judgment told to the owner, by its token, and the last
// look.
type seatSilentTold struct {
	mu     sync.Mutex
	told   map[string]bool
	lookAt time.Time
}

// seatSilentToken is the bus token of one raise of the judgment: the sprint (its key
// prefix), its epoch, the judgment's id and the raise, so the same raise is the same send
// across retries and restarts, and a raise of another sprint or epoch is another.
// tla/SprintRules.tla PutMsg
func seatSilentToken(prefix string, epoch uint64, id string, raise int) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r) {
				return r
			}
			return '_'
		}, s)
	}
	t := "seat-silent:" + clean(prefix) + ":" + strconv.FormatUint(epoch, 10) + ":" + clean(id) + ":" + strconv.Itoa(raise)
	if len(t) > bus.MaxToken {
		t = t[len(t)-bus.MaxToken:]
	}
	return t
}

// tellSeatSilent sends each raise of NSeatSilent and NNeedBlocked not yet told to the sprint's owner on the
// bus, and says it on the server's log; a sprint with no owner is said on the log alone.
// tla/SprintRules.tla SendOK
// tla/NeedsBlocked.tla BlockedDependentsCovered
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
	es, err := st.EpochNow(ctx)
	if err != nil {
		a.serveSay("SEAT SILENT: the epoch could not be read: %s", oneline.Escape(err.Error()))
		return
	}
	open := map[string]bool{}
	for _, g := range groups {
		if g.Type != sprint.NSeatSilent && g.Type != sprint.NNeedBlocked {
			continue
		}
		token := seatSilentToken(st.Names.Prefix, es.N, g.ID, g.Before)
		open[token] = true
		a.seatSilent.mu.Lock()
		told := a.seatSilent.told[token]
		a.seatSilent.mu.Unlock()
		if told {
			continue
		}
		a.serveSay("SEAT SILENT %s raise=%d: %s", g.ID, g.Before, oneline.Quote(g.What))
		owner, err := a.owner(ctx, st)
		if err != nil {
			a.serveSay("SEAT SILENT %s: the owner could not be read, tried again at the next look: %s", g.ID, oneline.Escape(err.Error()))
			continue
		}
		if owner == "" {
			a.serveSay("SEAT SILENT %s: the sprint names no owner: told on this line only", g.ID)
		} else {
			m := bus.Message{From: "nova-sprint", To: []string{owner}, Kind: bus.KindBlocker, Subject: g.Type, Body: g.What, Token: token}
			if err := a.bus(ctx, m, func(l string) { a.serveSay("%s", l) }); err != nil {
				a.serveSay("SEAT SILENT %s: the push to %s failed, tried again at the next look under the same token: %s", g.ID, owner, oneline.Escape(err.Error()))
				continue
			}
		}
		a.seatSilent.mu.Lock()
		if a.seatSilent.told == nil {
			a.seatSilent.told = map[string]bool{}
		}
		a.seatSilent.told[token] = true
		a.seatSilent.mu.Unlock()
	}
	// forget every raise no longer open: its judgment closed, or raised again since
	a.seatSilent.mu.Lock()
	for token := range a.seatSilent.told {
		if !open[token] {
			delete(a.seatSilent.told, token)
		}
	}
	a.seatSilent.mu.Unlock()
}
