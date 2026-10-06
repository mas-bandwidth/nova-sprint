package sprint_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/hostload"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/internal/sprintdash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The performance counters (docs/SPEC-SPRINT.md, processor-counters; layer 2 of the
// processor) on the twin store: four cards of known waits, each step at a known time on
// the injected clock, and the counters read back from the cards' own stage stamps.

// counterRig is a sprint on the twin (store.Mem) with readers reader-a and reader-b and
// one machine, m1, width 2, up from the rig's start.
type counterRig struct {
	t   *testing.T
	st  *store.Store
	ctx context.Context
	mu  sync.Mutex
	now time.Time
	n   int
}

func newCounterRig(t *testing.T) *counterRig {
	t.Helper()
	m := store.NewMem()
	r := &counterRig{t: t, ctx: context.Background(), now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	r.st = &store.Store{
		B: m, Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); r.n++; return fmt.Sprint(r.n) },
		Sleep: func(time.Duration) {},
	}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, m.RowsAdd(r.ctx, "t-readers", []string{"reader-a", "reader-b"}))
	require.NoError(t, m.SetCoordinator(r.ctx, "coordinator"))
	return r
}

// sec moves the clock on n seconds and is the new time.
func (r *counterRig) sec(n int) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = r.now.Add(time.Duration(n) * time.Second)
	return r.now
}

func (r *counterRig) must(step store.Step) {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, step)
	require.NoError(r.t, err, step.Verb)
	require.Empty(r.t, res.Refused, "%s refused: %v", step.Verb, res.Refused)
}

func (r *counterRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, store.All, nil)
	require.NoError(r.t, err)
	return s
}

// deal runs one tick with the machine running (m1 and the readers beating), which deals
// the one ready card, and stops the machine again so every other step is the test's.
func (r *counterRig) deal(id string) {
	r.t.Helper()
	require.NoError(r.t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	_, err := r.st.Beat(r.ctx, "m1", &zero, hostload.Source{})
	require.NoError(r.t, err)
	_, _, _, err = r.st.SetMachine(r.ctx, true)
	require.NoError(r.t, err)
	_, err = r.st.Tick(r.ctx)
	require.NoError(r.t, err)
	_, _, _, err = r.st.SetMachine(r.ctx, false)
	require.NoError(r.t, err)
	pr := r.snap().Work.Card(id)
	require.Equal(r.t, sprint.Working, pr.Col, "%s dealt", id)
	require.True(r.t, r.snap().Fleet.Card(pr.F("work")).Placed(), "%s has a work card", id)
}

// work takes and finishes the dealt card's work card, take seconds after the deal and
// work seconds after the take.
func (r *counterRig) work(id string, take, work int) {
	r.t.Helper()
	wc := r.snap().Fleet.Card(r.snap().Work.Card(id).F("work"))
	r.sec(take)
	r.must(store.TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Who: wc.Row}))
	wc = r.snap().Fleet.Card(wc.ID)
	r.sec(work)
	r.must(store.FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Head: "abc", Who: wc.Row}))
	require.Equal(r.t, sprint.Review, r.snap().StateOf(id), "%s after finish", id)
}

// read asks the card's read ask seconds after the finish and reads it read seconds
// later with the verdict (a flash card is accepted on one read).
func (r *counterRig) read(id string, ask, read int, verdict string) {
	r.t.Helper()
	r.sec(ask)
	require.NoError(r.t, r.st.BeatReaders(r.ctx)) // the readers beating at the ask
	r.must(store.AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{id}}}))
	r.sec(read)
	n := 0
	for _, rc := range r.snap().Readers.Of(id) {
		if rc.Col == sprint.Asked || rc.Col == sprint.Reading {
			req := sprint.ReadReq{As: rc.F("reader"), Verdict: verdict, Sel: sprint.Sel{IDs: []string{rc.ID}}, Who: rc.F("reader")}
			if verdict == "broken" {
				req.Finding = "counters.go:1: off by one, change the count"
			}
			r.must(store.ReadStep(req))
			n++
		}
	}
	require.Equal(r.t, 1, n, "%s read once", id)
}

// land accepts accept seconds after the read and merges merge seconds after the accept;
// it is the landing time.
func (r *counterRig) land(id, stream string, accept, merge int) time.Time {
	r.t.Helper()
	r.sec(accept)
	r.must(store.AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{id}}, Who: "coordinator"}))
	at := r.sec(merge)
	r.must(store.MergeStep(sprint.MergeReq{Stream: stream, Cards: []string{id}}))
	require.Equal(r.t, sprint.Landed, r.snap().StateOf(id), "%s landed", id)
	return at
}

