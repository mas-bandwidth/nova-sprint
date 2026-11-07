package dash

// Bounded adaptations of two probes by Rowan's second breaker (rowan2),
// origin/v2-break-rowan2 at 113e1fc07a1d87b9474a51d30d55ecb27702e064.
// They use immediate fakes and compare context deadlines, with no sleeps.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

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
func TestFunctionalContextRowan2StartupRetryDeadline(t *testing.T) {
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
	// Inspect the available context budget, as in Rowan's probe; this does not wait or measure latency.
	if left := time.Until(s.snapshotDeadline); left < 10*time.Second {
		t.Fatalf("startup retry budget %s is below the probe's 10-second store response", left)
	}
}
func TestRowan2StartupRetryRespectsCancellation(t *testing.T) {
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
func TestRowan2UnknownProducerCountsAndAlarms(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	mustApply(t, f, "seed", 1, store.PutStream{Name: "s"}, store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro"}, store.PutCard{ID: "p2", Kind: "producer", Stream: "s", Tier: "pro"})
	mustApply(t, f, "move", 1, store.SetCardState{Card: "p1", State: "landed"}, store.SetCardState{Card: "p2", State: "done"})
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	d := c.pageSnapshot().Data.(envelope)
	w := d.Tables["work"].(map[string]*workRow)["s"]
	if d.All != 2 || d.Done || w.Waiting != 1 || w.Landed != 1 {
		t.Fatalf("all=%d done=%v work=%+v", d.All, d.Done, w)
	}
	if text := logs.String(); !strings.Contains(text, "ALARM") || !strings.Contains(text, `"p2"`) || !strings.Contains(text, `"done"`) {
		t.Fatalf("missing card/state alarm: %q", text)
	}
}
