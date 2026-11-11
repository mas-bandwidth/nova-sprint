package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// watchStep is one Watch at now+d, waited for.
func watchStep(w *BaseWatch, snap store.Snapshot, tip string, now *time.Time, d time.Duration) {
	*now = now.Add(d)
	w.Watch(context.Background(), snap, watchHeads(tip), *now)
	w.Wait()
}

// TestBaseWatchRereadsAGreenTipTheLandingFoundRed (the cold read of #793):
// a tip that settled green is read again only every CIWait, but a nightly
// or a re-run can fail at the same sha in between, and the landing reads
// the base tip on every red pass. A landing that finds the base failing at
// the tip the watch holds has the next Watch read it again at once: no new
// polling, and the base-red push is not late. The repository is one key
// whether a stream names it o/r or by its clone URL.
func TestBaseWatchRereadsAGreenTipTheLandingFoundRed(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{a: {SHA: a, State: forge.CheckGreen}}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	watchStep(w, snap, a, &now, CIWait/2)
	if n, _ := f.counts(); n != 1 {
		t.Fatalf("a settled green tip read %d times, want 1", n)
	}
	f.set(a, forge.CheckRollup{SHA: a, State: forge.CheckRed, Failing: []string{"nightly"}, URLs: []string{"https://x/job/7"}}, nil)
	w.LandSawRed("o/r", "main", strings.Repeat("b", 40)) // another tip: not the one held
	watchStep(w, snap, a, &now, time.Minute)
	if n, _ := f.counts(); n != 1 {
		t.Fatalf("a landing's red at another tip read the held one: %d reads", n)
	}
	w.LandSawRed("https://github.com/o/r.git", "main", a)
	watchStep(w, snap, a, &now, time.Second)
	if r := w.Read()["o/r@main"]; !r.Red || r.Tip != a {
		t.Fatalf("the landing's red base was not read again: %+v", r)
	}
	if n, _ := f.counts(); n != 2 {
		t.Fatalf("%d reads, want 2", n)
	}
}

// TestEdgesTellTheWatchWhenTheBaseIsRed: a land pass that read the base
// tip red says so to BaseRed, with the stream's repository and base.
func TestEdgesTellTheWatchWhenTheBaseIsRed(t *testing.T) {
	t.Parallel()
	var got []string
	e := &Edges{BaseRed: func(repo, base, tip string) { got = append(got, repo+" "+base+" "+tip) }}
	s := store.Stream{Name: "s", Repo: "o/r", Base: "main", Forge: "github"}
	e.sawBaseRed(s, git.LandResult{Outcome: git.OutcomeRed, BaseTip: "t1"})
	e.sawBaseRed(s, git.LandResult{Outcome: git.OutcomeRed, BaseTip: "t2", BaseFailing: []string{"unit"}})
	if strings.Join(got, ",") != "o/r main t2" {
		t.Fatalf("told %q, want only the red base", got)
	}
}

// TestBaseWatchOpensARedTipStillRunningAfterCIWait (the cold read of
// #793): a tip that has shown a failure while other checks still run (a job
// stuck in the queue, a long nightly) is red once it has waited CIWait, the
// landing's own bound; and a tip still running is polled every CIWait
// past CIWait, not every LandPoll.
func TestBaseWatchOpensARedTipStillRunningAfterCIWait(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{
		a: {SHA: a, State: forge.CheckRed, Waiting: true, Failing: []string{"unit"}, URLs: []string{"https://x/job/1"}},
		b: {SHA: b, State: forge.CheckPending},
	}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	for now.Before(time.Unix(1000, 0).UTC().Add(CIWait - 2*LandPoll)) {
		watchStep(w, snap, a, &now, LandPoll)
		if r, ok := w.Read()["o/r@main"]; ok {
			t.Fatalf("red with checks running for under CIWait is a fact: %+v", r)
		}
	}
	watchStep(w, snap, a, &now, LandPoll)
	if r := w.Read()["o/r@main"]; !r.Red || r.Tip != a {
		t.Fatalf("red with checks running past CIWait is no fact: %+v", r)
	}
	n0, _ := f.counts()
	for range 100 {
		watchStep(w, snap, b, &now, LandPoll)
	}
	want := int(CIWait/LandPoll) + int((100*LandPoll-CIWait)/CIWait) + 1
	if n, _ := f.counts(); n-n0 > want {
		t.Fatalf("a running tip polled %d times, want at most %d", n-n0, want)
	}
}

// TestBaseWatchForgetsABaseItStopsWatching (the cold read of #793): a base
// whose cards all landed leaves the watch with its fact, so when new cards
// arrive after a gap (the base went green meanwhile) no stale red reaches
// the tick before the new tip is read.
func TestBaseWatchForgetsABaseItStopsWatching(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{
		a: {SHA: a, State: forge.CheckRed, Failing: []string{"unit"}, URLs: []string{"https://x/job/1"}},
		b: {SHA: b, State: forge.CheckPending},
	}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	if !w.Read()["o/r@main"].Red {
		t.Fatal("no red fact to begin with")
	}
	gone := watchSnap()
	for _, id := range []string{"p1", "p2", "p3"} {
		c := gone.Cards[id]
		c.State = "landed"
		gone.Cards[id] = c
	}
	watchStep(w, gone, a, &now, time.Minute)
	if _, ok := w.Read()["o/r@main"]; ok {
		t.Fatal("a base no longer watched still has a fact")
	}
	watchStep(w, snap, b, &now, time.Hour)
	if r, ok := w.Read()["o/r@main"]; ok {
		t.Fatalf("a stale fact came back across the gap: %+v", r)
	}
}

