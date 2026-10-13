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

func TestHookCacheForgetCannotRestoreAnExpiredRead(t *testing.T) {
	t.Parallel()
	const name = "hook-cache-epoch"
	now := time.Now()
	old := sprint.HookShared{Identity: name, Role: sprint.HookRoleCoordinator, Pushes: []string{sprint.HookPushBus}, ArmedAt: now.Add(-2 * sprint.HookSharedBound), ProvedAt: now.Add(-2 * sprint.HookSharedBound)}
	fresh := sprint.HookShared{Identity: name, Role: sprint.HookRoleCoordinator, Pushes: []string{sprint.HookPushBus}, ArmedAt: now, ProvedAt: now}
	h := &heldShared{rec: old, entered: make(chan struct{}), release: make(chan struct{}), first: true}
	a := &app{hk: &hookerBox{cur: h}}
	first := make(chan sprint.HookShared, 1)
	go func() { rec, _ := a.hookerShared(context.Background(), name); first <- rec }()
	<-h.entered
	h.set(fresh)
	a.hookerForget(name)
	got, err := a.hookerShared(context.Background(), name)
	require.NoError(t, err)
	require.True(t, got.PushLive(sprint.HookPushBus, now))
	close(h.release)
	<-first
	got, err = a.hookerShared(context.Background(), name)
	require.NoError(t, err)
	require.True(t, got.PushLive(sprint.HookPushBus, now), "the old read restored an expired proof after the new proof")
}
