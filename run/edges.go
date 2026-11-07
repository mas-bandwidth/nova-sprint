package run

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Git is the git edge as run uses it: LandPass's Git and one clone per repo.
type Git interface {
	git.Git
	Clone(ctx context.Context, repo, dir string) error
}

// LandForge is the forge as the landing uses it (edge/forge's Client): the
// group's gate (forge.Forge, through git.LandPass) and a card's own PR.
type LandForge interface {
	forge.Forge
	FindPR(ctx context.Context, repo, head, base string) (forge.PRState, bool, error)
	MergePR(ctx context.Context, repo string, pr int, head string, methods []string) (string, error)
	Behind(ctx context.Context, repo, base, head string) (int, error)
	FailingLines(ctx context.Context, repo, url string, n int) []string
}

// Edges runs layer 3's edge requests off the tick (SEAMS 6): each request
// on a goroutine of its own, its result kept for the next tick's inbox. It
// also keeps each stream's base tip (Inputs.Heads), read at start, after
// each landing and every HeadsEvery.
type Edges struct {
	Git      Git
	Forge    LandForge // nil: no stream lands through a forge
	Bus      bus.Bus
	ReposDir string
	Pause    func(context.Context, time.Duration) error // between asks of a pending CI
	Now      func() time.Time                           // the run's clock, for the CI bounds
	Log      io.Writer                                  // a line per landing outcome; nil: none

	mu      sync.Mutex
	results []steps.InboxItem
	heads   map[string]string
	n       int
	stale   chan struct{}
}

// LandPoll is the pause before a pending CI is asked again; HeadsEvery how
// often the base tips are read again. NoChecksAfter bounds the wait for a
// first check run on the gated sha (a workflow with no trigger for the PR
// never starts one); PendingMax bounds the wait for checks to finish;
// LandTries is how many failed passes in a row (gh or git failing) a
// landing takes before it escalates. Nothing waits silently (INTENT 16).
const (
	LandPoll      = 30 * time.Second
	HeadsEvery    = 10 * time.Second
	NoChecksAfter = 10 * time.Minute
	PendingMax    = 60 * time.Minute
	LandTries     = 3
	FailingLines  = 6
)

// Results drains the results that finished since the last call.
func (e *Edges) Results() []steps.InboxItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.results
	e.results = nil
	return out
}

// Heads is each stream's base tip as last read.
func (e *Edges) Heads() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return maps.Clone(e.heads)
}

// Dispatch starts each request; snap names each stream's repo, base branch,
// mode and forge. It never blocks the tick.
func (e *Edges) Dispatch(ctx context.Context, reqs []steps.EdgeRequest, snap store.Snapshot) {
	for _, r := range reqs {
		switch r.Kind {
		case "land":
			s := snap.Streams[r.Stream]
			go e.land(ctx, r, s)
		case "stop":
			go e.stop(ctx, r)
		}
	}
}

func (e *Edges) result(it steps.InboxItem) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.n++
	it.ID = fmt.Sprintf("%s#%d", it.Request, e.n)
	e.results = append(e.results, it)
}

func (e *Edges) logf(format string, args ...any) {
	if e.Log != nil {
		fmt.Fprintf(e.Log, format+"\n", args...)
	}
}

// stop is one bus message to the row; sent, it is acknowledged.
func (e *Edges) stop(ctx context.Context, r steps.EdgeRequest) {
	if _, err := e.Bus.Send(ctx, r.Row, "STOP", "STOP "+r.Consumer); err == nil {
		e.result(steps.InboxItem{Kind: "stop", Request: r.ID, Outcome: "ack"})
	}
}

// pass is one look at a landing: done (an item to report), or pending at a
// gated sha (with no check run reported yet when noChecks).
type pass struct {
	done     *steps.InboxItem
	sha      string
	noChecks bool
}

