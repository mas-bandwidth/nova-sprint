package dash

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type startupBlockingStore struct {
	store.Store
	deadline time.Duration
}

func (s *startupBlockingStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	if d, ok := ctx.Deadline(); ok {
		s.deadline = time.Until(d)
	}
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
	if s.deadline < 29*time.Second || s.deadline > 30*time.Second {
		t.Fatalf("deadline = %s", s.deadline)
	}
}
