package run

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// BaseWatch reads the CI of each base tip the sprint lands on, so a red
// base is pushed to the coordinator (steps.baseRed, the base-red
// judgement) instead of being met card by card. One read per repository
// and base (the repository by its owner/repo, however a stream names it),
// however many streams share it, and only while those streams have live
// cards; a stream with no forge is not watched, and a base that leaves the
// watch forgets what it read. A tip whose checks completed (green, or red
// with none still running) is read once: the answer is kept by tip. A tip
// still running, or with no check yet, is asked again every LandPoll (the
// landing's cadence) until CIWait (the landing's bound) has passed since it
// was first asked; one that showed a failure by then is red. A
// rate-limited forge, a failed read and a gate fault are no fact: the last
// completed read stands. The coordinator's answer to the open judgement,
// and a land pass that finds the base red at the tip held (LandSawRed: a
// nightly or a re-run failing at a sha that was green), read the tip again
// once. Reads run off the tick; Read is what the next tick passes to the
// steps (Inputs.Bases).
type BaseWatch struct {
	Forge forge.Forge // nil: nothing is read
	Fault func(what string, err error)

	mu    sync.Mutex
	bases map[string]*baseWatch
	wg    sync.WaitGroup
}

// baseWatch is one repository and base: the tip asked about, and the last
// completed read.
type baseWatch struct {
	streams []string
	tip     string        // the tip last asked about
	asked   time.Time     // when it was asked
	first   time.Time     // when tip was first asked
	reading bool          // a read is in flight
	settled bool          // tip is answered for good: completed, or CIWait passed
	faulted bool          // the last read failed, and was said
	green   bool          // a green read was taken since the watch began
	readAt  time.Time     // when the last completed read was taken
	fact    *steps.BaseCI // the last completed read; nil: none yet
}

// liveStates are the producer states a red base holds back.
var liveStates = []string{"waiting", "ready", "working", "review", "merging"}

// failingJobs is how many distinct failing jobs' logs a red read reads.
const failingJobs = 3

// BaseAboutOf is stream s's base as the base-red judgement names it:
// <owner>/<repo>@<base>, however s names its repository.
func BaseAboutOf(s store.Stream) string { return steps.BaseAbout(repoKey(s.Repo), s.Base) }

// repoKey is repo's owner/repo when it names one on github.com (a clone
// URL, or owner/repo itself), else repo as it is.
func repoKey(repo string) string {
	if p, err := forge.RepoPath(repo); err == nil {
		return p
	}
	return repo
}

// Watch starts a read of each watched base whose tip is due one; it never
// blocks the tick. now is the tick's clock. A base no longer watched (no
// live cards) is forgotten, so a stale read never comes back after a gap.
func (w *BaseWatch) Watch(ctx context.Context, snap store.Snapshot, heads map[string]string, now time.Time) {
	if w == nil || w.Forge == nil {
		return
	}
	groups, tips := watchedBases(snap, heads)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.bases == nil {
		w.bases = map[string]*baseWatch{}
	}
	for about := range w.bases {
		if _, ok := groups[about]; !ok {
			delete(w.bases, about)
		}
	}
	for _, about := range slices.Sorted(maps.Keys(groups)) {
		b := w.bases[about]
		if b == nil {
			b = &baseWatch{}
			w.bases[about] = b
		}
		b.streams = groups[about]
		tip := tips[about]
		if tip == "" || b.reading {
			continue
		}
		if b.fact != nil && answeredSince(snap, about, b.readAt) {
			b.settled = false // retry: look again
		}
		if tip == b.tip && (b.settled || now.Before(b.asked.Add(LandPoll))) {
			continue
		}
		if tip != b.tip {
			b.first, b.settled = now, false
		}
		b.tip, b.asked, b.reading = tip, now, true
		repo, base, _ := strings.Cut(about, "@")
		w.wg.Go(func() { w.read(ctx, about, repo, base, tip, now) })
	}
}

// LandSawRed is a land pass that read repo's base red at tip: a tip the
// watch holds as settled (green, say) is read again at the next Watch.
func (w *BaseWatch) LandSawRed(repo, base, tip string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if b := w.bases[steps.BaseAbout(repoKey(repo), base)]; b != nil && b.tip == tip {
		b.settled, b.asked = false, time.Time{}
	}
}

