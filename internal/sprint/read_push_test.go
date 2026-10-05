package sprint_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/hostload"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readPushRig is a twin store on a fake clock with injected readers and members.
type readPushRig struct {
	t   *testing.T
	st  *store.Store
	m   *store.Mem
	ctx context.Context
	mu  sync.Mutex
	now time.Time
}

var readPushT0 = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

func newReadPushRig(t *testing.T) *readPushRig {
	t.Helper()
	m := store.NewMem()
	r := &readPushRig{t: t, m: m, ctx: context.Background(), now: readPushT0}
	n := 0
	r.st = &store.Store{
		B:     m,
		Names: sprint.Names{Prefix: "t-"},
		Actor: "coordinator",
		Now: func() time.Time {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.now
		},
		NewID: func() string {
			r.mu.Lock()
			defer r.mu.Unlock()
			n++
			return fmt.Sprint(n)
		},
		Sleep: func(time.Duration) {},
	}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, m.RowsAdd(r.ctx, "t-readers", []string{"reader-m1", "reader-m2"}))
	require.NoError(t, m.SetCoordinator(r.ctx, "coordinator"))
	r.beat()
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 2}))
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m2", Width: 2}))
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	return r
}

func (r *readPushRig) beat() {
	r.t.Helper()
	require.NoError(r.t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	for _, m := range []string{"m1", "m2"} {
		_, err := r.st.Beat(r.ctx, m, &zero, hostload.Source{})
		require.NoError(r.t, err)
	}
}

func (r *readPushRig) tick() {
	r.t.Helper()
	r.mu.Lock()
	r.now = r.now.Add(time.Second)
	r.mu.Unlock()
	r.beat()
	_, err := r.st.Tick(r.ctx)
	require.NoError(r.t, err)
}

func (r *readPushRig) must(step store.Step) store.Result {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, step)
	require.NoError(r.t, err, step.Verb)
	require.Empty(r.t, res.Refused, "%s refused", step.Verb)
	return res
}

func (r *readPushRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, store.All, nil)
	require.NoError(r.t, err)
	return s
}

// TestAFinishAsksAFreeReaderInTheSameTick tests that on the twin store with a fake
// clock, a finished card is asked in the tick of its finish when a reader has room;
// with none free it is asked on the first free one (docs/SPEC-SPRINT.md section 6).
func TestAFinishAsksAFreeReaderInTheSameTick(t *testing.T) {
	t.Parallel()

	t.Run("a finish with a free reader is asked at once", func(t *testing.T) {
		t.Parallel()
		r := newReadPushRig(t)

		// Add card and tick to deal it.
		r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"s1-1"}}))
		r.tick()

		// m1 takes the card.
		r.must(store.TakeStep(sprint.TakeReq{
			As:   "m1",
			Sel:  sprint.Sel{IDs: []string{"s1-1.w1"}},
			Gens: map[string]int{"s1-1.w1": 1},
			Who:  "m1",
		}))

		// Reader reader-m1 and reader-m2 are both up with room.
		// When m1 finishes the card, it should be asked of a free reader at once in the finish step.
		r.must(store.FinishStep(sprint.FinishReq{
			As:   "m1",
			Sel:  sprint.Sel{IDs: []string{"s1-1.w1"}},
			Gens: map[string]int{"s1-1.w1": 1},
			Who:  "m1",
		}))

		snap := r.snap()
		asked := snap.Readers.Column(sprint.Asked)
		require.NotEmpty(t, asked, "a finish with a free reader with room is asked at once, got no asked reads")
		assert.Equal(t, "s1-1", asked[0].F("primary"))
		assert.Contains(t, []string{"reader-m1", "reader-m2"}, asked[0].Row)
		assert.Equal(t, snap.Fleet.Card("s1-1.w1").F("finished"), asked[0].F("asked"))
	})

	t.Run("with none free it is asked on the first free one", func(t *testing.T) {
		t.Parallel()
		r := newReadPushRig(t)

		r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"s1-2"}}))
		r.tick()

		r.must(store.TakeStep(sprint.TakeReq{
			As:   "m1",
			Sel:  sprint.Sel{IDs: []string{"s1-2.w1"}},
			Gens: map[string]int{"s1-2.w1": 1},
			Who:  "m1",
		}))

		// Set both members to drain so readers have no room.
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Drain: true}))
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m2", Drain: true}))

		// Finish with no reader with room: finishes ok, but cannot be asked yet.
		r.must(store.FinishStep(sprint.FinishReq{
			As:   "m1",
			Sel:  sprint.Sel{IDs: []string{"s1-2.w1"}},
			Gens: map[string]int{"s1-2.w1": 1},
			Who:  "m1",
		}))

		snap := r.snap()
		asked := snap.Readers.Column(sprint.Asked)
		require.Empty(t, asked, "no reader has room: should not have asked any reader")

		// Restore width on m1 so reader-m1 has room.
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 2}))

		// On the next tick, the card is asked of the newly free reader.
		r.tick()

		snap = r.snap()
		asked = snap.Readers.Column(sprint.Asked)
		require.NotEmpty(t, asked, "once a reader has room, it is asked in the next tick")
		assert.Equal(t, "s1-2", asked[0].F("primary"))
		assert.Equal(t, "reader-m1", asked[0].Row)
	})
}
