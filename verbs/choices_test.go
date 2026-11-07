package verbs

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestChoicesAreDurableAndInactive(t *testing.T) {
	t.Parallel()
	f := store.NewFake()
	ctx := context.Background()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "policy", Epoch: 1, Actor: "rowan", Kind: "policy", At: at, Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "default", Body: "script"}}})
	applyTest(t, f, store.Op{ID: "activate", Epoch: 1, Actor: "rowan", Kind: "activate", At: at, Writes: []store.Write{store.SetActivePolicy{Version: 1}}})
	c := Choices{Store: f, Actor: "rowan", Now: func() time.Time { return at }}
	if err := c.Save(ctx, "dashboard", "tailnet"); err != nil {
		t.Fatal(err)
	}
	value, found, err := c.Get(ctx, "dashboard")
	if err != nil || !found || value != "tailnet" {
		t.Fatal(value, found, err)
	}
	if err = c.Save(ctx, "dashboard", "tailnet"); err != nil {
		t.Fatal(err)
	}
	if err = c.Save(ctx, "secret-key", "bad"); err == nil {
		t.Fatal("stored a secret")
	}
	r, err := f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "rowan", Kind: "clear", At: at})
	if err != nil || !r.Accepted {
		t.Fatal(r, err)
	}
	value, found, err = c.Get(ctx, "dashboard")
	if err != nil || !found || value != "tailnet" {
		t.Fatal(value, found, err)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Policy != 1 {
		t.Fatal(snap.Policy)
	}
}

// An explicit flag is the operator answering again: it wins over the saved
// answer and is saved as a newer version; the same flag saves nothing.
func TestChoicesExplicitFlagAnswersAgain(t *testing.T) {
	t.Parallel()
	f := store.NewFake()
	ctx := context.Background()
	at := time.Unix(1000, 0)
	c := Choices{Store: f, Actor: "rowan", Now: func() time.Time { return at }}
	if err := c.Save(ctx, "dashboard", "public=old=1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	value, stored, err := c.Effective(ctx, "dashboard", "")
	if err != nil || !stored || value != "public=old=1.2.3.4" {
		t.Fatal(value, stored, err)
	}
	value, stored, err = c.Effective(ctx, "dashboard", "tailnet")
	if err != nil || stored || value != "tailnet" {
		t.Fatal(value, stored, err)
	}
	if err = c.Save(ctx, "dashboard", "tailnet"); err != nil {
		t.Fatal(err)
	}
	if value, found, err := c.Get(ctx, "dashboard"); err != nil || !found || value != "tailnet" {
		t.Fatal(value, found, err)
	}
	before, _ := f.Snapshot(ctx)
	value, stored, err = c.Effective(ctx, "dashboard", "tailnet")
	if err != nil || !stored || value != "tailnet" {
		t.Fatal(value, stored, err)
	}
	if err = c.Save(ctx, "dashboard", "tailnet"); err != nil {
		t.Fatal(err)
	}
	if after, _ := f.Snapshot(ctx); after.Seq != before.Seq {
		t.Fatal("the same answer wrote a new version", before.Seq, after.Seq)
	}
}

type brokenChoiceHistory struct {
	store.Store
	events []store.Event
}

func (b brokenChoiceHistory) Events(context.Context, int64, int) ([]store.Event, error) {
	return b.events, nil
}
func TestChoicesRefusesGappedOrNonProgressingHistory(t *testing.T) {
	t.Parallel()
	for _, events := range [][]store.Event{{{Seq: 2}}, {{Seq: 3}}} {
		c := Choices{Store: brokenChoiceHistory{Store: store.NewFake(), events: events}}
		if _, _, err := c.getAt(context.Background(), "setup:services", store.Snapshot{Seq: 2}); err == nil {
			t.Fatal("accepted incomplete history", events)
		}
	}
}
