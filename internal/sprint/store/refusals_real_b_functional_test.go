//go:build functional

package store

// The refusals R19-R37 of L1-STORE-CONTRACT.md section 3 on a real store,
// except those already covered on Redis (R22, R23, in
// redis_sprint_functional_test.go). Each subtest asserts the exact refusal and
// starts its own Redis the way redis_sprint_functional_test.go does
// (liveClient, testutil.StartRaw).
//
// These run only in the functional tier (make test-functional-container).
// The clock is the harness's own (h.now); the store sleeps on no time: every
// store here has Sleep set to a no-op, so a refusal that is given up on
// (FenceBusyError, Lost) takes its tries at once.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// failAcquire is a backend whose every acquire fails while leaving the fence
// unchanged, as a broken connection to the fence does.
type failAcquire struct {
	Backend
}

func (f failAcquire) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	return false, errors.New("connection refused")
}

// loseAcquire is a backend whose every acquire is lost to another writer: the
// fence is never taken, and the step plans again (G22, R25).
type loseAcquire struct {
	Backend
}

func (l loseAcquire) Acquire(context.Context, uint64, OpRecord) (bool, error) { return false, nil }

// Unwrap keeps the real store behind the seam: the store's reads stay the
// Redis pipeline (pipeline_load.go redisBackend).
func (l loseAcquire) Unwrap() Backend { return l.Backend }

// realFenceMover is a backend whose every fence read moves the fence's
// generation on: another writer takes the fence between the read of the
// tables and the read of the fence after them (G2, R27).
type realFenceMover struct {
	Backend
	c      *redis.Client
	genKey string
}

func (m realFenceMover) Unwrap() Backend { return m.Backend }

func (m realFenceMover) ReadFence(ctx context.Context) (Fence, error) {
	f, err := m.Backend.ReadFence(ctx)
	if err != nil {
		return f, err
	}
	// the generation moves on the real store, as another writer's take does
	m.c.Set(ctx, m.genKey, f.Gen+1, 0)
	return f, nil
}

// failAfterCommit is a backend whose reads fail once the step's operation has
// been committed: the step's own write stands, and the sync of the display
// cells that follows it fails (R28).
type failAfterCommit struct {
	Backend
	committed bool
}

func (f *failAfterCommit) Unwrap() Backend { return f.Backend }

func (f *failAfterCommit) Release(ctx context.Context, op OpRecord, commit bool) error {
	err := f.Backend.Release(ctx, op, commit)
	if commit {
		f.committed = true
	}
	return err
}

func (f *failAfterCommit) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	if f.committed {
		return nil, errors.New("the display cells did not sync")
	}
	return f.Backend.Shapes(ctx, tables)
}

// failApply is a backend that does not answer the apply of one operation id:
// a pending operation whose manifest the store does not confirm cannot
// finish (R26).
type failApply struct {
	Backend
	op string
}

func (f failApply) Unwrap() Backend { return f.Backend }

func (f failApply) Apply(ctx context.Context, m ntable.BatchManifest) (ntable.Receipt, error) {
	if m.OperationID == f.op {
		return ntable.Receipt{}, ErrUnknown
	}
	return f.Backend.Apply(ctx, m)
}

// epochMover moves the sprint's epoch hash while a watch holds it: the watch's
// transaction is refused, as it is when a clear advances the epoch (R33).
type epochMover struct {
	key   string
	c     *redis.Client
	moved bool
}

func (m *epochMover) DialHook(next redis.DialHook) redis.DialHook { return next }

func (m *epochMover) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err != nil || m.moved {
			return err
		}
		if cmd.Name() == "hmget" && len(cmd.Args()) > 1 && fmt.Sprint(cmd.Args()[1]) == m.key {
			m.moved = true
			// the epoch moves while the watch holds the epoch hash
			if e := m.c.HSet(ctx, m.key, "n", "9").Err(); e != nil {
				return e
			}
		}
		return err
	}
}

