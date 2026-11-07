package run

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
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
// often the base tips are read again.
const (
	LandPoll   = 30 * time.Second
	HeadsEvery = 10 * time.Second
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

// land runs LandPass until it is not pending. A hand stream (mode hand, or
// no forge) gets no result: its producers land by hand.
func (e *Edges) land(ctx context.Context, r steps.EdgeRequest, s store.Stream) {
	if s.Mode == "hand" || s.Forge == "" || e.Forge == nil {
		return
	}
	dir := e.repoDir(s.Repo)
	if err := e.Git.Clone(ctx, s.Repo, dir); err != nil {
		e.fault("land "+r.ID+" stream "+r.Stream+": clone", err)
		e.result(steps.InboxItem{Kind: "land", Request: r.ID, Outcome: "fault"})
		return
	}
	req := git.LandRequest{RepoDir: dir, Repo: s.Repo, Stream: r.Stream, Base: s.Base, Mode: s.Mode, Heads: r.Heads}
	for {
		res, err := git.LandPass(ctx, e.Git, e.Forge, req)
		if err == nil && res.Outcome == git.OutcomeCIPending {
			if e.Pause(ctx, LandPoll) != nil {
				return
			}
			continue
		}
		it := landItem(r.ID, res, err)
		if it.Outcome == "fault" {
			// Layer 3 reissues a fault with no move; the cause is said here.
			e.fault("land "+r.ID+" stream "+r.Stream, cmp.Or(err, landFault(res)))
		}
		e.result(it)
		if res.Outcome == git.OutcomeLanded {
			e.Stale()
		}
		return
	}
}

// landFault is a fault outcome's reason as an error.
func landFault(res git.LandResult) error {
	return fmt.Errorf("%s: %s", cmp.Or(string(res.Outcome), "no outcome"), cmp.Or(res.Reason, "no reason given"))
}

// landItem is LandResult as layer 3 reads it (SEAMS 6).
func landItem(id string, res git.LandResult, err error) steps.InboxItem {
	it := steps.InboxItem{Kind: "land", Request: id, Outcome: "fault"}
	if err != nil {
		return it
	}
	switch res.Outcome {
	case git.OutcomeLanded:
		it.Outcome, it.Landed = "landed", res.LandedHeads
	case git.OutcomeCIRed:
		it.Outcome, it.Blamed = "red", res.BlamedHead // the green heads are not on the base: they land again
	case git.OutcomeRung3Conflict, git.OutcomeLedgerFailed:
		it.Outcome = "conflict"
	}
	return it
}

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
	heads, failed := map[string]string{}, map[string]error{}
	for name, s := range streams {
		dir := e.repoDir(s.Repo)
		if err := e.Git.Clone(ctx, s.Repo, dir); err != nil {
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