// land is one land request on the ladder (INTENT rule 16): one card is rung
// 1 (its own PR from its branch into the base, the PR's checks, merged when
// green: a fast-forward to the exact sha the checks passed when the head
// contains the base, else the repository's merge method); several are rung
// 2 (git.LandPass: merged on the base into the stream's group branch, one PR
// into the base, its checks, the exact gated sha fast-forwarded onto the
// base). It ends in landed or escalate, never in silence: pending checks
// are bounded (NoChecksAfter with no run, PendingMax in all) and failing
// passes by LandTries. A hand stream escalates at once: the coordinator
// lands it.
func (e *Edges) land(ctx context.Context, r steps.EdgeRequest, s store.Stream) {
	repo := forge.Slug(s.Repo)
	if s.Mode == "hand" || s.Forge == "" || e.Forge == nil {
		e.finish(r, escalate(r.ID, fmt.Sprintf("stream %s lands by hand (mode %s, forge %q): merge it into %s, then answer land", r.Stream, s.Mode, s.Forge, s.Base)))
		return
	}
	start, gated, fails := e.Now(), "", 0
	var last error
	for {
		var p pass
		var err error
		if len(r.Cards) == 1 {
			p, err = e.rung1(ctx, repo, s, r)
		} else {
			p, err = e.rung2(ctx, repo, s, r)
		}
		if ctx.Err() != nil {
			return
		}
		switch {
		case err != nil:
			fails, last = fails+1, err
			if fails >= LandTries {
				e.finish(r, escalate(r.ID, fmt.Sprintf("the landing failed %d passes in a row; the last: %s", fails, oneLine(last.Error()))))
				return
			}
		case p.done != nil:
			e.finish(r, *p.done)
			return
		default:
			fails = 0
			if p.sha != gated {
				gated, start = p.sha, e.Now()
			}
			waited := e.Now().Sub(start)
			if p.noChecks && waited >= NoChecksAfter {
				e.finish(r, escalate(r.ID, noChecksFacts(repo, s.Base, p.sha, waited, len(r.Cards) > 1)))
				return
			}
			if waited >= PendingMax {
				e.finish(r, escalate(r.ID, fmt.Sprintf("checks on %s are still pending after %s: look at gh pr checks in %s, then answer retry or land", p.sha, waited.Round(time.Minute), repo)))
				return
			}
		}
		if e.Pause(ctx, LandPoll) != nil {
			return
		}
	}
}

func (e *Edges) finish(r steps.EdgeRequest, it steps.InboxItem) {
	it.Kind, it.Request = "land", r.ID
	e.logf("LAND %s stream=%s cards=%s heads=%s outcome=%s %s", r.ID, r.Stream, strings.Join(r.Cards, ","), strings.Join(r.Heads, ","), it.Outcome, oneLine(it.Facts))
	e.result(it)
	if it.Outcome == "landed" {
		e.Stale()
	}
}

func escalate(id, facts string) steps.InboxItem {
	return steps.InboxItem{Kind: "land", Request: id, Outcome: "escalate", Facts: facts}
}

func landed(id string, heads []string, facts string) *steps.InboxItem {
	return &steps.InboxItem{Kind: "land", Request: id, Outcome: "landed", Landed: heads, Facts: facts}
}

