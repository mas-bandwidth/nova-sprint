package run

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// setClock is a clock whose now the test sets.
type setClock struct{ now time.Time }

func (c *setClock) Now() time.Time           { return c.now }
func (c *setClock) Every(time.Duration) Beat { return nil }

// The run loop reconciles on its first beat, then once an hour, with the
// snapshot it holds; a failure is logged and waits for the next hour.
func TestLoopReconcilesHourly(t *testing.T) {
	f, _ := seeded(t)
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	clk := &setClock{now: t0}
	var log bytes.Buffer
	var at []time.Time
	fail := false
	l := &loop{ticker: tk, cfg: Config{Clock: clk, Log: &log, Reconcile: func(_ context.Context, now time.Time, snap store.Snapshot) error {
		if snap.Seq != tk.Snapshot().Seq {
			t.Fatalf("snapshot seq %d", snap.Seq)
		}
		at = append(at, now)
		if fail {
			return errors.New("deepseek: 503")
		}
		return nil
	}}}
	for _, d := range []time.Duration{0, time.Second, 59 * time.Minute, time.Hour, time.Hour + time.Minute, 2*time.Hour + time.Second} {
		clk.now = t0.Add(d)
		fail = d > time.Hour
		l.reconcile(context.Background())
	}
	want := []time.Time{t0, t0.Add(time.Hour), t0.Add(2*time.Hour + time.Second)}
	if len(at) != len(want) {
		t.Fatalf("reconciled at %v, want %v", at, want)
	}
	for i := range want {
		if !at[i].Equal(want[i]) {
			t.Fatalf("reconciled at %v, want %v", at, want)
		}
	}
	if got := log.String(); got != "COST reconcile failed: deepseek: 503\n" {
		t.Fatalf("log %q", got)
	}
	(&loop{cfg: Config{Clock: clk}}).reconcile(context.Background()) // no hook: nothing
}
