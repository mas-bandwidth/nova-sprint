package run

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
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

// Edges runs layer 3's edge requests off the tick (SEAMS 6): each request
// on a goroutine of its own, its result kept for the next tick's inbox. It
// also keeps each stream's base tip (Inputs.Heads), read at start, after
// each landing and every HeadsEvery.
type Edges struct {
	Git      Git
	Forge    forge.Forge // nil: no stream lands through a forge
	Bus      bus.Bus
	ReposDir string
	Pause    func(context.Context, time.Duration) error // between asks of a pending CI
	// Fault is told every edge failure, with its cause (the run log's EDGE
	// FAULT line); nil drops nothing that layer 3 reads, only the line.
	Fault func(what string, err error)

	mu      sync.Mutex
	results []steps.InboxItem
	heads   map[string]string
	headErr map[string]string // the last heads failure said, per stream
	n       int
	stale   chan struct{}
}

// LandPoll is the pause before a pending CI is asked again; HeadsEvery how
// often the base tips are read again. CIWait bounds the wait for the first
// check on a gated sha when the request names no wait of its own (policy's
// landing(stream).ci_wait): GitHub queues a workflow's jobs as checks within
// seconds of a push, so a sha with no check at all after this long has no
// CI coming, and the landing is a judgement instead of a wait for ever. 10
// minutes is a guess, not measured.
const (
	LandPoll   = 30 * time.Second
	HeadsEvery = 10 * time.Second
	CIWait     = 10 * time.Minute
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

// stop is one bus message to the row; sent, it is acknowledged.
func (e *Edges) stop(ctx context.Context, r steps.EdgeRequest) {
	if _, err := e.Bus.Send(ctx, r.Row, "STOP", "STOP "+r.Consumer); err != nil {
		e.fault("stop "+r.ID+" to "+r.Row, err)
		return
	}
	e.result(steps.InboxItem{Kind: "stop", Request: r.ID, Outcome: "ack"})
}

// fault says an edge failure; a nil Fault says nothing.
func (e *Edges) fault(what string, err error) {
	if e.Fault != nil && err != nil {
		e.Fault(what, err)
	}
}

// land runs LandPass until it is not pending (LANDING.md). A stream that
// lands by hand (mode hand, or no forge) is answered at once with outcome
// hand: its cards are the coordinator's judgement.
func (e *Edges) land(ctx context.Context, r steps.EdgeRequest, s store.Stream) {
	if s.Mode == "hand" || s.Forge == "" || e.Forge == nil {
		e.result(steps.InboxItem{Kind: "land", Request: r.ID, Outcome: "hand", Reason: "stream " + r.Stream + " lands by hand (mode " + cmp.Or(s.Mode, "auto") + ", forge " + cmp.Or(s.Forge, "none") + ")"})
		return
	}
	dir, err := e.clone(ctx, s)
	if err != nil {
		e.fault("land "+r.ID+" stream "+r.Stream+": clone", err)
		e.result(steps.InboxItem{Kind: "land", Request: r.ID, Outcome: "fault", Reason: "the clone of " + s.Repo + " failed: " + err.Error()})
		return
	}
	req := git.LandRequest{RepoDir: dir, Repo: s.Repo, Stream: r.Stream, Base: s.Base, Heads: r.Heads, Branches: r.Branches}
	wait := time.Duration(r.Wait) * time.Second
	if wait <= 0 {
		wait = CIWait
	}
	absent := time.Duration(0)
	for {
		res, err := git.LandPass(ctx, e.Git, e.Forge, req)
		if again, waited := askAgain_internal(res, err, absent, wait); again {
			absent = waited
			if e.Pause(ctx, LandPoll) != nil {
				return
			}
			continue
		}
		it := landItem(r.ID, res, err, wait)
		if err != nil {
			e.fault("land "+r.ID+" stream "+r.Stream, err)
		}
		e.result(it)
		if res.Outcome == git.OutcomeLanded {
			e.Stale()
		}
		return
	}
}

// askAgain_internal says whether a pass is asked again after LandPoll, and
// how long the gated sha has had no check at all. Pending CI and a base
// that moved after the gate are asked again; an absent CI only until wait,
// after which the pass goes to layer 3 as no-ci.
func askAgain_internal(res git.LandResult, err error, absent, wait time.Duration) (bool, time.Duration) {
	if err != nil {
		return false, 0
	}
	switch res.Outcome {
	case git.OutcomePending, git.OutcomeBaseMoved:
		return true, 0
	case git.OutcomeAbsent:
		absent += LandPoll
		return absent < wait, absent
	}
	return false, 0
}

// landItem is LandResult as layer 3 reads it (SEAMS 6): landed with the
// heads and the merge sha, or the outcome and its facts for the
// coordinator's judgement (an error is the outcome fault).
func landItem(id string, res git.LandResult, err error, wait time.Duration) steps.InboxItem {
	it := steps.InboxItem{Kind: "land", Request: id, Outcome: string(res.Outcome)}
	switch {
	case err != nil:
		it.Outcome, it.Reason = "fault", err.Error()
		return it
	case res.Outcome == git.OutcomeLanded:
		it.Landed, it.Merge = res.LandedHeads, res.MergeSHA
		return it
	case res.Outcome == git.OutcomeAbsent:
		it.Outcome = "no-ci"
		res.Reason = fmt.Sprintf("no check was reported on %s within %s", res.GatedSHA, wait)
	}
	it.Reason = landFacts(res)
	return it
}

// landFacts is a pass's facts as the judgement shows them: the rung, the
// base and its tip, the group tip, the gated sha, the PR, the failing checks
// with the first one's URL and its first failing lines, the conflicting
// files, and the pass's own reason.
func landFacts(res git.LandResult) string {
	var parts []string
	add := func(label, v string) {
		if v != "" {
			parts = append(parts, label+" "+v)
		}
	}
	add("rung", map[int]string{1: "1", 2: "2"}[res.Rung])
	add("base tip", res.BaseTip)
	add("group tip", res.GroupTip)
	add("gated", res.GatedSHA)
	add("PR", res.PR)
	add("failing", strings.Join(res.FailingCI, ", "))
	add("at", res.CIURL)
	add("first failing lines:", strings.Join(res.FailingLines, " | "))
	add("conflicts:", strings.Join(res.Conflicts, ", "))
	add("reason:", res.Reason)
	return strings.Join(parts, "; ")
}

// clone is the one clone of the stream's repository under ReposDir, at its
// clone address (git.CloneURL: an org/repo with forge github is GitHub's
// https URL), named by that address.
func (e *Edges) clone(ctx context.Context, s store.Stream) (string, error) {
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(url))
	dir := filepath.Join(e.ReposDir, fmt.Sprintf("%x", sum[:8]))
	return dir, e.Git.Clone(ctx, url, dir)
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
	heads, failed := map[string]string{}, map[string]error{}
	for name, s := range streams {
		dir, err := e.clone(ctx, s)
		if err != nil {
			failed[name] = fmt.Errorf("clone: %w", err)
			continue
		}
		sha, ok, err := e.Git.LsRemote(ctx, dir, "refs/heads/"+s.Base)
		switch {
		case err != nil:
			failed[name] = fmt.Errorf("ls-remote %s: %w", s.Base, err)
		case !ok:
			failed[name] = fmt.Errorf("base branch %s is not on origin", s.Base)
		default:
			heads[name] = sha
		}
	}
	e.mu.Lock()
	e.heads = heads
	// A failure is said when it starts or changes, not every pass.
	said := e.headErr
	e.headErr = map[string]string{}
	var tell []string
	for name, err := range failed {
		e.headErr[name] = err.Error()
		if said[name] != err.Error() {
			tell = append(tell, name)
		}
	}
	e.mu.Unlock()
	slices.Sort(tell)
	for _, name := range tell {
		e.fault("heads stream "+name, failed[name])
	}
}
