package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoLoopsFence drives two tick loops (a and b) in explicit steps against the
// outside world on a fake clock.
type twoLoopsFence struct {
	*Mem
	ctx                          context.Context
	now                          func() time.Time
	a, b                         *Store
	ticks, limit, burst, loseAcq int
	errs                         []string
	active, trailing, retrying   bool
	beforeGen                    uint64
	lastTryAt                    time.Time
}

func (f *twoLoopsFence) stepTick() {
	if f.ticks >= f.limit {
		return
	}
	st := f.a
	if f.ticks%2 == 1 {
		st = f.b
	}
	f.ticks++
	if _, err := st.Tick(f.ctx); err != nil {
		f.errs = append(f.errs, err.Error())
	}
}

func (f *twoLoopsFence) ReadFence(ctx context.Context) (Fence, error) {
	if !f.active {
		return f.Mem.ReadFence(ctx)
	}
	if !f.trailing {
		f.trailing = true
		fe, err := f.Mem.ReadFence(ctx)
		f.beforeGen = fe.Gen
		return fe, err
	}
	f.trailing = false
	now := f.now()
	if f.burst > 0 || (f.retrying && !now.After(f.lastTryAt)) {
		if f.burst > 0 {
			f.burst--
		}
		f.stepTick()
		fe, err := f.Mem.ReadFence(ctx)
		if fe.Gen == f.beforeGen {
			fe.Gen++
		}
		f.retrying, f.lastTryAt = true, now
		return fe, err
	}
	if f.retrying {
		f.retrying = false
		return f.Mem.ReadFence(ctx)
	}
	if f.loseAcq == 0 && f.ticks < f.limit {
		f.stepTick()
		fe, err := f.Mem.ReadFence(ctx)
		if fe.Gen != f.beforeGen {
			f.retrying, f.lastTryAt = true, now
		}
		return fe, err
	}
	return f.Mem.ReadFence(ctx)
}

func (f *twoLoopsFence) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	if f.active && f.loseAcq > 0 {
		f.loseAcq--
		f.stepTick()
		return false, nil
	}
	return f.Mem.Acquire(ctx, gen, op)
}

func TestTwoTickLoopsOnAFakeClock(t *testing.T) {
	t.Parallel()
	for trial := 0; trial < crScale.LoopTrials; trial++ {
		w := crSprint(t, uint64(trial+1))
		h := w.h
		h.st.Sleep = func(d time.Duration) { h.mu.Lock(); h.now = h.now.Add(d); h.mu.Unlock() }
		h.st.Rand = func(n int64) int64 { return n / 2 }
		h.startMachine()
		mk := func(actor string) *Store {
			return &Store{B: h.m, Names: h.st.Names, Actor: actor, Now: h.st.Now, NewID: h.st.NewID, Sleep: func(time.Duration) {}}
		}
		a, b := mk("machine"), mk("machine")
		f := &twoLoopsFence{Mem: h.m, ctx: h.ctx, now: h.st.Now, a: a, b: b, limit: 2 * crScale.LoopTicks}
		h.st.B = f

		f.stepTick()
		beforeRev := h.revisionsAll()
		f.active, f.loseAcq = true, FenceTries
		lostRes, err := h.st.Run(h.ctx, TakeStep(sprint.TakeReq{As: crMembers[0], Sel: sprint.Sel{Limit: 100}, Who: crMembers[0]}))
		f.active = false
		require.NoError(t, err)
		require.True(t, lostRes.Lost && lostRes.Attempts == FenceTries && lostRes.Op == "" && len(lostRes.Moved) == 0 && lostRes.Notes == 0 && len(lostRes.Refused) > 0, "lost step: %+v", lostRes)
		require.Equal(t, beforeRev, h.revisionsAll())

		f.burst = FenceTries
		const busyTries = 20
		cut, busyRetries := 0, 0
		run := func(step Step) {
			f.active = true
			defer func() { f.active, f.trailing = false, false }()
			for try := 1; ; try++ {
				before := h.revisionsAll()
				_, err := h.st.Run(h.ctx, step)
				if err == nil {
					return
				}
				if strings.Contains(err.Error(), "changed under the step") {
					cut++
					return
				}
				var busy *FenceBusyError
				require.ErrorAs(t, err, &busy, "%s: %v", step.Verb, err)
				require.Equal(t, FenceTries, busy.Reads)
				require.Equal(t, before, h.revisionsAll())
				if try >= busyTries {
					require.NoError(t, err, "outside verb %q lost the fence to two run loops: still busy after %d outer tries (%d fence reads on the last, %d busy retries this trial)", step.Verb, try, busy.Reads, busyRetries+1)
					return
				}
				busyRetries++
			}
		}
		w.running = true
		for r := 1; r <= crScale.LoopRounds; r++ {
			for _, m := range crMembers {
				run(TakeStep(sprint.TakeReq{As: m, Sel: sprint.Sel{Limit: 100}, Who: m}))
				s := h.snap()
				for _, c := range s.Fleet.Cell(m, sprint.Working) {
					run(FinishStep(sprint.FinishReq{As: m, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}, Who: m}))
				}
			}
			for _, rd := range crReaders {
				run(ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: rd, Verdict: "ok", Sel: sprint.Sel{Limit: 100}, Who: rd}))
			}
			w.coordinate(r)
			for _, st := range []string{"s1", "s2", "s3"} {
				run(MergeStep(sprint.MergeReq{Stream: st, Batch: 3}))
			}
			f.stepTick()
			f.stepTick()
			h.tick(time.Second)
		}
		for f.ticks < f.limit {
			f.stepTick()
		}
		h.st.B = h.m
		for i := 0; i < 3; i++ {
			h.machine()
		}
		h.clean(fmt.Sprintf("trial %d", trial))
		require.Positive(t, busyRetries)
		assert.LessOrEqual(t, cut, 0, "trial %d: %d outside verbs were told \"cut ... changed under the step\" while two ticks ran (their operation was applied by a tick's Fenced finish)", trial, cut)
		t.Logf("trial %d: %d busy-fence retries, %d cut", trial, busyRetries, cut)
		if len(f.errs) > 0 {
			t.Logf("trial %d: %d tick errors, e.g. %s", trial, len(f.errs), f.errs[0])
		}
		notes, _, _ := h.m.NotesSince(h.ctx, "", 100000)
		seen := map[string]int{}
		for _, n := range notes {
			if n.Who == sprint.MachineActor && n.Kind == sprint.Judgment {
				seen[n.Type+"|"+strings.Join(n.Subjects(), ",")+"|"+n.What]++
			}
		}
		for k, v := range seen {
			assert.LessOrEqual(t, v, 1, "trial %d: the machine wrote %s %d times", trial, k, v)
		}
		s := h.snap()
		for _, m := range crMembers {
			n := heldBy(s, m)
			assert.LessOrEqual(t, n, s.Width(m), "trial %d: %s holds %d, its width %d", trial, m, n, s.Width(m))
		}
		for _, c := range s.Work.Column(sprint.Review) {
			n := 0
			for _, rc := range s.Readers.Of(c.ID) {
				if rc.Int("attempt") == c.Int("attempt") {
					n++
				}
			}
			assert.LessOrEqual(t, n, 2, "trial %d: %s asked of %d readers at one attempt", trial, c.ID, n)
		}
	}
}
