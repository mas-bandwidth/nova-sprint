package main

import (
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// queuedTickNote is the NOTE a verb prints beside its OK when it queued
// work-table changes behind a failing tick (v17-queued-step-names-a-failing-tick;
// the 2026-10-17 incident: from 14:02 every tick drain was refused, while add
// acknowledged new cards that were only queued and never admitted, and nothing on
// add's output said the queue was not draining). It is "" unless the machine is
// RUNNING, the step queued work-table changes, and the last tick failed: then it
// names how many ticks failed and the error, and what to run.
func queuedTickNote(m store.Machine, hb store.Heartbeat, queued bool) string {
	if !m.Running() || !queued || hb.Failures <= 0 || hb.Error == "" {
		return ""
	}
	last := fmt.Sprintf("the last %d ticks failed", hb.Failures)
	if hb.Failures == 1 {
		last = "the last tick failed"
	}
	return fmt.Sprintf("queued: the work-table changes wait for the next tick's drain, and %s (%s): nothing queued is applied until a tick drains; run: nova-sprint check", last, oneline.Cap(hb.Error, 300))
}
