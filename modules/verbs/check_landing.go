package verbs

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// checkLandings is one line per stream with producers merging: what its
// landing waits for, read from what the store records (the stream's
// settings and hold, the open judgements about it) and the group branch at
// origin. It is red when a person or a stopped piece must act.
func (e *Engine) checkLandings(ctx context.Context, r *checkReport, snap store.Snapshot, heads map[string]string, running bool) {
	merging := map[string][]string{}
	for id, c := range snap.Cards {
		if c.Kind == "producer" && c.State == "merging" {
			merging[c.Stream] = append(merging[c.Stream], id)
		}
	}
	if len(merging) == 0 {
		r.ok("landings: none waiting")
		return
	}
	for _, s := range streamsInOrder(snap) {
		if ids := merging[s.Name]; len(ids) > 0 {
			slices.Sort(ids)
			e.checkLanding(ctx, r, snap, s, ids, heads[s.Name] != "", running)
		}
	}
	e.checkMergeQueue(ctx, r, snap)
}

// checkMergeQueue is CHECK RED while any producer has been merging longer
// than timeouts merging_nag (the run's merge-queue nag, steps.mergingNag;
// Glenn 2026-11-15): how many, the oldest and its age, and the command that
// clears it. Each card's merging since is read from the store's last
// landLookback events of the epoch; one that went merging before them is
// at least as old as the oldest event read, and said so. A history that
// cannot be read is a red line of its own.
func (e *Engine) checkMergeQueue(ctx context.Context, r *checkReport, snap store.Snapshot) {
	limit := time.Duration(policy.DefaultMergingNag) * time.Second
	if _, tm, err := e.rules(ctx, snap); err == nil && tm.MergingNag > 0 {
		limit = time.Duration(tm.MergingNag) * time.Second
	}
	since, floor, err := e.mergingSince(ctx, snap)
	if err != nil {
		r.red(refuse("merge queue: "+errText(err), "nova-sprint check again"))
		return
	}
	now := e.Now()
	n, oldest, age, atLeast := 0, "", time.Duration(0), false
	for _, id := range slices.Sorted(maps.Keys(snap.Cards)) {
		c := snap.Cards[id]
		if c.Kind != "producer" || c.State != "merging" {
			continue
		}
		at, seen := since[id]
		if !seen {
			at = floor
		}
		if a := now.Sub(at); a > limit {
			n++
			if a > age {
				oldest, age, atLeast = id, a, !seen
			}
		}
	}
	if n == 0 {
		return
	}
	how := ""
	if atLeast {
		how = "at least "
	}
	fix := fmt.Sprintf("nova-sprint land %s --head %s --reason \"<why>\"", steps.ShellWord(oldest), snap.Cards[oldest].Head)
	if j := openLand(snap, oldest); j != "" {
		fix = orWord(strings.TrimSpace(snap.Judgements[j].Command), "nova-sprint show "+j)
	}
	r.red(refuse(fmt.Sprintf("merge queue: %d cards merging longer than %s (timeouts merging_nag); oldest %s merging %s%s", n, limit, oldest, how, age.Round(time.Second)), fix))
}

// mergingSince is when each producer last went merging in the store's last
// landLookback events of snap's epoch, and the floor for one not seen go
// (mergingSinceOf).
func (e *Engine) mergingSince(ctx context.Context, snap store.Snapshot) (map[string]time.Time, time.Time, error) {
	var evs []store.Event
	for cursor := max(0, snap.Seq-landLookback); cursor < snap.Seq; {
		batch, err := e.Store.Events(ctx, cursor, store.MaxEvents)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("reading the merging history after seq %d: %w", cursor, err)
		}
		if len(batch) == 0 {
			break
		}
		evs = append(evs, batch...)
		cursor = batch[len(batch)-1].Seq
	}
	since, floor := mergingSinceOf(evs, snap, e.Now())
	return since, floor, nil
}

