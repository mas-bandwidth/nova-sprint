package dash

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// startupRetryRecorder fails the first Snapshot and records the deadlines of the next Snapshot and of Events.
type startupRetryRecorder struct {
	store.Store
	failFirst                       bool
	snapshotDeadline, eventDeadline time.Time
}

func (s *startupRetryRecorder) Snapshot(ctx context.Context) (store.Snapshot, error) {
	if s.failFirst {
		s.failFirst = false
		return store.Snapshot{}, errors.New("temporarily unavailable")
	}
	var ok bool
	s.snapshotDeadline, ok = ctx.Deadline()
	if !ok {
		return store.Snapshot{}, errors.New("snapshot has no deadline")
	}
	return s.Store.Snapshot(ctx)
}

func (s *startupRetryRecorder) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	var ok bool
	s.eventDeadline, ok = ctx.Deadline()
	if !ok {
		return nil, errors.New("event read has no deadline")
	}
	return s.Store.Events(ctx, after, max)
}

// TestStartupRetryDeadlineExceedsEventRead checks that the startup retry's Snapshot gets the longer
// startup deadline (at least 10 s of budget) and that Events gets a shorter one. It reads the deadlines and
// waits for nothing.
func TestStartupRetryDeadlineExceedsEventRead(t *testing.T) {
	if startupSnapshotDeadline != 30*time.Second {
		t.Fatal("startup deadline changed")
	}
	f := store.NewFake()
	s := &startupRetryRecorder{Store: f, failFirst: true}
	c, err := NewRecovering(context.Background(), s)
	if err == nil || c == nil {
		t.Fatal("expected initial failure")
	}
	if err = c.Advance(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.snapshotDeadline.IsZero() || s.eventDeadline.IsZero() || !s.snapshotDeadline.After(s.eventDeadline) {
		t.Fatal("startup retry did not receive a longer deadline than the event read")
	}
	// Inspect the available context budget; this does not wait or measure latency.
	if left := time.Until(s.snapshotDeadline); left < 10*time.Second {
		t.Fatalf("startup retry budget %s is below 10 seconds", left)
	}
}

// TestStartupRetryRespectsCancellation checks that an Advance with a cancelled parent context returns
// context.Canceled.
func TestStartupRetryRespectsCancellation(t *testing.T) {
	f := store.NewFake()
	s := &startupRetryRecorder{Store: f, failFirst: true}
	c, err := NewRecovering(context.Background(), s)
	if err == nil {
		t.Fatal("expected initial failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Advance(ctx, s); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled parent, got %v", err)
	}
}

// unavailableSnapshot fails Snapshot while unavailable is set.
type unavailableSnapshot struct {
	store.Store
	unavailable bool
}

func (s *unavailableSnapshot) Snapshot(ctx context.Context) (store.Snapshot, error) {
	if s.unavailable {
		return store.Snapshot{}, errors.New("store unavailable")
	}
	return s.Store.Snapshot(ctx)
}

// TestStartupRecovers checks that a dashboard whose startup Snapshot fails serves an unavailable cache
// (/healthz 503), keeps failing while the store is down, and recovers once it returns.
func TestStartupRecovers(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro"})
	s := &unavailableSnapshot{Store: f, unavailable: true}
	c, err := NewRecovering(ctx, s)
	if err == nil || c == nil {
		t.Fatal("expected unavailable cache and startup error")
	}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 || c.pageSnapshot().OK {
		t.Fatalf("unavailable cache health=%d", rec.Code)
	}
	if err := c.Advance(ctx, s); err == nil {
		t.Fatal("unavailable poll succeeded")
	}
	s.unavailable = false
	if err := c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	d := c.pageSnapshot().Data
	if rec.Code != 200 || !d.OK || d.All != 1 {
		t.Fatalf("recovered health=%d data=%+v", rec.Code, d)
	}
}
