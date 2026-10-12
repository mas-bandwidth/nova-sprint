//go:build functional

package store

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recoveryWatchHook struct {
	fence string
	fire  func()
	armed bool
}

func (h *recoveryWatchHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (h *recoveryWatchHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (h *recoveryWatchHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			args := cmd.Args()
			if h.armed && cmd.Name() == "set" && len(args) > 1 && args[1] == h.fence {
				h.armed = false
				h.fire()
				break
			}
		}
		return next(ctx, cmds)
	}
}

// This changes a watched proof after its MGET and before EXEC using a second
// connection to the test's own Redis. It exercises actual WATCH invalidation,
// not merely the memory backend or a mocked transaction.
func TestRedisRecoveryProofChangeBetweenReadAndExec(t *testing.T) {
	for _, lock := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquire", true: "relock"}[lock], func(t *testing.T) {
			st, c := liveStore(t)
			ctx := context.Background()
			p, err := st.Pinned(ctx)
			require.NoError(t, err)
			r := p.B.(*Redis)
			other := redis.NewClient(c.Options())
			t.Cleanup(func() { _ = other.Close() })
			key := friendBeatKey("amy")
			require.NoError(t, r.SetKey(ctx, key, "down proof"))
			f, err := r.ReadFence(ctx)
			require.NoError(t, err)
			op := OpRecord{ID: "recovery", KeyGuards: []KeyGuard{{Key: key, Value: "down proof", Present: true}}}
			if lock {
				ok, err := r.Acquire(ctx, f.Gen, OpRecord{ID: "lock", Lock: true})
				require.NoError(t, err)
				require.True(t, ok)
			}
			hook := &recoveryWatchHook{fence: r.key(keyFence), armed: true, fire: func() { require.NoError(t, other.Set(ctx, r.Names.Key(key), "fresh up proof", 0).Err()) }}
			c.AddHook(hook)
			var ok bool
			if lock {
				ok, err = r.Relock(ctx, "lock", op)
			} else {
				ok, err = r.Acquire(ctx, f.Gen, op)
			}
			require.NoError(t, err)
			require.False(t, ok)
			require.False(t, hook.armed, "the proof changed inside the actual watched transaction")
			got, err := r.ReadFence(ctx)
			require.NoError(t, err)
			if lock {
				require.NotNil(t, got.Pending)
				assert.Equal(t, "lock", got.Pending.ID)
			} else {
				assert.Nil(t, got.Pending)
			}
		})
	}
}

func TestRedisRunningTickRecoversDownLeases(t *testing.T) {
	h, _ := liveHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1, Class: "flash"}, {Name: "bob", Width: 1, Class: "flash"}})
	require.NoError(t, err)
	h.up("amy")
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: "Task.\nREPO: mas-bandwidth/nova-tools\nWHO: friend\n\nTask."}}}))
	h.startMachine()
	h.machine()
	downAmy(h)
	h.up("bob")
	h.st.RecoveryHeads = func(_ context.Context, requests []sprint.RecoveryHeadRequest) []sprint.RecoveryHead {
		var out []sprint.RecoveryHead
		for _, request := range requests {
			out = append(out, sprint.RecoveryHead{RecoveryHeadRequest: request, Head: strings.Repeat("a", 40)})
		}
		return out
	}
	h.machine()
	c := h.snap().Fleet.Card("s1-1.w1")
	require.Equal(t, sprint.FriendRow("bob"), c.Row)
	assert.Equal(t, 3, c.Int("gen"))
	assert.Equal(t, strings.Repeat("a", 40), c.F(sprint.FieldCarryHead))
	assert.Empty(t, c.F(sprint.FieldRecoveryPending))
	assert.Empty(t, sprint.Check(h.snap(), nil))
}
