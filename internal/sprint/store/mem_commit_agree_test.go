package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

func TestMemCommitIsAllOrNothingLikeRedis(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op := OpRecord{
		ID: "op-1", Verb: "test", CallerOp: "caller-1", Result: "result",
		Log:   []sprint.Line{{Verb: "test"}},
		Notes: []sprint.Note{{ID: "note-1", Kind: sprint.Judgment}},
	}
	wantFailure := errors.New("forced commit failure")

	t.Run("Mem", func(t *testing.T) {
		m := NewMem()
		_, err := m.Acquire(ctx, 0, op)
		require.NoError(t, err)
		m.Fail = func(point string) error {
			if point == "commit" {
				return wantFailure
			}
			return nil
		}
		err = m.Release(ctx, op, true)
		require.ErrorIs(t, err, wantFailure)

		m.mu.Lock()
		defer m.mu.Unlock()
		l := m.log()
		require.Empty(t, l.lines, "the failed commit must not append its log")
		require.Empty(t, l.inbox, "the failed commit must not publish its note")
		require.Empty(t, l.notes, "the failed commit must not store its note")
		require.Empty(t, l.aliases, "the failed commit must not allocate its alias")
		require.Empty(t, l.open, "the failed commit must not open its judgment")
		require.Empty(t, l.queue, "the failed commit must not change its queue")
		_, recorded := l.done[op.CallerOp]
		require.False(t, recorded, "the failed commit must not record its caller result")
		require.NotNil(t, l.fence, "the failed commit must leave its operation fenced")
	})

	t.Run("Redis", func(t *testing.T) {
		r, ctx := coverRedis(t, 1)
		body, err := json.Marshal(op)
		require.NoError(t, err)
		require.NoError(t, r.C.Set(ctx, r.key(keyFence), string(body), 0).Err())
		// The wrong type forces Redis.commit to fail while preparing its transaction,
		// after the log append has been queued but before EXEC can publish it.
		r.C.(*coverRedisClient).db.keys[r.key(keyAliases)] = &coverVal{kind: coverString, s: "wrong type"}

		err = r.Release(ctx, op, true)
		require.Error(t, err)
		db := r.C.(*coverRedisClient).db
		db.mu.Lock()
		defer db.mu.Unlock()
		require.Nil(t, db.keys[r.key(keyLog)], "the failed transaction must not append its log")
		require.Nil(t, db.keys[r.key(keyDone)], "the failed transaction must not record its caller result")
		require.Nil(t, db.keys[r.key(keyInbox)], "the failed transaction must not publish its note")
		require.Nil(t, db.keys[r.key(keyNotes)], "the failed transaction must not store its note")
		require.Nil(t, db.keys[r.key(keyOpen)], "the failed transaction must not open its judgment")
		require.Nil(t, db.keys[r.key(keyQueue)], "the failed transaction must not change its queue")
		require.NotNil(t, db.keys[r.key(keyFence)], "the failed transaction must leave its operation fenced")
	})
}
