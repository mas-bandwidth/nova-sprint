package verbs

import (
	"context"
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// chainWarn is the NEEDS chain length at which add and check warn
// (steps.ChainWarn, the board's too).
const chainWarn = steps.ChainWarn

// landLookback is how many of the store's last events the land rate reads:
// on a busy sprint about an hour (a guess, not measured), and a bounded
// read however long the sprint has run.
const landLookback = 5000

// flattenFix is how a long chain is made short (CORRECT-USE, "Keep
// everybody at width").
const flattenFix = "flatten it before the deal: a NEEDS only on an output the card consumes, independent work as cards that need nothing, one card per file set (CORRECT-USE: keep everybody at width)"

// checkWaits is one line per stream with producers waiting, why they wait
// (steps.Waits: the front of the chain they wait on, the producer and file
// they are held behind), and a warning for each stream whose longest NEEDS
// chain is chainWarn cards or more, with what it takes at the stream's land
// rate.
func (e *Engine) checkWaits(ctx context.Context, r *checkReport, snap store.Snapshot, ws []steps.StreamWaits) {
	var times map[string][]time.Time
	var terr error
	for _, s := range ws {
		if line := s.Line(); line != "" {
			r.ok("waits %s: %s", s.Stream, line)
		}
		if len(s.Chain) < chainWarn {
			continue
		}
		if times == nil && terr == nil {
			times, terr = e.landTimes(ctx, snap)
		}
		r.warn(chainLine(s.Stream, s.Chain, times[s.Stream], terr), flattenFix)
	}
}

// chainLine is a stream's longest NEEDS chain and what it takes at the
// stream's land rate.
func chainLine(stream string, chain []string, times []time.Time, terr error) string {
	return fmt.Sprintf("stream %s: its longest NEEDS chain is %d cards: %s; %s", stream, len(chain), steps.ChainText(chain), chainTime(times, len(chain), terr))
}

// chainTime is what n cards one after another take at the land rate the
// landing times show, or why there is no rate.
func chainTime(times []time.Time, n int, terr error) string {
	if terr != nil {
		return "the land rate is unreadable (" + errText(terr) + ")"
	}
	if len(times) < 2 || !times[len(times)-1].After(times[0]) {
		return "no land rate yet (fewer than two landings in the store's recent history)"
	}
	perHour := float64(len(times)-1) / times[len(times)-1].Sub(times[0]).Hours()
	return fmt.Sprintf("at the stream's land rate (%.1f an hour over its last %d landings) that is about %.1f hours", perHour, len(times), float64(n)/perHour)
}

// landTimes is when each stream's producers landed, oldest first, over the
// store's last landLookback events of the snapshot's epoch.
func (e *Engine) landTimes(ctx context.Context, snap store.Snapshot) (map[string][]time.Time, error) {
	out := map[string][]time.Time{}
	for cursor := max(0, snap.Seq-landLookback); cursor < snap.Seq; {
		batch, err := e.Store.Events(ctx, cursor, store.MaxEvents)
		if err != nil {
			return nil, fmt.Errorf("reading the landed history after seq %d: %w", cursor, err)
		}
		if len(batch) == 0 {
			break
		}
		for _, ev := range batch {
			if ev.Seq <= snap.Seq {
				landings(ev, snap.Epoch, out)
			}
			cursor = ev.Seq
		}
	}
	return out, nil
}

// landings adds the producers ev moved into landed.
func landings(ev store.Event, epoch int, out map[string][]time.Time) {
	if !ev.Accepted || ev.After == nil || ev.Epoch != epoch {
		return
	}
	for id, c := range ev.After.Cards {
		if c.Kind != "producer" || c.State != "landed" {
			continue
		}
		if ev.Before != nil {
			if b, ok := ev.Before.Cards[id]; ok && b.State == "landed" {
				continue
			}
		}
		out[c.Stream] = append(out[c.Stream], ev.At)
	}
}
