package dash

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type startupBlockingStore struct {
	store.Store
	deadline bool
}

func (s *startupBlockingStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	_, s.deadline = ctx.Deadline()
	<-ctx.Done()
	return store.Snapshot{}, ctx.Err()
}
func TestStartupSnapshotDeadline(t *testing.T) {
	s := &startupBlockingStore{Store: store.NewFake()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(ctx, s)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "startup snapshot") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.deadline {
		t.Fatal("startup snapshot had no deadline")
	}
}
