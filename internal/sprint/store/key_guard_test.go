package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPresenceGuardRejectsChangedProofAtAcquireAndRelock(t *testing.T) {
	t.Parallel()
	for _, relock := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquire", true: "relock"}[relock], func(t *testing.T) {
			h := newHarness(t)
			key := friendBeatKey("amy")
			require.NoError(t, h.m.SetKey(h.ctx, key, "down proof"))
			f, err := h.m.ReadFence(h.ctx)
			require.NoError(t, err)
			op := OpRecord{ID: "recovery", KeyGuards: []KeyGuard{{Key: key, Value: "down proof", Present: true}}}
			if relock {
				ok, err := h.m.Acquire(h.ctx, f.Gen, OpRecord{ID: "lock", Lock: true})
				require.NoError(t, err)
				require.True(t, ok)
			}
			require.NoError(t, h.m.SetKey(h.ctx, key, "new up proof"))
			var ok bool
			err = nil
			if relock {
				ok, err = h.m.Relock(h.ctx, "lock", op)
			} else {
				ok, err = h.m.Acquire(h.ctx, f.Gen, op)
			}
			require.NoError(t, err)
			require.False(t, ok, "a proof written after planning invalidates recovery")
		})
	}
}

func TestPresenceGuardDistinguishesMissingFromEmptyAndAllowsUnchanged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	key := friendHealthKey("amy")
	f, err := h.m.ReadFence(h.ctx)
	require.NoError(t, err)
	op := OpRecord{ID: "recovery", KeyGuards: []KeyGuard{{Key: key}}}
	require.NoError(t, h.m.SetKey(h.ctx, key, ""))
	ok, err := h.m.Acquire(h.ctx, f.Gen, op)
	require.NoError(t, err)
	require.False(t, ok)
	op.KeyGuards[0].Present = true
	ok, err = h.m.Acquire(h.ctx, f.Gen, op)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRedisPresenceGuardsAtAcquireAndRelock(t *testing.T) {
	t.Parallel()
	for _, relock := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquire", true: "relock"}[relock], func(t *testing.T) {
			r, ctx := coverRedis(t, 0)
			key := friendBeatKey("amy")
			require.NoError(t, r.SetKey(ctx, key, "old proof"))
			op := OpRecord{ID: "recovery", KeyGuards: []KeyGuard{{Key: key, Value: "old proof", Present: true}}}
			if relock {
				ok, err := r.Acquire(ctx, 0, OpRecord{ID: "lock", Lock: true})
				require.NoError(t, err)
				require.True(t, ok)
			}
			require.NoError(t, r.SetKey(ctx, key, "new proof"))
			var ok bool
			var err error
			if relock {
				ok, err = r.Relock(ctx, "lock", op)
			} else {
				ok, err = r.Acquire(ctx, 0, op)
			}
			require.NoError(t, err)
			require.False(t, ok)
			op.KeyGuards[0].Value = "new proof"
			if relock {
				ok, err = r.Relock(ctx, "lock", op)
			} else {
				ok, err = r.Acquire(ctx, 0, op)
			}
			require.NoError(t, err)
			require.True(t, ok, "unchanged proof allows reservation")
		})
	}
}