// rung1 is one look at a card's own PR into the base.
func (e *Edges) rung1(ctx context.Context, repo string, s store.Stream, r steps.EdgeRequest) (pass, error) {
	head, branch, base := r.Heads[0], r.Branch, s.Base
	if branch == "" {
		it := escalate(r.ID, "the card has no branch recorded: land it by hand")
		return pass{done: &it}, nil
	}
	pr, ok, err := e.Forge.FindPR(ctx, repo, branch, base)
	if err != nil {
		return pass{}, err
	}
	if ok && pr.State == "MERGED" {
		if pr.HeadSHA == head {
			return pass{done: landed(r.ID, r.Heads, fmt.Sprintf("PR %s merged as %s", pr.URL, pr.Merge))}, nil
		}
		it := escalate(r.ID, fmt.Sprintf("PR %s was merged at %s, but the card finished at %s", pr.URL, pr.HeadSHA, head))
		return pass{done: &it}, nil
	}
	if !ok || pr.State != "OPEN" {
		if _, err := e.Forge.OpenPR(ctx, repo, branch, base, r.Cards[0]+": land on "+base, "nova-sprint landing of "+r.Cards[0]+" at "+head+" (rung 1)."); err != nil {
			return pass{}, fmt.Errorf("gh pr create -R %s --head %s --base %s: %w", repo, branch, base, err)
		}
		return pass{sha: head, noChecks: true}, nil
	}
	if pr.HeadSHA != head {
		it := escalate(r.ID, fmt.Sprintf("PR %s is at %s, but the card finished at %s: the branch moved after finish", pr.URL, pr.HeadSHA, head))
		return pass{done: &it}, nil
	}
	if pr.Mergeable == "CONFLICTING" {
		it := escalate(r.ID, fmt.Sprintf("PR %s conflicts with %s: the head needs %s merged in (git fetch origin %s; git merge origin/%s on %s), answer fix to send it back", pr.URL, base, base, base, base, branch))
		return pass{done: &it}, nil
	}
	rollups, err := e.Forge.Checks(ctx, repo, []string{head})
	if err != nil {
		return pass{}, err
	}
	c := rollups[head]
	switch c.State {
	case forge.CheckGreen:
		behind, err := e.Forge.Behind(ctx, repo, base, head)
		if err != nil {
			return pass{}, err
		}
		sha, how := head, "fast-forwarded"
		if behind == 0 {
			err = e.Forge.Merge(ctx, repo, pr.Number, head, base)
		} else {
			sha, err = e.Forge.MergePR(ctx, repo, pr.Number, head, forge.MergeMethods)
			how = "merged"
		}
		if err != nil {
			it := escalate(r.ID, fmt.Sprintf("PR %s is green but the merge was refused: %s", pr.URL, oneLine(err.Error())))
			return pass{done: &it}, nil
		}
		return pass{done: landed(r.ID, r.Heads, fmt.Sprintf("PR %s %s as %s", pr.URL, how, sha))}, nil
	case forge.CheckRed:
		it := escalate(r.ID, e.redFacts(ctx, repo, "PR "+pr.URL, head, c))
		return pass{done: &it}, nil
	case forge.CheckFault:
		it := escalate(r.ID, fmt.Sprintf("PR %s: %s (%s); rerun it (gh run rerun --failed -R %s), then answer retry", pr.URL, c.Reason, c.URL, repo))
		return pass{done: &it}, nil
	}
	return pass{sha: head, noChecks: c.Runs == 0}, nil
}

// rung2 is one LandPass of the stream's group.
func (e *Edges) rung2(ctx context.Context, repo string, s store.Stream, r steps.EdgeRequest) (pass, error) {
	dir := e.repoDir(s.Repo)
	if err := e.Git.Clone(ctx, s.Repo, dir); err != nil {
		return pass{}, err
	}
	res, err := git.LandPass(ctx, e.Git, e.Forge, git.LandRequest{RepoDir: dir, Repo: repo, Stream: r.Stream, Base: s.Base, Mode: s.Mode, Heads: r.Heads, Branches: cardBranches(r)})
	if err != nil {
		return pass{}, err
	}
	group := git.StreamBranch(r.Stream)
	var facts string
	switch res.Outcome {
	case git.OutcomeLanded:
		return pass{done: landed(r.ID, res.LandedHeads, fmt.Sprintf("group %s fast-forwarded %s to %s", group, s.Base, res.GatedSHA))}, nil
	case git.OutcomeCIPending:
		return pass{sha: res.GatedSHA, noChecks: res.NoChecks}, nil
	case git.OutcomeCIRed:
		facts = e.redFacts(ctx, repo, fmt.Sprintf("group %s (PR #%d)", group, res.PR), res.GatedSHA, forge.CheckRollup{Failing: res.FailingCI, URL: res.CIURL, Reason: res.Reason})
	case git.OutcomeRung3Conflict, git.OutcomeLedgerFailed:
		facts = fmt.Sprintf("the group's heads do not merge on %s at %s: %s; conflicting files %s", s.Base, res.BaseTip, oneLine(res.Reason), strings.Join(res.Conflicts, ","))
	default:
		facts = fmt.Sprintf("group %s at %s on %s at %s: %s %s", group, res.GatedSHA, s.Base, res.BaseTip, res.Outcome, oneLine(res.Reason))
	}
	it := escalate(r.ID, facts)
	return pass{done: &it}, nil
}

