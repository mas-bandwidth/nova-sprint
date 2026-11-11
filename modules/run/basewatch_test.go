package run

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// checksForge answers Checks with the rollup set for each sha (or err) and
// FailedLines with one line per url; it counts both.
type checksForge struct {
	forge.Forge
	mu      sync.Mutex
	rollups map[string]forge.CheckRollup
	err     error
	checks  int
	logs    []string
}

func (f *checksForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]forge.CheckRollup{}
	for _, s := range shas {
		out[s] = f.rollups[s]
	}
	return out, nil
}

func (f *checksForge) FailedLines(_ context.Context, _, url string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, url)
	return []string{"FAIL at " + url, "second line", "third line", "fourth line"}, nil
}

func (f *checksForge) set(sha string, r forge.CheckRollup, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollups[sha], f.err = r, err
}

func (f *checksForge) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks, len(f.logs)
}

// watchSnap is three github streams on o/r main (each with a live card),
// one with no forge, and one github stream on o/q main with only a landed
// card.
func watchSnap() store.Snapshot {
	return store.Snapshot{
		Streams: map[string]store.Stream{
			"s1": {Name: "s1", Repo: "o/r", Base: "main", Forge: "github"},
			"s2": {Name: "s2", Repo: "o/r", Base: "main", Forge: "github"},
			"s3": {Name: "s3", Repo: "o/r", Base: "main", Forge: "github"},
			"hand": {Name: "hand", Repo: "o/r", Base: "main"},
			"done": {Name: "done", Repo: "o/q", Base: "main", Forge: "github"},
		},
		Cards: map[string]store.Card{
			"p1": {ID: "p1", Kind: "producer", Stream: "s1", State: "merging"},
			"p2": {ID: "p2", Kind: "producer", Stream: "s2", State: "waiting"},
			"p3": {ID: "p3", Kind: "producer", Stream: "s3", State: "review"},
			"h1": {ID: "h1", Kind: "producer", Stream: "hand", State: "waiting"},
			"d1": {ID: "d1", Kind: "producer", Stream: "done", State: "landed"},
		},
		Judgements: map[string]store.Judgement{},
	}
}

func watchHeads(tip string) map[string]string {
	return map[string]string{"s1": tip, "s2": tip, "s3": tip, "hand": tip, "done": strings.Repeat("e", 40)}
}

// TestBaseWatchReadsEachBaseOncePerTip: one checks read per repository and
// base however many streams share it, none for a base with no live card or
// a stream with no forge; a completed tip (red or green) is never read
// again, its logs read once each for at most three distinct failing jobs;
// running checks and a rate-limited forge are asked again on the landing's
// cadence and are no fact; the red stretch keeps its first red tip; a green
// tip ends it; the coordinator's answer reads the tip once more.
func TestBaseWatchReadsEachBaseOncePerTip(t *testing.T) {
	t.Parallel()
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{}}
	var faults []string
	w := &BaseWatch{Forge: f, Fault: func(what string, err error) { faults = append(faults, what+": "+err.Error()) }}
	snap := watchSnap()
	now := time.Unix(1000, 0).UTC()
	step := func(tip string, d time.Duration) map[string]steps.BaseCI {
		now = now.Add(d)
		w.Watch(context.Background(), snap, watchHeads(tip), now)
		w.Wait()
		return w.Read()
	}

	f.set(a, forge.CheckRollup{SHA: a, State: forge.CheckRed, Waiting: true, Failing: []string{"unit"}, URLs: []string{"https://x/job/1"}}, nil)
	if got := step(a, 0); len(got) != 0 {
		t.Fatalf("a red tip with checks still running is no fact yet: %+v", got)
	}
	step(a, LandPoll/2)
	if n, _ := f.counts(); n != 1 {
		t.Fatalf("asked %d times inside one LandPoll, want 1", n)
	}

	f.set(a, forge.CheckRollup{SHA: a, State: forge.CheckRed, Failing: []string{"unit", "lint", "tsan", "asan"},
		URLs: []string{"https://x/job/1", "https://x/job/1", "https://x/job/2", "https://x/job/3"}}, nil)
	got := step(a, LandPoll)
	r, ok := got["o/r@main"]
	if !ok || !r.Red || r.Tip != a || r.FirstRed != a || !r.Since.Equal(now) || strings.Join(r.Streams, ",") != "s1,s2,s3" {
		t.Fatalf("completed red read %+v (ok %v)", r, ok)
	}
	if want := "unit: FAIL at https://x/job/1 | second line | third line"; len(r.Lines) != 3 || r.Lines[0] != want || !strings.HasPrefix(r.Lines[1], "tsan: ") || !strings.HasPrefix(r.Lines[2], "asan: ") {
		t.Fatalf("lines %q: want three distinct jobs, the first %q", r.Lines, want)
	}
	if _, ok := got["o/q@main"]; ok {
		t.Fatal("a base with no live card is read")
	}
	step(a, time.Hour)
	if n, logs := f.counts(); n != 2 || logs != 3 {
		t.Fatalf("%d checks reads and %d log reads after a completed tip, want 2 and 3", n, logs)
	}

	f.set(b, forge.CheckRollup{}, &forge.FaultError{Kind: forge.FaultRateLimit, Reason: "HTTP 403: API rate limit exceeded"})
	if got := step(b, LandPoll); got["o/r@main"].Tip != a {
		t.Fatalf("a rested read replaced the last fact: %+v", got["o/r@main"])
	}
	f.set(b, forge.CheckRollup{SHA: b, State: forge.CheckRed, Failing: []string{"unit"}, URLs: []string{"https://x/job/9"}}, nil)
	got = step(b, LandPoll)
	if r := got["o/r@main"]; r.Tip != b || r.FirstRed != a || !r.Since.Equal(time.Unix(1000, 0).UTC().Add(LandPoll/2+LandPoll)) {
		t.Fatalf("the red stretch lost its start: %+v", r)
	}

	snap.Judgements["base-red.o/r@main.1"] = store.Judgement{ID: "base-red.o/r@main.1", Kind: "base-red", About: "o/r@main", State: "answered", ClosedAt: now.Add(time.Minute)}
	n0, _ := f.counts()
	step(b, 2*time.Minute)
	step(b, 2*time.Minute)
	if n, _ := f.counts(); n != n0+1 {
		t.Fatalf("an answer read the tip %d times, want once", n-n0)
	}

	f.set(c, forge.CheckRollup{SHA: c, State: forge.CheckGreen}, nil)
	if r := step(c, time.Minute)["o/r@main"]; r.Red || r.Tip != c || r.FirstRed != "" {
		t.Fatalf("green read %+v", r)
	}
	if len(faults) != 0 {
		t.Fatalf("a rest or a red base said as a fault: %q", faults)
	}
}

// TestBaseWatchStopsAskingATipWithNoCI: a tip with no check at all is
// asked every LandPoll only until CIWait, then left (no fact, no more
// calls) until the tip moves.
func TestBaseWatchStopsAskingATipWithNoCI(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{a: {SHA: a, State: forge.CheckAbsent}}}
	w := &BaseWatch{Forge: f}
	now := time.Unix(1000, 0).UTC()
	for range 100 {
		w.Watch(context.Background(), watchSnap(), watchHeads(a), now)
		w.Wait()
		now = now.Add(LandPoll)
	}
	if n, _ := f.counts(); n != int(CIWait/LandPoll) {
		t.Fatalf("asked a tip with no CI %d times, want %d", n, int(CIWait/LandPoll))
	}
	if len(w.Read()) != 0 {
		t.Fatal("no CI is a fact")
	}
}