// watchedBases is each github stream's repository and base with live
// producers on it: the streams that land there and its tip.
func watchedBases(snap store.Snapshot, heads map[string]string) (map[string][]string, map[string]string) {
	live := map[string]bool{}
	for _, c := range snap.Cards {
		if c.Kind == "producer" && slices.Contains(liveStates, c.State) {
			live[c.Stream] = true
		}
	}
	groups, tips := map[string][]string{}, map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(snap.Streams)) {
		s := snap.Streams[name]
		if s.Forge != "github" || !live[name] {
			continue
		}
		about := BaseAboutOf(s)
		groups[about] = append(groups[about], name)
		if tips[about] == "" {
			tips[about] = heads[name]
		}
	}
	return groups, tips
}

// answeredSince is whether the coordinator answered a base-red judgement
// about about after at.
func answeredSince(snap store.Snapshot, about string, at time.Time) bool {
	for _, j := range snap.Judgements {
		if j.Kind == "base-red" && j.About == about && j.State == "answered" && j.ClosedAt.After(at) {
			return true
		}
	}
	return false
}

// read is one checks read of tip, and for a red tip whose checks all
// completed (or that has waited CIWait since it was first asked), the
// first failing lines of at most failingJobs failing jobs (each log once:
// a completed tip is never read again).
func (w *BaseWatch) read(ctx context.Context, about, repo, base, tip string, now time.Time) {
	rollups, err := w.Forge.Checks(ctx, repo, []string{tip})
	var r forge.CheckRollup
	if err == nil {
		r = rollups[tip]
	}
	w.mu.Lock()
	// this read is the CIWait/LandPoll-th of tip or later, as the landing counts its passes
	waited := w.bases[about] != nil && !now.Add(LandPoll).Before(w.bases[about].first.Add(CIWait))
	w.mu.Unlock()
	red := err == nil && r.State == forge.CheckRed && (!r.Waiting || waited)
	var lines []string
	if red {
		lines = w.failingLines(ctx, repo, r)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.bases[about]
	if b == nil || b.tip != tip { // forgotten, or moved on, while the read ran
		return
	}
	b.reading = false
	var fe *forge.FaultError
	switch {
	case errors.As(err, &fe) && fe.Kind == forge.FaultRateLimit: // forge_rested: no fact, asked again
	case err != nil:
		if !b.faulted && w.Fault != nil { // said once per stretch of failed reads
			w.Fault("base checks "+about+" at "+tip, err)
		}
		b.faulted = true
	case r.State == forge.CheckGreen:
		b.settled, b.readAt, b.green = true, now, true
		b.fact = &steps.BaseCI{Repo: repo, Base: base, Tip: tip}
	case red:
		f := steps.BaseCI{Repo: repo, Base: base, Tip: tip, Red: true, Failing: r.Failing, Lines: lines, FirstRed: tip, Since: now, SinceWatch: !b.green}
		if b.fact != nil && b.fact.Red {
			f.FirstRed, f.Since, f.SinceWatch = b.fact.FirstRed, b.fact.Since, b.fact.SinceWatch
		}
		b.settled, b.readAt, b.fact = true, now, &f
	case r.State == forge.CheckFault:
		b.settled = true // a gate that did not run is neither red nor green, and stays so
	default: // running, or no check yet: asked again until CIWait
		b.settled = waited
	}
	if err == nil {
		b.faulted = false
	}
}

// failingLines is "<check>: <first failing lines>" for the first
// failingJobs distinct failing jobs of r; a log that cannot be read is said.
func (w *BaseWatch) failingLines(ctx context.Context, repo string, r forge.CheckRollup) []string {
	var out, seen []string
	for i, url := range r.URLs {
		if len(out) == failingJobs || i >= len(r.Failing) {
			break
		}
		if url == "" || slices.Contains(seen, url) {
			continue
		}
		seen = append(seen, url)
		lines, err := w.Forge.FailedLines(ctx, repo, url)
		if err != nil {
			lines = []string{"(its log was not read: " + edge.RedactN(err.Error(), edge.FailureMax) + ")"}
		}
		out = append(out, r.Failing[i]+": "+clipLine(strings.Join(lines[:min(len(lines), 3)], " | ")))
	}
	return out
}

// clipLine is s cut to 240 bytes on a rune boundary.
func clipLine(s string) string {
	if len(s) <= 240 {
		return s
	}
	s = s[:240]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "..."
}

// Read is each watched base's last completed read, by steps.BaseAbout.
func (w *BaseWatch) Read() map[string]steps.BaseCI {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]steps.BaseCI{}
	for about, b := range w.bases {
		if b.fact != nil {
			f := *b.fact
			f.Streams = slices.Clone(b.streams)
			out[about] = f
		}
	}
	return out
}

// Wait waits for the reads in flight (tests; run never waits).
func (w *BaseWatch) Wait() { w.wg.Wait() }