func (m *epochMover) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// TestRefusalsLateOnRedis is the refusals R19-R37 of L1-STORE-CONTRACT.md
// section 3 on a real store, except those already covered on Redis (R22, R23,
// in redis_sprint_functional_test.go). Each subtest starts its own Redis and
// asserts the exact refusal.
func TestRefusalsLateOnRedis(t *testing.T) {
	t.Parallel()

	// R21: the store did not take the step's record: acquire errored and the
	// fence is unchanged.
	t.Run("R21_store_did_not_take_the_record", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		h.setup(1)
		st := *h.st
		st.B = failAcquire{Backend: h.st.B}
		res, err := st.Run(h.ctx, DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
		require.NoError(t, err, "R21 err: %v", err)
		require.Len(t, res.Refused, 1, "R21 refusals: %+v", res.Refused)
		require.Contains(t, res.Refused[0].Why, "did not take", "R21: %s", res.Refused[0].Why)
	})

	// R25: every attempt lost to another writer: the step lost, every key it
	// would have moved is refused, and nothing was written.
	t.Run("R25_lost_every_attempt", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		h.setup(1)
		st := *h.st
		st.B = loseAcquire{Backend: h.st.B}
		res, err := st.Run(h.ctx, DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
		require.NoError(t, err, "R25 err: %v", err)
		require.True(t, res.Lost, "R25: the step lost every attempt: %+v", res)
		require.Len(t, res.Refused, 1, "R25 refusals: %+v", res.Refused)
		require.Contains(t, res.Refused[0].Why, "kept changing under this step", "R25: %s", res.Refused[0].Why)
		require.Equal(t, "", res.Op, "R25: nothing was written: %+v", res)
	})

	// R26: an operation in the fence that cannot finish, past the grace: the
	// verb cannot read the sprint and says which operation is pending.
	t.Run("R26_pending_past_the_grace", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		h.setup(1)
		s := h.snap()
		ctl := s.Merge.Card(sprint.CtlID("s1"))
		p1 := s.Work.Card("s1-1")
		guard := func(c *sprint.Card) *ntable.MemberExpect {
			return &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev), Place: &ntable.PlaceExpect{Row: c.Row, Col: c.Col}}
		}
		// A cut operation: its first manifest applies, and its second is never
		// confirmed, so it stays open past its grace.
		op := OpRecord{ID: "deal-cut", Verb: "deal", At: h.now.Add(-2 * time.Minute),
			Manifests: []ntable.BatchManifest{
				{Schema: 1, Table: h.st.Names.Table(sprint.Merge), Epoch: "0",
					ExpectedTableRevision: fmt.Sprint(s.Merge.Revision), OperationID: "deal-cut-1", Actor: h.st.Actor,
					Members: []ntable.BatchMemberEntry{{ID: ctl.ID, Expect: guard(ctl), Set: map[string]string{"probe": "1"}}}},
				{Schema: 1, Table: h.st.Names.Table(sprint.Work), Epoch: "0",
					ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: "deal-cut-2", Actor: h.st.Actor,
					Members: []ntable.BatchMemberEntry{{ID: p1.ID, Expect: guard(p1), Set: map[string]string{"probe": "1"}}}},
			}}
		f, err := h.st.B.ReadFence(h.ctx)
		require.NoError(t, err, "R26 read fence: %v", err)
		ok, err := h.st.B.Acquire(h.ctx, f.Gen, op)
		require.NoError(t, err, "R26 acquire: %v", err)
		require.True(t, ok, "R26 acquire: the operation is not in the fence")
		st := *h.st
		st.B = failApply{Backend: h.st.B, op: "deal-cut-2"}
		_, err = st.Run(h.ctx, FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m9"}))
		var pe *PendingError
		require.ErrorAs(t, err, &pe, "R26: %v", err)
		require.Equal(t, "deal-cut", pe.Op, "R26: %v", err)
		require.Contains(t, err.Error(), "run: nova-sprint repair", "R26: %v", err)
	})

	// R27: a fenced read that keeps losing gives up with nothing changed.
	t.Run("R27_fence_busy", func(t *testing.T) {
		t.Parallel()
		h, c := liveHarness(t)
		st := *h.st
		st.Sleep = func(time.Duration) {}
		st.B = realFenceMover{Backend: h.st.B, c: c, genKey: h.st.Names.KeyAt(keyGen, 0)}
		_, _, err := st.Fenced(h.ctx, All, nil, nil)
		require.Error(t, err, "R27: the read gave up")
		var busy *FenceBusyError
		require.ErrorAs(t, err, &busy, "R27: %v", err)
		require.Contains(t, err.Error(), "other operations kept the fence moving", "R27: %v", err)
		require.Contains(t, err.Error(), "nothing was changed", "R27: %v", err)
	})

	// R28: the step's write committed and the display cells did not sync.
	t.Run("R28_sync_failed", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		h.setup(1)
		st := *h.st
		seam := &failAfterCommit{Backend: h.st.B}
		st.B = seam
		_, err := st.Run(h.ctx, DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
		require.Error(t, err, "R28: the step's write committed and the sync failed")
		var se *SyncError
		require.ErrorAs(t, err, &se, "R28: %v", err)
		require.Contains(t, err.Error(), "the step's write committed", "R28: %v", err)
		require.True(t, seam.committed, "R28: the step's write committed before the sync failed")
	})

	// R30: a conditional delete at an epoch the sprint has left is refused
	// with a *GuardError.
	t.Run("R30_guard_moved", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		r := h.st.B.(*Redis)
		moved := *r
		moved.Pinned = 7 // the epoch the sprint has left
		guard := RowGuard{Row: "m1", ID: "m1", Key: moved.Names.RecordKey(sprint.Fleet, "m1")}
		rows, err := moved.RowsDelIf(h.ctx, moved.Names.Table(sprint.Fleet), []RowGuard{guard})
		require.Error(t, err, "R30: the delete wrote something: %v", rows)
		var ge *GuardError
		require.ErrorAs(t, err, &ge, "R30: %v", err)
		require.Equal(t, GuardMoved, ge.Outcome, "R30: %v", err)
		require.Contains(t, err.Error(), "the sprint left the epoch the delete pinned", "R30: %v", err)
	})

	// R31: a guarded verb that found the sprint at another epoch writes
	// nothing and says the sprint was cleared under it.
	t.Run("R31_cleared_under_verb", func(t *testing.T) {
		t.Parallel()
		h, c := liveHarness(t)
		st := *h.st
		st.beforeGuardedWrite = func() {
			// a clear advances the epoch between the verb's pin and its write
			c.HSet(h.ctx, h.st.Names.EpochKey(), "n", "7")
		}
		err := st.SetReaderTiers(h.ctx, []string{"reader-a"}, "tier: pro")
		require.ErrorIs(t, err, ErrClearedUnderVerb, "R31: %v", err)
		require.Contains(t, err.Error(), "nothing was changed", "R31: %v", err)
	})

	// R32: a judgment id of another epoch is refused by SetReview, naming its
	// epoch.
	t.Run("R32_setreview_other_epoch", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		err := h.st.SetReview(h.ctx, "j5~3", h.now.Add(time.Hour))
		require.Error(t, err, "R32 err")
		require.Contains(t, err.Error(), "belongs to epoch 3", "R32: %v", err)
	})

	// R33: the epoch moved while the set-defaults step was settled: the
	// transaction is refused and the step says so.
	t.Run("R33_epoch_moved_while_settling", func(t *testing.T) {
		t.Parallel()
		h, c := liveHarness(t)
		// the sprint is at epoch 1 and owes the set-defaults step
		require.NoError(t, c.HSet(h.ctx, h.st.Names.EpochKey(), "n", "1", "defaults", "1").Err())
		mover := &epochMover{key: h.st.Names.EpochKey(), c: c}
		watched := redis.NewClient(&redis.Options{Addr: c.Options().Addr})
		t.Cleanup(func() { _ = watched.Close() })
		watched.AddHook(mover)
		r := &Redis{C: watched, Names: h.st.Names, Now: h.st.Now, Pinned: 1}
		err := r.SettleDefaults(h.ctx, 1)
		require.Error(t, err, "R33: the settle wrote something")
		require.Contains(t, err.Error(), "the sprint's epoch moved", "R33: %v", err)
		require.True(t, mover.moved, "R33: the epoch moved inside the watch")
	})

	// R34: a fence holding a non-JSON operation record is unreadable.
	t.Run("R34_unreadable_fence_record", func(t *testing.T) {
		t.Parallel()
		h, c := liveHarness(t)
		ctx := h.ctx
		require.NoError(t, c.Set(ctx, h.st.Names.Key("fence"), "not json", 0).Err())
		_, err := h.st.B.ReadFence(ctx)
		require.Error(t, err, "R34 err")
		require.Contains(t, err.Error(), "unreadable", "R34: %v", err)
	})

	// R35: any write through a read-only backend is refused with ErrReadOnly.
	t.Run("R35_readonly", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		ro := ReadOnly(h.st.B)
		err := ro.RowsAdd(h.ctx, h.st.Names.Table(sprint.Work), []string{"x"})
		require.ErrorIs(t, err, ErrReadOnly, "R35: %v", err)
	})

	// R36: a stop of a READY machine (never started since the clear) is
	// refused with ErrNothingToStop.
	t.Run("R36_nothing_to_stop", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		_, _, _, err := h.st.StopUntil(h.ctx, "stop", time.Time{})
		require.ErrorIs(t, err, ErrNothingToStop, "R36: %v", err)
	})

	// R37: a start whose run generation is exhausted writes nothing.
	t.Run("R37_start_generation_exhausted", func(t *testing.T) {
		t.Parallel()
		h, c := liveHarness(t)
		body, err := json.Marshal(Machine{State: Stopped, RunSeq: ^uint64(0)})
		require.NoError(t, err, "R37: %v", err)
		require.NoError(t, c.Set(h.ctx, h.st.Names.Key(keyMachine), body, 0).Err(), "R37: the machine record")
		_, _, _, err = h.st.SetMachine(h.ctx, true)
		require.Error(t, err, "R37: the start wrote something")
		require.Contains(t, err.Error(), "start generation is exhausted", "R37: %v", err)
		require.Contains(t, err.Error(), "nothing was changed", "R37: %v", err)
	})
}
