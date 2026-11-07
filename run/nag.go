package run

import (
	"context"
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Nagger is the ready nag (Glenn 2026-11-07: "We work wide so work is done
// as quickly as possible. By leaving cards in ready we are violating this
// promise."): while a hooked row holds ready consumers, every policy
// timeouts() ready_nag seconds of the run's clock after the DEAL it is sent
// one READY message, Glenn's words then each ready card's take line (the
// DEAL's). It stops the moment the row has none ready.
type Nagger struct{ last map[string]time.Time }

func (n *Nagger) Nag(ctx context.Context, b bus.Bus, s store.Snapshot, ro roster.Roster, every time.Duration, now time.Time) {
	if n.last == nil {
		n.last = map[string]time.Time{}
	}
	ready, working := map[string][]pending{}, map[string]int{}
	for _, id := range sortedCardIDs(s.Cards) {
		c := s.Cards[id]
		if c.Kind == "work" || c.Kind == "read" {
			if c.State == "ready" {
				ready[c.Holder] = append(ready[c.Holder], pending{notice: Notice{Subject: "READY", Line: dealNotice(c, s.Epoch).Line}})
			}
			working[c.Holder] += b2i(c.State == "working")
		}
	}
	for _, r := range recipients(s) {
		last, seen := n.last[r]
		switch {
		case len(ready[r]) == 0:
			delete(n.last, r)
		case !seen: // the DEAL has just gone
			n.last[r] = now
		case now.Sub(last) >= every:
			n.last[r] = now
			width := max(ro.Kin[r].Width, ro.Workers[r].Width)
			head := Notice{Subject: "READY", Line: fmt.Sprintf("you have a new ready card. you have %d ready cards, you are currently working on %d, and your width is %d. Please work to width!", len(ready[r]), working[r], width)}
			subject, body := envelope(r, append([]pending{{notice: head}}, ready[r]...))
			_, _ = b.Send(ctx, r, subject, body)
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
