//go:build functional

package store

// The refusals R19-R37 of L1-STORE-CONTRACT.md section 3 on a real store,
// except those already covered on Redis (R22, R23, in
// redis_sprint_functional_test.go). Each subtest asserts the exact refusal and
// starts its own Redis the way redis_sprint_functional_test.go does
// (liveClient, testutil.StartRaw).
//
// These run only in the functional tier (make test-functional-container).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
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

	// R32: a judgment id of another epoch is refused by SetReview, naming its
	// epoch.
	t.Run("R32_setreview_other_epoch", func(t *testing.T) {
		t.Parallel()
		h, _ := liveHarness(t)
		err := h.st.SetReview(h.ctx, "j5~3", h.now.Add(time.Hour))
		require.Error(t, err, "R32 err")
		require.Contains(t, err.Error(), "belongs to epoch 3", "R32: %v", err)
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
}
