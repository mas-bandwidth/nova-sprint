package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// logShared is the log state the zombie test's stores share: every apply in
// the trial lands in one log, kept ordered by the mutex.
type logShared struct {
	mu  sync.Mutex
	log []string
}

// logMem wraps a Mem so the test can see what the machine did. The varying
// step is a field: who names the store, rev chooses the "rev" apply label over
// "exp" (the two labels nova-sprint logs for the same field).
type logMem struct {
	*Mem
	*logShared
	who string
	rev bool
}

func (l *logMem) add(s string) {
	l.mu.Lock()
	l.logShared.log = append(l.logShared.log, s)
	l.mu.Unlock()
}

func (l *logMem) Apply(ctx context.Context, m ntable.BatchManifest) (ntable.Receipt, error) {
	r, err := l.Mem.Apply(ctx, m)
	var ids []string
	for _, e := range m.Members {
		if hasChanges(e) {
			ids = append(ids, e.ID)
		}
	}
	kind := "rev"
	if !l.rev {
		kind = "exp"
	}
	l.add(fmt.Sprintf("%s apply %s %s %s %s -> %v err=%v replay=%v [%s]", l.who, m.Table, m.OperationID, kind, m.ExpectedTableRevision, r.After, err, r.Replay, strings.Join(ids, " ")))
	return r, err
}
func (l *logMem) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	ok, err := l.Mem.Acquire(ctx, gen, op)
	l.add(fmt.Sprintf("%s acquire %s gen %d ok=%v", l.who, op.ID, gen, ok))
	return ok, err
}
func (l *logMem) Release(ctx context.Context, op OpRecord, commit bool) error {
	had := l.Pending()
	err := l.Mem.Release(ctx, op, commit)
	hid := ""
	if had != nil {
		hid = had.ID
	}
	l.add(fmt.Sprintf("%s release %s commit=%v (fence held %s)", l.who, op.ID, commit, hid))
	return err
}

func TestCRZombieWriter(t *testing.T) {
	t.Parallel()
	for trial := 0; trial < crScale.ZombieTrials; trial++ {
		w := crSprint(t, uint64(trial+1))
		h := w.h
		h.startMachine()
		lm := &logShared{}
		mk := func(who string) *Store {
			return &Store{B: &logMem{Mem: h.m, logShared: lm, who: who}, Names: h.st.Names, Actor: "machine", Now: h.st.Now, NewID: h.st.NewID, Sleep: func(time.Duration) {}}
		}
		a, b := mk("A"), mk("B")
		h.st.B = &logMem{Mem: h.m, logShared: lm, who: "W"}
		var wg sync.WaitGroup
		stop := make(chan struct{})
		loop := func(st *Store) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = st.Tick(h.ctx)
			}
		}
		wg.Add(2)
		go loop(a)
		go loop(b)
		var bad error
		for r := 1; r <= crScale.ZombieRounds && bad == nil; r++ {
			for _, m := range crMembers {
				if _, err := h.st.Run(h.ctx, TakeStep(sprint.TakeReq{As: m, Sel: sprint.Sel{Limit: 100}, Who: m})); err != nil {
					bad = err
					break
				}
				s := h.snap()
				for _, c := range s.Fleet.Cell(m, sprint.Working) {
					if _, err := h.st.Run(h.ctx, FinishStep(sprint.FinishReq{As: m, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}, Who: m})); err != nil {
						bad = err
					}
				}
			}
			h.tick(time.Second)
		}
		close(stop)
		wg.Wait()
		if bad != nil {
			msg := bad.Error()
			t.Logf("trial %d: %s", trial, msg)
			var member string
			if i := strings.Index(msg, "member "); i >= 0 {
				member = strings.Fields(msg[i+7:])[0]
			}
			var tail []string
			for _, l := range lm.log {
				if strings.Contains(l, member) || strings.Contains(l, "acquire") && strings.Contains(l, "ok=true") || strings.Contains(l, "release") {
					tail = append(tail, l)
				}
			}
			if len(tail) > 40 {
				tail = tail[len(tail)-40:]
			}
			t.Logf("log:\n%s", strings.Join(tail, "\n"))
			require.NoError(t, bad)
		}
	}
}
