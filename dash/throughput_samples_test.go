package dash

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestThroughputReportedAfterLandings checks that the throughput figure is a rate once cards have landed
// over more than the ten-minute warmup.
func TestThroughputReportedAfterLandings(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	now := c.at
	c.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		mustApply(t, f, fmt.Sprintf("land%d", i), 1, store.SetCardState{Card: fmt.Sprintf("p%05d", i), State: "landed"})
		now = now.Add(3 * time.Minute)
		if err = c.Advance(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var env PageEnvelope
	if err = json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Throughput <= 0 {
		t.Errorf("throughput is zero after 5 landings over 15 minutes")
	}
}

// TestThroughputWarmupRestartsAfterClear checks that a clear restarts the ten-minute throughput warmup
// even when the landed count does not fall across it (0 landed before, 1 after).
func TestThroughputWarmupRestartsAfterClear(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	mustApply(t, f, "e1", 1, store.PutStream{Name: "s1"})

	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return now }
	c.samples = []sample{{now, 0}}

	// 11 minutes pass in Epoch 1 with 0 landed cards.
	now = now.Add(11 * time.Minute)
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}

	// Clear opens Epoch 2, and 1 card lands in Epoch 2 at 11m10s.
	now = now.Add(10 * time.Second)
	if _, err := f.Clear(ctx, store.Op{ID: "clear-2", Epoch: 1, Actor: "coord", Kind: "clear", At: now}); err != nil {
		t.Fatal(err)
	}
	mustApply(t, f, "e2-seed", 2,
		store.PutStream{Name: "s1"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"},
	)
	mustApply(t, f, "e2-land", 2,
		store.SetCardState{Card: "p1", State: "landed"},
	)
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}

	// Epoch 2 is only 10 seconds old (< 10-minute warmup), so throughput rate must be 0!
	if rate, minutes := c.rate(); rate != 0 {
		t.Fatalf("10s into epoch 2 after a clear, throughput rate = %v cards/hr (window=%vm), want 0 (the warmup restarts)", rate, minutes)
	}
}
