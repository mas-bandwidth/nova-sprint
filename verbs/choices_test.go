package verbs

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestChoicesAreDurableAndInactive(t *testing.T) {
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
	if err = c.Save(ctx, "dashboard", "public=host"); err == nil {
		t.Fatal("changed a durable answer")
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
