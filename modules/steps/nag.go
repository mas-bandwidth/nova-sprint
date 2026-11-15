package steps

import (
	"cmp"
	"fmt"
	"time"
)

// MergingNagCard is the merge-queue nag's key in the pusher (Nudge.Card):
// one nudge for the whole queue, never one per card, so a harness's wake
// tool that stops a source flooding it still gets one line per interval.
const MergingNagCard = "merging-nag"

// mergingNag is the merge-queue nag (Glenn 2026-11-15: "You should be
// getting pested every 1s when cards are in the merge queue longer than 5
// minutes"; tla/MergeNag.tla): while any producer has been merging longer
// than timeouts merging_nag (under a land judgement or not; a card from
// before the view counts from Since), the coordinator's row, on the bus,
// gets one nudge every merging_nag_every with how many are past it, the
// oldest and its age, and the command that clears that one: its open land
// judgement's, else its hand landing. None past it is no nudge, so the
// nag stops the tick the queue clears.
func (t *tick) mergingNag() []Nudge {
	limit := seconds(cmp.Or(t.tmo.MergingNag, Engine.Timeouts.MergingNag))
	every := seconds(cmp.Or(t.tmo.MergingNagEvery, Engine.Timeouts.MergingNagEvery))
	row := t.coordinatorRow()
	if r, ok := t.v.Rows[row]; !ok || !Hooked(r) || !t.present(row) {
		return nil // the bus only queues to an absent row: a nag a second would pile up there
	}
	n, oldest, age := 0, "", time.Duration(0)
	for _, p := range t.v.Producers("merging") {
		at, ok := t.v.MergingAt[p]
		if !ok {
			at = t.v.Since
		}
		a := t.in.Now.Sub(at)
		if a <= limit {
			continue
		}
		n++
		if a > age {
			oldest, age = p, a
		}
	}
	if n == 0 {
		return nil
	}
	c := t.v.Cards[oldest]
	why, cmd := "with no land judgement (its landing is in flight)", fmt.Sprintf("nova-sprint land %s --head %s --reason \"<why>\"", ShellWord(oldest), c.Head)
	for _, j := range t.v.Judgements {
		if j.Kind == "land" && j.About == oldest && j.State == "open" {
			why, cmd = "under land judgement "+j.ID, j.Command
		}
	}
	return []Nudge{{Row: row, Card: MergingNagCard, Every: every, Line: fmt.Sprintf("MERGING %d cards merging longer than %s; oldest %s merging %s, %s; run: %s",
		n, limit, oldest, age.Truncate(time.Second), why, cmd)}}
}
