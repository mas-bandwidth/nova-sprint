package sprint

import (
	"fmt"
	"strings"
)

// The bound is not the brief's when the lane, not the brief, is broken (docs/SPEC-SPRINT.md,
// the bounds): four attempts in a row, each ended without a worker report, raise "the lane
// is broken, not the brief" instead of the brief's judgment, naming the member or friend
// and the fault each attempt ended on, and offering hold <member>, rework --tier <other
// tier>, and drop.

// LaneBrokenBound is how many attempts, each ended without a worker report, in a row
// make a broken lane: the lane is broken, not the brief (docs/SPEC-SPRINT.md, the bounds).
const LaneBrokenBound = 4

// laneBroken is the lane-broken bound: the member or friend whose lane is broken, the
// faults its attempts ended on, and the attempts' ran record, said in the judgment text.
type laneBroken struct {
	ID          string
	Since       int
	Lane        string
	Faults      []string
	RanAttempts string
}

// String is the lane-broken bound said in one line.
func (b laneBroken) String() string {
	line := fmt.Sprintf("%s: the lane is broken, not the brief: %d attempts ended without a worker report (%s) on %s",
		b.ID, b.Since, strings.Join(b.Faults, ", "), b.Lane)
	if b.RanAttempts != "" {
		line += "; attempts: " + b.RanAttempts
	}
	return line
}

// Remedy is the lane-broken bound's offered actions: hold <member>, rework --tier
// <other tier>, and drop, never the brief bound's brief/drop.
func (b laneBroken) Remedy() string {
	return "hold " + b.Lane + ", rework --tier <other tier>, or drop " + b.ID + " --reason '<why>'"
}

// Why is the lane-broken bound said with its remedy, as a rework's refusal says it:
// the line, and hold <member>, rework --tier <other tier>, and drop.
func (b laneBroken) Why() string { return b.String() + "; " + b.Remedy() }

// AtLaneBroken is whether the primary's last n attempts each ended without a worker
// report (an idle reap, a harness fault, a lane refused, a push refused, a friend lane
// that never started): the lane is broken, not the brief (docs/SPEC-SPRINT.md, the
// bounds). It names the member or friend (the card's row) and the faults it ended on,
// and the offered actions are hold <member>, rework --tier <other tier>, and drop. An
// attempt with no record counts as having run, so a lane is never broken on unknown
// attempts.
func AtLaneBroken(s *Snapshot, c *Card, n int) (laneBroken, bool) {
	attempt := c.Int("attempt")
	if attempt == 0 || n <= 0 {
		return laneBroken{}, false
	}
	if attempt < n {
		return laneBroken{}, false // fewer attempts than the bound: the lane is not yet broken
	}
	runs := ranAttempts(c)
	faults := []string{}
	for i := attempt; i > attempt-n; i-- {
		r, ok := runs[i]
		if !ok || r.ran {
			return laneBroken{}, false
		}
		if r.fault != "" && !containsStr(faults, r.fault) {
			faults = append(faults, r.fault)
		}
	}
	if len(faults) == 0 {
		return laneBroken{}, false
	}
	lane := CardHolder(s, c)
	if lane == "" {
		lane = c.Row
	}
	var ranLines []string
	for i := attempt - n + 1; i <= attempt; i++ {
		r, ok := runs[i]
		if !ok {
			continue
		}
		word := "yes"
		if !r.ran {
			word = "no"
			if r.fault != "" {
				word += ": " + r.fault
			}
		}
		ranLines = append(ranLines, fmt.Sprintf("attempt %d: %s", i, word))
	}
	return laneBroken{ID: c.ID, Since: n, Lane: lane, Faults: faults, RanAttempts: strings.Join(ranLines, "; ")}, true
}

// containsStr is whether ss contains s.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
