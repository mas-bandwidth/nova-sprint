package dash

import (
	"bytes"
	"context"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestUnknownProducerStateCountsAsWaitingAndAlarms checks that a producer in a state the board does not
// know counts as waiting, keeps the sprint from being done, and logs an ALARM naming the card and state.
func TestUnknownProducerStateCountsAsWaitingAndAlarms(t *testing.T) { // unitlint: serial, it captures the process's log output (log.SetOutput)
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
	d := c.pageSnapshot().Data
	w := d.Tables["work"].(map[string]*workRow)["s"]
	if d.All != 2 || d.Done || w.Waiting != 1 || w.Landed != 1 {
		t.Fatalf("all=%d done=%v work=%+v", d.All, d.Done, w)
	}
	if text := logs.String(); !strings.Contains(text, "ALARM") || !strings.Contains(text, `"p2"`) || !strings.Contains(text, `"done"`) {
		t.Fatalf("missing card/state alarm: %q", text)
	}
}

// TestStaleViewServes200WhileHealthzFails503 checks that after 30 seconds of failed polls /healthz
// returns 503 while /api/sprint keeps serving the last good view.
func TestStaleViewServes200WhileHealthzFails503(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}

	// 1. Stale alarm triggers 503 on /healthz after 30s of failed polls while
	// /api/sprint continues serving the last good view with stale: true.
	ts := &tapStore{Store: f, fail: true}
	now = now.Add(35 * time.Second)
	_ = c.Advance(ctx, ts)
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 {
		t.Fatalf("expected 503 on stale /healthz, got %d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec2, httptest.NewRequest("GET", "/api/sprint", nil))
	if rec2.Code != 200 {
		t.Fatalf("expected 200 on stale /api/sprint, got %d", rec2.Code)
	}
}