// TestCountersGiveIPCAndStallReasonsThatSumToWallTime drives four cards through the twin
// store on an injected clock, each waiting for a different reason: a-1 for nothing, a-2
// for its need a-1 and then a rework, b-1 behind sentinel b-gate until its release, and
// c-1 admitted held until its release. The counters read back from the cards' stage
// stamps give each card's wall time split by reason, the parts summing to the whole, and
// IPC as the cards landed per slot-hour of m1's width 2 since it came up, each equal to
// the hand count from the clock. The where record carries them, and the dashboard's row
// is IPC and the top stall.
func TestCountersGiveIPCAndStallReasonsThatSumToWallTime(t *testing.T) {
	t.Parallel()
	r := newCounterRig(t)
	t0 := r.now
	flash := "tier: flash\n\nA card."
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 2}))
	r.must(store.AddStep(sprint.AddReq{Stream: "a", Cards: []sprint.CardAdd{
		{ID: "a-1", Brief: flash},
		{ID: "a-2", Brief: flash, Needs: []string{"a-1"}},
	}}))
	r.must(store.AddStep(sprint.AddReq{Stream: "b", IDs: []string{"b-gate"}, Sentinel: true}))
	r.must(store.AddStep(sprint.AddReq{Stream: "b", Cards: []sprint.CardAdd{{ID: "b-1", Brief: flash}}}))
	r.must(store.AddStep(sprint.AddReq{Stream: "c", Cards: []sprint.CardAdd{{ID: "c-1", Brief: flash}}, Held: true}))

	// a-1: dealt 10 s after the add, taken 2 s later, 100 s of work, read 5 + 20 s,
	// accepted 3 s later, merged 7 s after that
	r.sec(10)
	r.deal("a-1")
	r.work("a-1", 2, 100)
	r.read("a-1", 5, 20, "ok")
	landA1 := r.land("a-1", "a", 3, 7)

	// a-2: ready when a-1 landed; dealt 15 s later, 3 + 50 s, read broken 4 + 10 s,
	// reworked 40 s later (the rework deals the next attempt), taken 6 + 2 s later, 60 s
	// of work, read 5 + 10 s, 2, 8
	r.sec(15)
	r.deal("a-2")
	r.work("a-2", 3, 50)
	r.read("a-2", 4, 10, "broken")
	reworkA2 := r.sec(40)
	r.must(store.ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"a-2"}}, Fix: "off by one", Who: "coordinator"}))
	r.sec(6)
	r.work("a-2", 2, 60)
	r.read("a-2", 5, 10, "ok")
	landA2 := r.land("a-2", "a", 2, 8)

	// b-1: behind b-gate, released 30 s later; dealt 4 s after, 1 + 70 s, 3 + 9 s, 1, 6
	relB := r.sec(30)
	r.must(store.ReleaseStep(sprint.ReleaseReq{IDs: []string{"b-gate"}, Reason: "nothing before it", Coordinator: "coordinator", Who: "coordinator"}))
	r.sec(4)
	r.deal("b-1")
	r.work("b-1", 1, 70)
	r.read("b-1", 3, 9, "ok")
	landB1 := r.land("b-1", "b", 1, 6)

	// c-1: admitted held, released 20 s later; dealt 5 s after, 2 + 40 s, 2 + 8 s, 2, 3
	relC := r.sec(20)
	r.must(store.ReleaseStep(sprint.ReleaseReq{IDs: []string{"c-1"}, Reason: "its turn", Coordinator: "coordinator", Who: "coordinator"}))
	r.sec(5)
	r.deal("c-1")
	r.work("c-1", 2, 40)
	r.read("c-1", 2, 8, "ok")
	landC1 := r.land("c-1", "c", 2, 3)

	now := r.sec(60)
	got := sprint.CycleTimes(r.snap(), now).Counters
	require.NotNil(t, got, "counters with cards landed")

	secs := func(d time.Duration) float64 { return d.Seconds() }
	card := func(id, stream string, landed time.Time, parts map[string]float64) sprint.CardStalls {
		return sprint.CardStalls{ID: id, Stream: stream, WallS: secs(landed.Sub(t0)), Stalls: parts}
	}
	want := []sprint.CardStalls{
		card("a-1", "a", landA1, map[string]float64{sprint.StallSlot: 10 + 2, sprint.StallWork: 100, sprint.StallRead: 5 + 20 + 3, sprint.StallMerge: 7}),
		card("a-2", "a", landA2, map[string]float64{
			sprint.StallNeed:   secs(landA1.Sub(t0)),
			sprint.StallRework: secs(reworkA2.Sub(landA1)), // the first attempt, its read and the rework, to the deal again
			sprint.StallSlot:   6 + 2, sprint.StallWork: 60, sprint.StallRead: 5 + 10 + 2, sprint.StallMerge: 8,
		}),
		card("b-1", "b", landB1, map[string]float64{sprint.StallRelease: secs(relB.Sub(t0)), sprint.StallSlot: 4 + 1, sprint.StallWork: 70, sprint.StallRead: 3 + 9 + 1, sprint.StallMerge: 6}),
		card("c-1", "c", landC1, map[string]float64{sprint.StallRelease: secs(relC.Sub(t0)), sprint.StallSlot: 5 + 2, sprint.StallWork: 40, sprint.StallRead: 2 + 8 + 2, sprint.StallMerge: 3}),
	}
	assert.Equal(t, want, got.Cards)
	for _, c := range got.Cards {
		sum := 0.0
		for _, d := range c.Stalls {
			sum += d
		}
		assert.Equal(t, c.WallS, sum, "%s: the stall parts sum to its wall time", c.ID)
	}

	// IPC, by hand: m1 width 2 up from t0 to now, so 2 * (now - t0) slot-seconds
	slotHours := 2 * secs(now.Sub(t0)) / 3600
	total := func(cs ...sprint.CardStalls) map[string]float64 {
		out := map[string]float64{}
		for _, c := range cs {
			for k, d := range c.Stalls {
				out[k] += d
			}
		}
		return out
	}
	counter := func(landed int, top string, cs ...sprint.CardStalls) sprint.Counter {
		return sprint.Counter{Landed: landed, SlotHours: slotHours, IPC: float64(landed) / slotHours, Stalls: total(cs...), Top: top}
	}
	// the top stall over all: release, 2 * ~10 min behind the two waits, over need's ~3 min
	wantAll := counter(4, sprint.StallRelease, want...)
	assert.InDelta(t, wantAll.IPC, got.All.IPC, 1e-9)
	assert.InDelta(t, wantAll.SlotHours, got.All.SlotHours, 1e-9)
	got.All.IPC, got.All.SlotHours = wantAll.IPC, wantAll.SlotHours
	assert.Equal(t, wantAll, got.All)
	wantStreams := map[string]sprint.Counter{
		"a": counter(2, sprint.StallNeed, want[0], want[1]), // need 147 s over rework 122 s
		"b": counter(1, sprint.StallRelease, want[2]),
		"c": counter(1, sprint.StallRelease, want[3]),
	}
	for k, c := range got.Streams {
		assert.InDelta(t, wantStreams[k].IPC, c.IPC, 1e-9, k)
		c.IPC, c.SlotHours = wantStreams[k].IPC, wantStreams[k].SlotHours
		got.Streams[k] = c
	}
	assert.Equal(t, wantStreams, got.Streams)

	// the where record carries the counters, as where --json prints them in stage_times
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	require.NoError(t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	_, err = r.st.Beat(r.ctx, "m1", &zero, hostload.Source{})
	require.NoError(t, err)
	_, err = r.st.Tick(r.ctx)
	require.NoError(t, err)
	facts, err := r.st.WhereFacts(r.ctx, r.snap().Work.Revision)
	require.NoError(t, err)
	require.NotNil(t, facts.StageTimes.Counters, "the where record's counters")
	assert.Equal(t, sprint.CycleTimes(r.snap(), now).Counters, facts.StageTimes.Counters)

	// the dashboard's row: IPC and the top stall, from where --json's stage_times
	st, err := json.Marshal(facts.StageTimes)
	require.NoError(t, err)
	body := []byte(`{"tables":{"work":{}},"landed":4,"stage_times":` + string(st) + `}`)
	d := &sprintdash.Server{Read: func() ([]byte, error) { return body, nil }, Now: func() time.Time { return now }, Every: time.Second}
	d.Refresh()
	var api map[string]any
	require.NoError(t, json.Unmarshal(d.Snapshot(), &api))
	assert.Equal(t, fmt.Sprintf("IPC %.2f landed per slot-hour; top stall: release", wantAll.IPC), api["counters"])
}
