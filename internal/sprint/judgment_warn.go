package sprint

import (
	"cmp"
	"fmt"
	"strings"
	"time"
)

// --- the waiting-judgment warning ---
//
// The owner, 2026-10-11: "Any card waiting for judgement should alert you via warning
// every second once it has been in this state for 10 seconds or longer." and "These
// must be push notifications." A look of the server's coordinator push (cmd/nova-sprint
// coordinator_push.go, every few seconds) that finds a judgment open JudgmentWarnAfter or
// longer sends one warning on the bus to the coordinator's name, listing every such
// judgment, and again until it is answered: nothing turns it off. It repeats at a bounded rate (v1.2.17: a
// warning every second drowned a seat preparing a cleared sprint, its reads refused
// once ten minutes of them went unacknowledged). The owner, 2026-10-15: fold
// unchanged reminders into one. Additions and removals send at once; unchanged
// sets repeat no more often than once a ReminderEvery (JudgmentWarnDue;
// docs/SPEC-SPRINT.md, "The waiting-judgment warning").

// ReminderEvery bounds repeats of an unchanged waiting-judgment set. A changed
// set is sent immediately; only a successful send starts the interval.
// CoordinatorPush.tla: SameSetNotResentWithin, ChangedSetWarnedImmediately.
const ReminderEvery = 30 * time.Minute

// JudgmentWarnEvery retains the existing name for callers of the warning policy.
const JudgmentWarnEvery = ReminderEvery

// JudgmentWarnDue says the look at now pushes the warning ws: the first warning of a
// nonempty set (warnedAt zero), any change from the last successfully sent ID set,
// or ReminderEvery since that send. Display order, aliases and elapsed text are
// not set changes. The caller keeps the receipt unchanged if sending fails.
func JudgmentWarnDue(ws []JudgmentWarning, warned map[string]bool, warnedAt, now time.Time) bool {
	if warnedAt.IsZero() || len(ws) != len(warned) || now.Sub(warnedAt) >= ReminderEvery {
		return true
	}
	for _, w := range ws {
		if !warned[w.ID] {
			return true
		}
	}
	return false
}

// JudgmentWarnAfter is how long a judgment waits on the coordinator before it is warned
// about by the first eligible look.
const JudgmentWarnAfter = 10 * time.Second

// JudgmentWarning is one judgment the warning names: its alias and id, the cards that
// wait on it, how long it has waited, and the commands that answer it.
type JudgmentWarning struct {
	Alias, ID string
	Cards     []string
	Waited    time.Duration
	Answer    []string
}

// JudgmentWarnings is every open judgment of groups that has waited JudgmentWarnAfter or
// longer at now, in the inbox's order; a quiet one (a review time set with wait) too,
// since the warning cannot be turned off. A judgment's wait starts at the older of its
// oldest note and when the coordinator push first saw its group (firstSeen, by group id: a group
// computed at read time, machine:stopped or stale:<stream>, carries its read time as its
// oldest). firstSeen is kept up to date: a group not seen before is noted at now, and a
// group no longer open is forgotten.
func JudgmentWarnings(groups []Group, now time.Time, firstSeen map[string]time.Time) []JudgmentWarning {
	var out []JudgmentWarning
	open := map[string]bool{}
	for _, g := range groups {
		_, _, state := StateJudgment(g)
		if g.Kind != Judgment || state || StateStatusOwner(g, groups) != "" {
			continue
		}
		open[g.ID] = true
		since, ok := firstSeen[g.ID]
		if !ok {
			since = now
			firstSeen[g.ID] = now
		}
		if !g.Oldest.IsZero() && g.Oldest.Before(since) {
			since = g.Oldest
		}
		waited := now.Sub(since)
		if waited < JudgmentWarnAfter {
			continue
		}
		w := JudgmentWarning{Alias: g.Alias, ID: g.ID, Cards: g.Members, Waited: waited.Truncate(time.Second)}
		for _, c := range g.Commands {
			if len(c.Lines) > 0 {
				w.Answer = append(w.Answer, c.Decision+": "+strings.Join(c.Lines, " && "))
			}
		}
		if len(w.Answer) == 0 {
			w.Answer = []string{"read it: nova-sprint inbox --open " + g.ID}
		}
		out = append(out, w)
	}
	for id := range firstSeen {
		if !open[id] {
			delete(firstSeen, id)
		}
	}
	return out
}

// JudgmentWarningSet is the warning's set: each judgment's alias (else id), in order, the
// one line said again, at most once a JudgmentWarnEvery, while the set does not change.
func JudgmentWarningSet(ws []JudgmentWarning) string {
	ids := make([]string, len(ws))
	for i, w := range ws {
		ids[i] = cmp.Or(w.Alias, w.ID)
	}
	return fmt.Sprintf("WARNING %d judgment(s) waiting on the coordinator %s or longer: %s", len(ws), JudgmentWarnAfter, strings.Join(ids, " "))
}

// JudgmentWarningText is the warning push's text when its set changed: the set's line,
// then each judgment with its cards, how long it has waited, and what answers it.
func JudgmentWarningText(ws []JudgmentWarning) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s; answer each: it repeats at most once every 30 minutes until they are answered, at once when the set changes\n", JudgmentWarningSet(ws))
	for _, w := range ws {
		fmt.Fprintf(&b, "%s %s cards=%s waited=%s\n", cmp.Or(w.Alias, "-"), w.ID, cmp.Or(strings.Join(w.Cards, ","), "-"), w.Waited)
		for _, a := range w.Answer {
			fmt.Fprintf(&b, "  %s\n", a)
		}
	}
	return b.String()
}
