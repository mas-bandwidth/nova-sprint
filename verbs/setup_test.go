package verbs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// upStore is a store with rowan hooked in as coordinator, or with no rows.
func upStore(hooked bool) *store.Fake {
	rows := map[string]store.Row{}
	if hooked {
		rows["rowan"] = store.Row{Name: "rowan", State: "up", Session: "s-rowan", Role: "coordinator", Gen: 1}
	}
	f, err := store.NewFakeFrom(store.Snapshot{Epoch: 1, Rows: rows, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Judgements: map[string]store.Judgement{}, Cursors: map[string]int64{}})
	if err != nil {
		panic(err)
	}
	return f
}

func upBus() *fakeBus  { return &fakeBus{present: map[string]bool{"rowan": true}} }
func upNow() time.Time { return t0 }

type setupLeaseFake struct{}

func (setupLeaseFake) Acquire(context.Context) (SetupLease, error) { return setupLeaseFake{}, nil }
func (setupLeaseFake) Monitor(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
func (setupLeaseFake) Check(context.Context) error { return nil }
func (setupLeaseFake) Close() error                { return nil }

type setupFake struct {
	ready [9]bool
	trace []string
	stop  int
}

func (f *setupFake) Completion(context.Context) (string, error) {
	return "UP DONE run on studio; dashboard http://studio; 4 workers up; coordinator rowan", nil
}
func (f *setupFake) Verify(_ context.Context, n int, _ SetupInput) (SetupCheck, error) {
	f.trace = append(f.trace, fmt.Sprintf("verify%d", n))
	if n == f.stop && !f.ready[n] {
		return SetupCheck{Line: "ASK dashboard", Next: "nova-sprint up --dash tailnet"}, nil
	}
	return SetupCheck{Verified: f.ready[n], Action: !f.ready[n], Line: fmt.Sprintf("UP %d/8", n)}, nil
}
func (f *setupFake) Act(_ context.Context, n int, _ SetupInput) error {
	f.trace = append(f.trace, fmt.Sprintf("act%d", n))
	f.ready[n] = true
	return nil
}
func TestSetupVerifiesActionsAndRestarts(t *testing.T) {
	f := &setupFake{stop: 5}
	e := Engine{Actor: "rowan", Store: upStore(true), Bus: upBus(), Now: upNow, Setup: f, Lock: setupLeaseFake{}}
	first := e.Execute(context.Background(), []string{"up"})
	if first.Exit != 1 || !strings.Contains(first.Output, "ASK dashboard\nSPRINT REFUSED up: step 5 is not verified: nova-sprint up --dash tailnet\n") {
		t.Fatal(first)
	}
	if strings.Join(f.trace, ",") != "verify1,act1,verify1,verify1,verify2,act2,verify2,verify3,act3,verify3,verify4,act4,verify4,verify5" {
		t.Fatal(f.trace)
	}
	f.trace = nil
	f.stop = 0
	f.ready[2] = false
	second := e.Execute(context.Background(), []string{"up", "--dash", "tailnet"})
	if second.Exit != 0 || !strings.HasPrefix(strings.Join(f.trace, ","), "verify1,verify1,verify2,act2,verify2") {
		t.Fatal(second, f.trace)
	}
	// UP DONE tells a new coordinator to read check's width lines before adding cards
	if !strings.Contains(second.Output, "coordinator rowan\nBEFORE CARDS: keep everybody at width: ") || !strings.Contains(second.Output, "then nova-sprint check and read its width, waits and NEEDS chain lines before start") {
		t.Fatal(second.Output)
	}
}
func TestSetupArguments(t *testing.T) {
	e := Engine{Actor: "rowan"}
	got := e.Execute(context.Background(), []string{"up", "--dash", "invalid"})
	if got.Exit != 2 || got.Output != "SPRINT REFUSED up: --dash is tailnet or public=<machine>[:<port>]: nova-sprint help up\n" {
		t.Fatal(got)
	}
}
