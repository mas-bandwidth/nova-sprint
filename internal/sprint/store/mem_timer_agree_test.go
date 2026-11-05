package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemAndRedisRefuseAnUnreadableTimer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	op := OpRecord{ID: "close-timer", Verb: "tick", CloseTimers: []string{"timer"}}

	mem := NewMem()
	mem.kv[keyTimers] = "{" // the record has started but cannot be decoded
	ok, err := mem.Acquire(ctx, 0, op)
	require.NoError(t, err)
	require.True(t, ok)
	memErr := mem.Release(ctx, op, true)

	redisStore, redisCtx := coverRedis(t, 0)
	fake := redisStore.C.(*coverRedisClient)
	fake.db.keys[redisStore.Names.Key(keyTimers)] = &coverVal{kind: coverString, s: "{"}
	ok, err = redisStore.Acquire(redisCtx, 0, op)
	require.NoError(t, err)
	require.True(t, ok)
	redisErr := redisStore.Release(redisCtx, op, true)

	require.Error(t, redisErr)
	require.EqualError(t, memErr, redisErr.Error())
}
