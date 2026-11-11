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
// watch forgets what it read. A tip still running, or with no check yet,
// is asked again every LandPoll (the landing's cadence) until CIWait (the
// landing's bound) has passed since it was first asked, then every CIWait
// until every check on it is done; one that showed a failure by CIWait is
// red. A tip whose checks all completed (green, red, or a gate that did not
// run) is asked again every CIWait too, so a re-run or a nightly that turns
// it red at the same sha is seen: a base whose tip does not move costs one
// checks call per CIWait, and a red tip read again keeps its red stretch
// and reads no log again. A rate-limited forge, a failed read and a gate
// fault are no fact: the last completed read stands. The coordinator's
// answer to the open judgement, and a land pass that finds the base red at
// the tip held (LandSawRed), read the tip again at once, once. Reads run
// off the tick; Read is what the next tick passes to the steps
// (Inputs.Bases).
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
	settled bool          // every check on tip is done: asked again only at the slow cadence
	faulted bool          // the last read failed, and was said
	green   bool          // a green read was taken since the watch began
	fact    *steps.BaseCI // the last completed read; nil: none yet

	seen  string          // the tip the heads last showed
	cands []moveSeen      // moves seen and not yet judged own or foreign
	moved *steps.BaseMove // the last foreign move, for one CIWait
}

// moveSeen is a move of a base tip, to to, seen at at.
type moveSeen struct {
	to string
	at time.Time
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
	owns := ownLandings(snap, groups)
	for _, about := range slices.Sorted(maps.Keys(groups)) {
		b := w.bases[about]
		if b == nil {
			b = &baseWatch{}
			w.bases[about] = b
		}
		b.streams = groups[about]
		tip := tips[about]
		b.moves(about, tip, owns[about], now)
		if tip == "" || b.reading {
			continue
		}
		if tip != b.tip {
			b.first, b.settled, b.asked = now, false, time.Time{}
		}
		if answeredSince(snap, about, b.asked) {
			b.asked = time.Time{} // retry: look again now, once
		}
		if now.Before(b.asked.Add(b.every(now))) {
			continue
		}
		b.tip, b.asked, b.reading = tip, now, true
		repo, base, _ := strings.Cut(about, "@")
		w.wg.Go(func() { w.read(ctx, about, repo, base, tip, now) })
	}
}

// moves follows b's tip as the heads show it (no forge read): a move from a
// tip seen before is judged one LandPoll after it was seen, so a landing's
// result has folded into the store first; one to a commit that is none of
// own (the sprint's landings on b's streams) is a foreign move, kept for one
// CIWait after the last one.
func (b *baseWatch) moves(about, tip string, own map[string]bool, now time.Time) {
	if tip != "" && tip != b.seen {
		if b.seen != "" {
			b.cands = append(b.cands, moveSeen{to: tip, at: now})
		}
		b.seen = tip
	}
	kept := b.cands[:0]
	for _, c := range b.cands {
		switch {
		case now.Before(c.at.Add(LandPoll)):
			kept = append(kept, c)
		case !own[c.to]:
			repo, base, _ := strings.Cut(about, "@")
			b.moved = &steps.BaseMove{Repo: repo, Base: base, To: c.to, At: c.at}
		}
	}
	b.cands = kept
	if b.moved != nil && !now.Before(b.moved.At.Add(CIWait)) {
		b.moved = nil
	}
}

// ownLandings is, for each watched base, the commits the sprint's own
// landings put there: each producer's recorded merge (landed on main, or
// merged onto its stream's base) and the head of each landed or merging
// producer, on the streams that land on it.
func ownLandings(snap store.Snapshot, groups map[string][]string) map[string]map[string]bool {
	baseOf := map[string]string{}
	for about, streams := range groups {
		for _, s := range streams {
			baseOf[s] = about
		}
	}
	out := map[string]map[string]bool{}
	for _, c := range snap.Cards {
		about, ok := baseOf[c.Stream]
		if !ok || c.Kind != "producer" || (c.State != "landed" && c.State != "merging") {
			continue
		}
		if out[about] == nil {
			out[about] = map[string]bool{}
		}
		out[about][c.Head] = true
		if merge, ok := store.BaseMerge(c.Reason); ok && merge != "" {
			out[about][merge] = true
		}
		if rest, ok := strings.CutPrefix(c.Reason, store.LandedReason+" at "); ok {
			merge, _, _ := strings.Cut(rest, ":")
			out[about][merge] = true
		}
	}
	return out
}

// Moves is each watched base a foreigner moved within the last CIWait, by
// steps.BaseAbout (Inputs.Moves).
func (w *BaseWatch) Moves() map[string]steps.BaseMove {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]steps.BaseMove{}
	for about, b := range w.bases {
		if b.moved != nil {
			m := *b.moved
			m.Streams = slices.Clone(b.streams)
			out[about] = m
		}
	}
	return out
}

// every is how long after it was last asked b's tip is asked again:
// LandPoll inside CIWait of its first ask while its checks may still be
// starting or running (the landing's cadence), and CIWait once they are all
// done or CIWait has passed (the landing's bound, as the slow cadence: no
// new number).
func (b *baseWatch) every(now time.Time) time.Duration {
	if b.settled || !now.Before(b.first.Add(CIWait)) {
		return CIWait
	}
	return LandPoll
}

// LandSawRed is a land pass that read repo's base red at tip: a tip the
// watch holds (green, say) is read again at the next Watch.
func (w *BaseWatch) LandSawRed(repo, base, tip string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if b := w.bases[steps.BaseAbout(repoKey(repo), base)]; b != nil && b.tip == tip {
		b.asked = time.Time{}
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
// about about after at (the tip was last asked): an answer reads it once.
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
// first failing lines of at most failingJobs failing jobs; a tip read red
// again with the same failing checks keeps the lines it had (each log is
// read once).
func (w *BaseWatch) read(ctx context.Context, about, repo, base, tip string, now time.Time) {
	rollups, err := w.Forge.Checks(ctx, repo, []string{tip})
	var r forge.CheckRollup
	if err == nil {
		r = rollups[tip]
	}
	var waited bool
	var prev steps.BaseCI
	w.mu.Lock()
	if b := w.bases[about]; b != nil {
		// this read is the CIWait/LandPoll-th of tip or later, as the landing counts its passes
		waited = !now.Add(LandPoll).Before(b.first.Add(CIWait))
		if b.fact != nil {
			prev = *b.fact
		}
	}
	w.mu.Unlock()
	red := err == nil && r.State == forge.CheckRed && (!r.Waiting || waited)
	var lines []string
	switch {
	case !red:
	case prev.Red && prev.Tip == tip && slices.Equal(prev.Failing, r.Failing):
		lines = prev.Lines
	default:
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
		b.settled, b.green = true, true
		b.fact = &steps.BaseCI{Repo: repo, Base: base, Tip: tip}
	case red:
		f := steps.BaseCI{Repo: repo, Base: base, Tip: tip, Red: true, Failing: r.Failing, Lines: lines, FirstRed: tip, Since: now, SinceWatch: !b.green}
		if b.fact != nil && b.fact.Red {
			f.FirstRed, f.Since, f.SinceWatch = b.fact.FirstRed, b.fact.Since, b.fact.SinceWatch
		}
		b.settled, b.fact = !r.Waiting, &f // red with checks still running is read on until they are done
	case r.State == forge.CheckFault:
		b.settled = true // a gate that did not run is neither red nor green: the last fact stands
	default: // running, or no check yet: asked again
		b.settled = false
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
