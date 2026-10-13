package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// heldShared takes a snapshot before blocking its first read. This puts a stale
// bus reply on the far side of a local proof and cache invalidation.
type heldShared struct {
	mu      sync.Mutex
	rec     sprint.HookShared
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	first   bool
}

func (h *heldShared) Shared(context.Context, string) (sprint.HookShared, bool, error) {
	h.mu.Lock()
	rec := h.rec
	block := h.first
	h.first = false
	h.mu.Unlock()
	if block {
		close(h.entered)
		<-h.release
	}
	return rec, true, nil
}

func (*heldShared) Arm(context.Context, string, time.Time) error    { panic("unexpected Arm") }
func (*heldShared) Prove(context.Context, string, time.Time) error  { panic("unexpected Prove") }
func (*heldShared) Unhook(context.Context, string, time.Time) error { panic("unexpected Unhook") }

func (h *heldShared) set(rec sprint.HookShared) {
	h.mu.Lock()
	h.rec = rec
	h.mu.Unlock()
}

func (h *heldShared) releaseRead() { h.once.Do(func() { close(h.release) }) }

func TestHookCacheForgetCannotRestoreAnExpiredRead(t *testing.T) {
	t.Parallel()
	const name = "hook-cache-epoch"
	now := time.Now()
	old := sprint.HookShared{Identity: name, Role: sprint.HookRoleCoordinator, Pushes: []string{sprint.HookPushBus}, ArmedAt: now.Add(-2 * sprint.HookSharedBound), ProvedAt: now.Add(-2 * sprint.HookSharedBound)}
	fresh := sprint.HookShared{Identity: name, Role: sprint.HookRoleCoordinator, Pushes: []string{sprint.HookPushBus}, ArmedAt: now, ProvedAt: now}
	h := &heldShared{rec: old, entered: make(chan struct{}), release: make(chan struct{}), first: true}
	t.Cleanup(h.releaseRead)
	a := &app{hk: &hookerBox{cur: h}}
	first := make(chan error, 1)
	go func() { _, err := a.hookerShared(context.Background(), name); first <- err }()
	<-h.entered
	h.set(fresh)
	a.hookerForget(name)
	got, err := a.hookerShared(context.Background(), name)
	require.NoError(t, err)
	require.True(t, got.PushLive(sprint.HookPushBus, now))
	h.releaseRead()
	require.NoError(t, <-first)
	got, err = a.hookerShared(context.Background(), name)
	require.NoError(t, err)
	require.True(t, got.PushLive(sprint.HookPushBus, now), "the old read restored an expired proof after the new proof")
}

func TestHookCacheRefillCannotRestoreAnExpiredRead(t *testing.T) {
	t.Parallel()
	const name = "hook-refill-epoch"
	now := time.Now()
	old := sprint.HookShared{Identity: name, Role: sprint.HookRoleCoordinator, Pushes: []string{sprint.HookPushBus}, ArmedAt: now.Add(-2 * sprint.HookSharedBound), ProvedAt: now.Add(-2 * sprint.HookSharedBound)}
	fresh := sprint.HookShared{Identity: name, Role: sprint.HookRoleCoordinator, Pushes: []string{sprint.HookPushBus}, ArmedAt: now, ProvedAt: now}
	h := &heldShared{rec: old, entered: make(chan struct{}), release: make(chan struct{}), first: true}
	t.Cleanup(h.releaseRead)
	a := &app{hk: &hookerBox{cur: h, cache: map[string]hookCopy{name: {rec: old, at: now}}}}
	done := make(chan struct{})
	go func() { a.hookerRefill(context.Background()); close(done) }()
	<-h.entered
	h.set(fresh)
	a.hookerForget(name)
	got, err := a.hookerShared(context.Background(), name)
	require.NoError(t, err)
	require.True(t, got.PushLive(sprint.HookPushBus, now))
	h.releaseRead()
	<-done
	got, err = a.hookerShared(context.Background(), name)
	require.NoError(t, err)
	require.True(t, got.PushLive(sprint.HookPushBus, now), "the old refill restored an expired proof after the new proof")
}