// mergingSinceOf is, over evs (in seq order, up to snap's seq), when each
// producer last went merging in snap's epoch, and the time of the oldest
// event read (now when none was): the floor for a producer not seen go.
func mergingSinceOf(evs []store.Event, snap store.Snapshot, now time.Time) (map[string]time.Time, time.Time) {
	out, floor := map[string]time.Time{}, now
	for _, ev := range evs {
		if ev.Seq > snap.Seq {
			continue
		}
		if ev.At.Before(floor) {
			floor = ev.At
		}
		if !ev.Accepted || ev.After == nil || ev.Epoch != snap.Epoch {
			continue
		}
		for id, c := range ev.After.Cards {
			if c.Kind != "producer" || c.State != "merging" {
				continue
			}
			if ev.Before != nil {
				if b, ok := ev.Before.Cards[id]; ok && b.State == "merging" {
					continue
				}
			}
			out[id] = ev.At
		}
	}
	return out, floor
}

// checkLanding is one stream's landing and its wait.
func (e *Engine) checkLanding(ctx context.Context, r *checkReport, snap store.Snapshot, s store.Stream, ids []string, readable, running bool) {
	what := fmt.Sprintf("landing %s: %d merging (%s)", s.Name, len(ids), firstN(ids, 5))
	onBase := 0
	for _, id := range ids {
		if _, ok := store.BaseMerge(snap.Cards[id].Reason); ok {
			onBase++
		}
	}
	if onBase > 0 {
		what += fmt.Sprintf(", %d on base,", onBase)
	}
	if j, ok := landingJudgement(snap, s, ids); ok {
		r.red(refuse(fmt.Sprintf("%s waits for judgement %s (%s, open since %s): %s", what, j.ID, j.Kind, j.OpenedAt.UTC().Format("2006-01-02 15:04:05"), clipN(j.Question, 160)),
			orWord(strings.TrimSpace(j.Command), "nova-sprint show "+j.ID)))
		return
	}
	switch {
	case !running:
		r.red(refuse(what+" waits for run, and nothing runs the sprint", runFix))
	case s.Held:
		r.red(refuse(what+" waits for the stream's hold to end", "nova-sprint unhold "+s.Name+" --reason \"<why>\""))
	case s.Mode == "hand" || s.Forge == "":
		r.ok("%s waits for a hand landing (mode %s, forge %s)", what, s.Mode, orWord(s.Forge, "none"))
	case !readable:
		r.ok("%s waits for its repository (the stream's red line above)", what)
	case onBase == len(ids):
		r.ok("%s waits for %s's PR to main and its CI (run asks every %s)", what, s.Base, run.LandPoll)
	default:
		r.ok("%s %s", what, e.groupWait(ctx, s))
	}
}

// landingJudgement is the oldest open judgement about the stream, its base
// branch, or one of its merging producers.
func landingJudgement(snap store.Snapshot, s store.Stream, ids []string) (store.Judgement, bool) {
	var found []store.Judgement
	for _, j := range snap.Judgements {
		if j.State == "open" && (j.About == s.Name || j.About == s.Base || slices.Contains(ids, j.About)) {
			found = append(found, j)
		}
	}
	if len(found) == 0 {
		return store.Judgement{}, false
	}
	slices.SortFunc(found, func(a, b store.Judgement) int {
		if c := a.OpenedAt.Compare(b.OpenedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return found[0], true
}

// groupWait is what an unblocked landing waits for: run's landing, which
// opens the PR (rung 1 the card's own branch, rung 2 the group branch) and
// reads its CI, and opens a judgement for anything that is not green.
func (e *Engine) groupWait(_ context.Context, s store.Stream) string {
	return fmt.Sprintf("waits for its landing PR's CI (rung 1 the card's branch, rung 2 %s; run asks every %s)", git.GroupBranch(s.Name), run.LandPoll)
}

// firstN is up to n names, then how many more.
func firstN(names []string, n int) string {
	s := strings.Join(names[:min(len(names), n)], ", ")
	if more := len(names) - n; more > 0 {
		s += fmt.Sprintf(" and %d more", more)
	}
	return s
}