// redFacts is a red gate as the coordinator reads it: what is red, the
// failing checks, the first failing lines of the first, and the command
// that shows the rest.
func (e *Edges) redFacts(ctx context.Context, repo, what, sha string, c forge.CheckRollup) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "%s is red at %s: failing %s", what, sha, strings.Join(c.Failing, ", "))
	if lines := e.Forge.FailingLines(ctx, repo, c.URL, FailingLines); len(lines) > 0 {
		fmt.Fprintf(b, "; first failing lines: %s", strings.Join(lines, " | "))
	}
	if _, job, ok := forge.JobOf(c.URL); ok {
		fmt.Fprintf(b, "; the rest: gh run view --job %s -R %s --log-failed", job, repo)
	} else if c.URL != "" {
		fmt.Fprintf(b, "; %s", c.URL)
	}
	return b.String()
}

// noChecksFacts is a gate no CI run ever started on: the workflow fix.
func noChecksFacts(repo, base, sha string, waited time.Duration, group bool) string {
	trigger := "pull_request: {branches: ['" + base + "']}"
	if group {
		trigger += " and push: {branches: ['sprint/**']}"
	}
	return fmt.Sprintf("no check run on %s after %s: %s's CI workflow does not run for this PR; add %s to the workflow's on: in .github/workflows/ on %s (as mas-bandwidth/nova-tools#6238 did), then answer retry; or merge by hand and answer land", sha, waited.Round(time.Minute), repo, trigger, base)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// repoDir is the one clone of repo under ReposDir.
func (e *Edges) repoDir(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(e.ReposDir, fmt.Sprintf("%x", sum[:8]))
}

// Stale asks the heads loop to read again now.
func (e *Edges) Stale() {
	e.mu.Lock()
	ch := e.stale
	e.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// WatchHeads reads every stream's base tip now, then every HeadsEvery or
// when Stale is called, until ctx ends. streams is read each pass (run's
// folded copy).
func (e *Edges) WatchHeads(ctx context.Context, c Clock, streams func() map[string]store.Stream) {
	e.mu.Lock()
	e.stale = make(chan struct{}, 1)
	e.mu.Unlock()
	beat := c.Every(HeadsEvery)
	defer beat.Stop()
	for {
		e.readHeads(ctx, streams())
		select {
		case <-ctx.Done():
			return
		case <-beat.C():
		case <-e.stale:
		}
	}
}

func (e *Edges) readHeads(ctx context.Context, streams map[string]store.Stream) {
	heads := map[string]string{}
	for name, s := range streams {
		dir := e.repoDir(s.Repo)
		if e.Git.Clone(ctx, s.Repo, dir) != nil {
			continue
		}
		if sha, ok, err := e.Git.LsRemote(ctx, dir, "refs/heads/"+s.Base); err == nil && ok {
			heads[name] = sha
		}
	}
	e.mu.Lock()
	e.heads = heads
	e.mu.Unlock()
}

// cardBranches is each landing card's branch at origin, in the request's
// card order (the same name steps gives a deal: sprint/<stream>/<card>).
func cardBranches(r steps.EdgeRequest) []string {
	out := make([]string, 0, len(r.Cards))
	for _, c := range r.Cards {
		out = append(out, "sprint/"+r.Stream+"/"+c)
	}
	return out
}
