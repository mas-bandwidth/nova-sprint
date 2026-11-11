package run

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
		case "land", "promote":
			s := snap.Streams[r.Stream]
			// the landing's merge commits are the store's identity (round 24), never a literal
			go e.land(git.WithIdentity(ctx, snap.Identity.Name, snap.Identity.Email), r, s)
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

// stop is one bus message to the row; sent, it is acknowledged. A failed
// send is an edge fault, not retried, and its result is outcome fault so the
// request leaves flight: kept, it would hold the id, and a later stop of the
// same card would never be sent (Ticker.dispatch).
func (e *Edges) stop(ctx context.Context, r steps.EdgeRequest) {
	if _, err := e.Bus.Send(ctx, r.Row, "STOP", "STOP "+r.Consumer); err != nil {
		e.fault("stop "+r.ID+" to "+r.Row, err)
		e.result(steps.InboxItem{Kind: "stop", Request: r.ID, Outcome: "fault", Reason: err.Error()})
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

// land runs LandPass (PromotePass for a promote request) until it is not
// pending (LANDING.md). A stream that lands by hand (mode hand, or no
// forge) is answered at once with outcome hand: its cards are the
// coordinator's judgement. A landing's result says whether the base is
// main (Main), so layer 3 records landed only on main; a promotion's
// landed merges are on main by definition (LANDING.md, "Landed means on
// main").
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
	pass := func() (git.LandResult, error) {
		return git.LandPass(ctx, e.Git, e.Forge, git.LandRequest{RepoDir: dir, Repo: s.Repo, Stream: r.Stream, Base: s.Base, Heads: r.Heads, Branches: r.Branches})
	}
	if r.Kind == "promote" {
		pass = func() (git.LandResult, error) {
			return git.PromotePass(ctx, e.Git, e.Forge, git.PromoteRequest{RepoDir: dir, Repo: s.Repo, Stream: r.Stream, Base: s.Base, Merges: r.Merges})
		}
	}
	wait := time.Duration(r.Wait) * time.Second
	if wait <= 0 {
		wait = CIWait
	}
	absent, rested := time.Duration(0), false
	for {
		res, err := e.pass(ctx, dir, pass)
		if again, waited := askAgain_internal(res, err, absent, wait); again {
			if res.Outcome != git.OutcomeRested {
				rested = false
			} else if !rested { // said when the wait starts, not every pass
				rested = true
				e.fault("land "+r.ID+" stream "+r.Stream+" waits", errors.New(res.Reason))
			}
			absent = waited
			if e.Pause(ctx, LandPoll) != nil {
				return
			}
			continue
		}
		it := landItem(r.ID, res, err, wait)
		it.Main = r.Kind == "promote" || it.Landed != nil && e.isMain(ctx, dir, s.Base)
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
// how long the gated sha has had no check at all. Pending CI, a base that
// moved after the gate and a forge GitHub rate-limited are asked again (a
// resting forge refuses its calls without making them, so those passes
// spend nothing); an absent CI only until wait, after which the pass goes
// to layer 3 as no-ci. A rested pass read no CI, so it keeps absent as it
// was: a rest neither restarts the no-CI clock nor counts on it.
func askAgain_internal(res git.LandResult, err error, absent, wait time.Duration) (bool, time.Duration) {
	if err != nil {
		return false, 0
	}
	switch res.Outcome {
	case git.OutcomeRested:
		return true, absent
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

// landFacts is a pass's facts as the judgement shows them: on a red base,
// first that the base itself is red at its tip and on which checks, and
// which checks the card's head is red on, so the coordinator weighs the two
// together; then the rung, the base and its tip, the group tip, the gated
// sha, the PR, the failing checks with the first one's URL and its first
// failing lines, the conflicting files, and the pass's own reason.
func landFacts(res git.LandResult) string {
	var parts []string
	add := func(label, v string) {
		if v != "" {
			parts = append(parts, label+" "+v)
		}
	}
	if len(res.BaseFailing) > 0 {
		add("the base itself is red at its tip "+res.BaseTip+" on:", strings.Join(res.BaseFailing, ", "))
		add("the card's head is red on:", strings.Join(res.FailingCI, ", "))
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
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return dir, nil // there: no lock, so a heads read never waits behind a landing
	}
	lock, err := git.LockClone(ctx, dir) // Clone looks for .git again under it
	if err != nil {
		return "", err
	}
	return dir, errors.Join(e.Git.Clone(ctx, url, dir), lock.Close())
}

// pass is one LandPass or PromotePass under the clone's lock
// (git.LockClone): streams on one repository share the clone, one land
// goroutine each, and a pass's fetch, MergeGroup (checkout and merge in the
// work tree) and push must not meet another stream's, nor nova-sprint
// check's fetch; two streams on one base promote one after the other, so
// the second reads main after the first merged. The pass's own fetch and
// push do not lock again; the Pause between passes holds nothing.
func (e *Edges) pass(ctx context.Context, dir string, pass func() (git.LandResult, error)) (git.LandResult, error) {
	lock, err := git.LockClone(ctx, dir)
	if err != nil {
		return git.LandResult{}, err
	}
	res, err := pass()
	return res, errors.Join(err, lock.Close())
}

// isMain is whether base is origin's default branch; a read that fails is
// no (the card is then recorded on base, and its promotion finds it on
// main), said as an edge fault.
func (e *Edges) isMain(ctx context.Context, dir, base string) bool {
	main, _, err := e.Git.Main(ctx, dir)
	e.fault("main of "+dir, err)
	return err == nil && main == base
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

// readHeads reads each stream's base tip: one ls-remote per repository and
// base, its answer every stream's that shares them (defect D6: nine streams
// on one base were read nine times, and the one read that timed out lost
// its head for the pass while the others kept theirs). A read that fails
// keeps the head as last read (SEAMS: Inputs.Heads is the tip "as the edge
// last read it"), and the failure is said per stream; a base not on origin
// is a definite answer, and the head goes.
func (e *Edges) readHeads(ctx context.Context, streams map[string]store.Stream) {
	heads, failed, read := map[string]string{}, map[string]error{}, map[string]headRead{}
	e.mu.Lock()
	last := e.heads
	e.mu.Unlock()
	for name, s := range streams {
		url, err := git.CloneURL(s.Repo, s.Forge)
		if err != nil {
			failed[name] = fmt.Errorf("clone: %w", err)
			continue
		}
		key := url + "\x00" + s.Base
		r, ok := read[key]
		if !ok {
			r = e.readHead(ctx, s)
			read[key] = r
		}
		switch {
		case r.err != nil && r.gone:
			failed[name] = r.err
		case r.err != nil:
			failed[name] = r.err
			if sha, kept := last[name]; kept {
				heads[name] = sha
			}
		default:
			heads[name] = r.sha
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

// headRead is one base tip as read at origin: the sha, or why the read
// failed; gone says the failure is the base not being on origin.
type headRead struct {
	sha  string
	err  error
	gone bool
}

func (e *Edges) readHead(ctx context.Context, s store.Stream) headRead {
	dir, err := e.clone(ctx, s)
	if err != nil {
		return headRead{err: fmt.Errorf("clone: %w", err)}
	}
	sha, ok, err := e.Git.LsRemote(ctx, dir, "refs/heads/"+s.Base)
	switch {
	case err != nil:
		return headRead{err: fmt.Errorf("ls-remote %s: %w", s.Base, err)}
	case !ok:
		return headRead{err: fmt.Errorf("base branch %s is not on origin", s.Base), gone: true}
	}
	return headRead{sha: sha}
}
