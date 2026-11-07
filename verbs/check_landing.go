package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
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
}

// checkLanding is one stream's landing and its wait.
func (e *Engine) checkLanding(ctx context.Context, r *checkReport, snap store.Snapshot, s store.Stream, ids []string, readable, running bool) {
	what := fmt.Sprintf("landing %s: %d merging (%s)", s.Name, len(ids), firstN(ids, 5))
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

// groupWait is what an unblocked landing waits for, from its group branch
// at origin: the lander's merge when the branch is not there, else the CI
// the lander reads at its tip.
func (e *Engine) groupWait(ctx context.Context, s store.Stream) string {
	group := git.StreamBranch(s.Name)
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return "waits for its repository (" + errText(err) + ")"
	}
	sha, ok, err := e.Origin.Tip(ctx, url, group)
	switch {
	case err != nil:
		return "waits for the lander; " + group + " cannot be read (" + errText(err) + ")"
	case !ok:
		return "waits for run to merge its heads into " + group
	}
	return fmt.Sprintf("waits for CI on %s at %s (run asks every %s)", group, shortSHA(sha), run.LandPoll)
}

// firstN is up to n names, then how many more.
func firstN(names []string, n int) string {
	s := strings.Join(names[:min(len(names), n)], ", ")
	if more := len(names) - n; more > 0 {
		s += fmt.Sprintf(" and %d more", more)
	}
	return s
}
