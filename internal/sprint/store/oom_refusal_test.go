package store

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// oomPipeline is a fake Redis pipeline: every queued command carries Redis's
// OOM refusal and EXEC returns an EXECABORT, as the live store did when its
// maxmemory was reached. It never dials.
type oomPipeline struct {
	oom     error
	execErr error
}

func (h *oomPipeline) DialHook(redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("the OOM fake does not dial")
	}
}

func (h *oomPipeline) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch strings.ToLower(cmd.Name()) {
		case "mget":
			cmd.(*redis.SliceCmd).SetVal([]any{nil, "0"})
			return nil
		case "hget":
			return redis.Nil
		case "watch", "unwatch", "multi", "exec":
			return nil
		}
		return next(ctx, cmd)
	}
}

func (h *oomPipeline) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(_ context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if strings.EqualFold(cmd.Name(), "set") {
				cmd.SetErr(h.oom)
			}
		}
		return h.execErr
	}
}

// oomAcquire is the store's backend with Acquire refusing as the OOM pipeline
// did: every other read is the in-memory store.
type oomAcquire struct {
	*Mem
	err error
}

func (b *oomAcquire) Acquire(context.Context, uint64, OpRecord) (bool, error) {
	return false, b.err
}

// TestOOMRefusalNamesTheStoreFull: an EXECABORT whose queued command carries
// Redis's OOM refusal becomes the store-full refusal on the real step path,
// never the record's size (tla/StoreRetention.tla Bounded).
func TestOOMRefusalNamesTheStoreFull(t *testing.T) {
	t.Parallel()
	oom := errors.New("OOM command not allowed when used memory > 'maxmemory'.")
	execErr := errors.New("EXECABORT Transaction discarded because of previous errors")

	// The fake pipeline through Redis.Acquire: the EXECABORT is unwrapped to
	// the queued SET's OOM error.
	c := redis.NewClient(&redis.Options{Addr: "store.invalid:1"})
	c.AddHook(&oomPipeline{oom: oom, execErr: execErr})
	t.Cleanup(func() { _ = c.Close() })
	r := &Redis{C: c, Names: sprint.Names{Prefix: "t-"}, Pinned: 0}
	_, acqErr := r.Acquire(context.Background(), 0, OpRecord{ID: "op1", Verb: "tick"})
	require.Error(t, acqErr)
	require.Contains(t, acqErr.Error(), "OOM", "Acquire kept the EXECABORT instead of the queued OOM")
	require.NotContains(t, acqErr.Error(), "EXECABORT")

	// The step path: Store.Run says the store is full, not how large its
	// record was.
	h := newHarness(t)
	h.setup(1)
	st := *h.st
	st.B = &oomAcquire{Mem: h.m, err: acqErr}
	step := AddStep(sprint.AddReq{Stream: "s2", Count: 1})
	res, err := st.Run(h.ctx, step)
	require.NoError(t, err)
	require.Len(t, res.Refused, 1, "the step was not refused: %+v", res)
	why := res.Refused[0].Why
	require.Contains(t, why, "the store is full")
	require.Contains(t, why, "OOM")
	require.Contains(t, why, "maxmemory")
	require.NotContains(t, why, "bytes")
	require.NotContains(t, why, "EXECABORT")

	direct := storeRefusal(OpRecord{}, oom)
	require.Contains(t, direct, "the store is full")
	require.NotContains(t, direct, "bytes")
}
