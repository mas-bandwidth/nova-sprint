package store

// Cold reader: two run loops on one sprint, with the outside world moving.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCRTwoRunLoopsAtOnce(t *testing.T) {
	t.Parallel()
	for trial := 0; trial < crScale.LoopTrials; trial++ {
		w := crSprint(t, uint64(trial+1))
		h := w.h
		h.startMachine()
		mk := func(actor string) *Store {
			return &Store{B: h.m, Names: h.st.Names, Actor: actor, Now: h.st.Now, NewID: h.st.NewID, Sleep: func(time.Duration) {}}
		}
		a, b := mk("machine"), mk("machine")
		var wg sync.WaitGroup
		var mu sync.Mutex
		var errs []string
		loop := func(st *Store) {
			defer wg.Done()
			for i := 0; i < crScale.LoopTicks; i++ {
				if _, err := st.Tick(h.ctx); err != nil {
					mu.Lock()
					errs = append(errs, err.Error())
					mu.Unlock()
				}
			}
		}
		wg.Add(2)
		go loop(a)
		go loop(b)
		// the outside world, serially, in this goroutine; a verb told it was
		// cut is counted (and checked below), not fatal. Under two tick loops
		// the store may also answer a verb with its documented retryable
		// refusal (FenceBusyError, "the sprint is busy"): the verb changed
		// nothing and asks to be run again, so run it again, up to a bound.
		// A verb starved past the bound is an outside verb losing to two tick
		// loops and fails the test, never a bigger bound.
		const busyTries = 20
		cut := 0
		busyRetries := 0
		run := func(step Step) {
			for try := 1; ; try++ {
				_, err := h.st.Run(h.ctx, step)
				if err == nil {
					return
				}
				if strings.Contains(err.Error(), "changed under the step") {
					cut++
					return
				}
				if !IsFenceBusy(err) {
					require.NoError(t, err, "%s: %v", step.Verb, err)
					return
				}
				if try >= busyTries {
					require.NoError(t, err, "%s: still busy after %d tries (%d busy retries this trial)", step.Verb, try, busyRetries+1)
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
			h.tick(time.Second)
		}
		wg.Wait()
		for i := 0; i < 3; i++ {
			h.machine()
		}
		h.clean(fmt.Sprintf("trial %d", trial))
		assert.LessOrEqual(t, cut, 0, "trial %d: %d outside verbs were told \"cut ... changed under the step\" while two ticks ran (their operation was applied by a tick's Fenced finish)", trial, cut)
		t.Logf("trial %d: %d busy-fence retries, %d cut", trial, busyRetries, cut)
		if len(errs) > 0 {
			t.Logf("trial %d: %d tick errors, e.g. %s", trial, len(errs), errs[0])
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