// TestBaseWatchAllCancelledOpensNothing: a tip whose checks were all
// cancelled is a gate that did not run, neither red nor green: no fact, and
// read again only at the slow cadence (a re-run may complete it).
func TestBaseWatchAllCancelledOpensNothing(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{a: {SHA: a, State: forge.CheckFault, Failing: []string{"unit", "lint"}, Reason: "gate did not run: unit, lint"}}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	watchStep(w, snap, a, &now, time.Hour)
	if r, ok := w.Read()["o/r@main"]; ok {
		t.Fatalf("an all-cancelled tip is a fact: %+v", r)
	}
	if n, _ := f.counts(); n != 2 {
		t.Fatalf("an all-cancelled tip read %d times in an hour of one Watch, want 2", n)
	}
}

// TestBaseWatchSaysAFailingReadOnce: a read that fails is said once per
// stretch of failures, not on every poll; a good read ends the stretch.
func TestBaseWatchSaysAFailingReadOnce(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{}}
	var faults int
	w := &BaseWatch{Forge: f, Fault: func(string, error) { faults++ }}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	f.set(a, forge.CheckRollup{}, errors.New("gh: connection reset"))
	for range 5 {
		watchStep(w, snap, a, &now, LandPoll)
	}
	if faults != 1 {
		t.Fatalf("a failing read said %d times, want once", faults)
	}
	f.set(a, forge.CheckRollup{SHA: a, State: forge.CheckPending}, nil)
	watchStep(w, snap, a, &now, LandPoll)
	f.set(a, forge.CheckRollup{}, errors.New("gh: connection reset"))
	watchStep(w, snap, a, &now, LandPoll)
	if faults != 2 {
		t.Fatalf("a new stretch of failures said %d times in all, want 2", faults)
	}
}

// TestBaseWatchReadsARedThatCompletesAfterCIWait (the second cold read of
// #793): a tip whose checks still run at CIWait is not settled. It is read
// again every CIWait until every check is done, so a red that completes
// after CIWait is a fact within one CIWait, with no card merging.
func TestBaseWatchReadsARedThatCompletesAfterCIWait(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{a: {SHA: a, State: forge.CheckPending}}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	for range 30 { // fifteen minutes running
		watchStep(w, snap, a, &now, LandPoll)
	}
	f.set(a, forge.CheckRollup{SHA: a, State: forge.CheckRed, Failing: []string{"unit"}, URLs: []string{"https://x/job/1"}}, nil)
	for range int(CIWait / LandPoll) {
		watchStep(w, snap, a, &now, LandPoll)
	}
	if r := w.Read()["o/r@main"]; !r.Red || r.Tip != a {
		t.Fatalf("a red that completed after CIWait is no fact one CIWait later: %+v", r)
	}
}

// TestBaseWatchRereadsASettledTipSlowly (the second cold read of #793): a
// completed tip is read again every CIWait, so a re-run or a nightly that
// turns a green tip red at the same sha is a fact within one CIWait. A base
// whose tip does not move costs one read per CIWait: six an hour. A red tip
// read again keeps its red stretch and does not read its logs again.
func TestBaseWatchRereadsASettledTipSlowly(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{a: {SHA: a, State: forge.CheckGreen}}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	for range int(time.Hour / LandPoll) {
		watchStep(w, snap, a, &now, LandPoll)
	}
	if n, _ := f.counts(); n != 1+int(time.Hour/CIWait) {
		t.Fatalf("a green tip read %d times in its first hour, want %d", n, 1+int(time.Hour/CIWait))
	}
	f.set(a, forge.CheckRollup{SHA: a, State: forge.CheckRed, Failing: []string{"nightly"}, URLs: []string{"https://x/job/7"}}, nil)
	watchStep(w, snap, a, &now, CIWait)
	r := w.Read()["o/r@main"]
	if !r.Red || r.Tip != a {
		t.Fatalf("a green tip that went red at the same sha is no fact a CIWait later: %+v", r)
	}
	watchStep(w, snap, a, &now, CIWait)
	if again := w.Read()["o/r@main"]; !again.Since.Equal(r.Since) || again.FirstRed != r.FirstRed {
		t.Fatalf("a red tip read again lost its stretch: %+v, was %+v", again, r)
	}
	if _, logs := f.counts(); logs != 1 {
		t.Fatalf("a red tip read again read its log %d times, want once", logs)
	}
}

// TestBaseWatchAnswerReadsAPendingTipOnce (the second cold read of #793):
// after the coordinator's answer, a tip whose reads keep coming back pending
// is read once for the answer and then at the watch's own cadence, not every
// LandPoll for good.
func TestBaseWatchAnswerReadsAPendingTipOnce(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{
		a: {SHA: a, State: forge.CheckRed, Failing: []string{"unit"}, URLs: []string{"https://x/job/1"}},
		b: {SHA: b, State: forge.CheckPending},
	}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	snap.Judgements["base-red.o/r@main.1"] = store.Judgement{ID: "base-red.o/r@main.1", Kind: "base-red", About: "o/r@main", State: "answered", ClosedAt: now.Add(time.Second)}
	n0, _ := f.counts()
	watchStep(w, snap, b, &now, LandPoll)
	for range int(time.Hour / LandPoll) {
		watchStep(w, snap, b, &now, LandPoll)
	}
	want := int(CIWait/LandPoll) + int((time.Hour-CIWait)/CIWait) + 2
	if n, _ := f.counts(); n-n0 > want {
		t.Fatalf("a pending tip after an answer read %d times in an hour, want at most %d", n-n0, want)
	}
}
