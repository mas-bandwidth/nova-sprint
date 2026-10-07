package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldQueue is the forge's merge queues as land asks them, and no forge: the branches whose
// queue holds a group, every branch's from the ask numbered from (0: never), and how many
// asks were made.
type heldQueue struct {
	mu    sync.Mutex
	held  map[string]bool
	from  int
	asked int
}

func (q *heldQueue) HoldsGroup(_ context.Context, _, branch string) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.asked++
	return q.held[branch] || q.from > 0 && q.asked >= q.from, nil
}

func (q *heldQueue) hold(branch string, held bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.held[branch] = held
}

// countingQueue answers held for one branch and counts the asks; an error when set.
type countingQueue struct {
	asks int
	err  error
}

func (c *countingQueue) HoldsGroup(_ context.Context, _, branch string) (bool, error) {
	c.asks++
	return branch == "dev", c.err
}

// The forge is asked a branch's queue once per mergeQueueKeep, its answer (an error too)
// kept by repository and branch, so the land loop's rounds do not ask it every round.
func TestKeptQueueAsksTheForgeOncePerKeep(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 4, 18, 0, 0, 0, time.UTC)
	ask := &countingQueue{err: errors.New("down")}
	k := &keptQueue{ask: ask, now: func() time.Time { return now }, kept: map[string]keptAnswer{}}
	ctx := context.Background()
	for range 3 {
		held, err := k.HoldsGroup(ctx, "r", "dev")
		assert.True(t, held)
		assert.EqualError(t, err, "down")
	}
	assert.Equal(t, 1, ask.asks, "the answer is kept")
	ask.err = nil
	held, err := k.HoldsGroup(ctx, "r", "main")
	require.NoError(t, err)
	assert.False(t, held)
	assert.Equal(t, 2, ask.asks, "another branch is its own answer")
	now = now.Add(mergeQueueKeep)
	held, err = k.HoldsGroup(ctx, "r", "dev")
	require.NoError(t, err, "a kept answer ends at mergeQueueKeep")
	assert.True(t, held)
	assert.Equal(t, 3, ask.asks)
}

// Only the address of a repository on the forge's host names a merge queue gh can ask: any
// other (a path, a bare clone, another host) asks nothing and has none.
func TestForgeRepoReadsOnlyAnAddressOnTheForge(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ repo, owner, name string }{
		{"https://forge.test/o/r.git", "o", "r"},
		{"https://forge.test/o/r", "o", "r"},
		{"https://forge.test/o/r/", "o", "r"},
		{"git@forge.test:o/r.git", "o", "r"},
		{"ssh://git@forge.test/o/r.git", "o", "r"},
		{"/srv/git/r.git", "", ""},
		{"", "", ""},
		{"https://example.com/o/r.git", "", ""},
		{"https://forge.test/o", "", ""},
		{"https://forge.test/o/r/tree/main", "", ""},
	} {
		t.Run(c.repo, func(t *testing.T) {
			t.Parallel()
			owner, name, ok := forgeRepo("forge.test", c.repo)
			assert.Equal(t, c.owner != "", ok)
			if ok {
				assert.Equal(t, [2]string{c.owner, c.name}, [2]string{owner, name})
			}
		})
	}
	held, err := ghMergeQueue{host: "forge.test"}.HoldsGroup(context.Background(), "/srv/git/r.git", "main")
	require.NoError(t, err, "a repository on no forge asks nothing")
	assert.False(t, held)
}
