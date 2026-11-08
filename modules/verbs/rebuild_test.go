package verbs

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestStreamingFoldMatchesStoreRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "p", Epoch: 1, Actor: "x", Kind: "policy", At: at, Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "p", Body: "body"}}})
	applyTest(t, f, store.Op{ID: "a", Epoch: 1, Actor: "x", Kind: "activate", At: at, Writes: []store.Write{store.SetActivePolicy{Version: 1}}})
	applyTest(t, f, store.Op{ID: "s", Epoch: 1, Actor: "x", Kind: "stream", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "repo", Base: "main", Pos: 1}}})
	applyTest(t, f, store.Op{ID: "go", Epoch: 1, Actor: "x", Kind: "start", At: at, Writes: []store.Write{store.SetStopped{Stopped: false, Reason: "go"}}})
	applyTest(t, f, store.Op{ID: "halt", Epoch: 1, Actor: "x", Kind: "stop", At: at, Writes: []store.Write{store.SetStopped{Stopped: true, Reason: "pause"}}})
	r, err := f.Clear(ctx, store.Op{ID: "c", Epoch: 1, Actor: "x", Kind: "clear", At: at})
	if err != nil || !r.Accepted {
		t.Fatal(r, err)
	}
	applyTest(t, f, store.Op{ID: "d", Epoch: 2, Actor: "x", Kind: "data", At: at, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "setup:dashboard", Body: "tailnet"}}})
	events, err := f.Events(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	fold := newEventFold()
	for _, event := range events {
		fold.add(event)
	}
	if got, want := fold.result(), store.Rebuild(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if snap, err := f.Snapshot(ctx); err != nil || !reflect.DeepEqual(fold.result(), snap) || snap.StopReason != "pause" {
		t.Fatalf("the fold of the whole log is not the snapshot (a backup's check): %+v %v", snap, err)
	}
	if got := newEventFold().result(); !got.Stopped || got.StopReason != store.NeverStarted {
		t.Fatalf("an empty log folds to %+v, want a fresh store, stopped, never started", got)
	}
}

func TestEventPrefixDigestStableAfterMoreEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "one", Epoch: 1, Actor: "x", Kind: "policy", At: at, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "x", Body: "one"}}})
	first, err := EventPrefixDigest(ctx, f, 1)
	if err != nil {
		t.Fatal(err)
	}
	applyTest(t, f, store.Op{ID: "two", Epoch: 1, Actor: "x", Kind: "policy", At: at, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "y", Body: "two"}}})
	again, err := EventPrefixDigest(ctx, f, 1)
	if err != nil || again != first {
		t.Fatal(first, again, err)
	}
	longer, err := EventPrefixDigest(ctx, f, 2)
	if err != nil || longer == first {
		t.Fatal(first, longer, err)
	}
}

// TestBackupFoldKeepsACutModel: the backup's verification folds the log as
// the store does. A card cut with a model and reported by a worker binary
// from before the model column (its after-image has none) keeps the cut's
// model, as the table does (store.Fold); v2.0.8's backup refused the live
// store on exactly that ("rebuild does not match snapshot").
func TestBackupFoldKeepsACutModel(t *testing.T) {
	t.Parallel()
	cut := store.Card{ID: "p.w4", Kind: "work", Of: "p", Stream: "s", State: "working", Holder: "amd-2", Model: "inception/mercury-3"}
	done := cut
	done.State, done.Model, done.Gen = "done", "", 1
	events := []store.Event{
		{Seq: 1, Accepted: true, Kind: "tick", After: &store.State{Cards: map[string]store.Card{cut.ID: cut}}},
		{Seq: 2, Accepted: true, Kind: "report", After: &store.State{Cards: map[string]store.Card{cut.ID: done}}},
	}
	fold := newEventFold()
	for _, e := range events {
		fold.add(e)
	}
	if got, want := fold.result(), store.Rebuild(events); !reflect.DeepEqual(got, want) || got.Cards[cut.ID].Model != cut.Model {
		t.Fatalf("the backup's fold %+v, the store's %+v", got.Cards, want.Cards)
	}
}

// TestSnapshotDifferenceNamesTheCardAndField: a mismatch names what differs.
func TestSnapshotDifferenceNamesTheCardAndField(t *testing.T) {
	t.Parallel()
	a := store.Snapshot{Seq: 2, Cards: map[string]store.Card{"p.w4": {ID: "p.w4", State: "done", Model: "m"}, "p": {ID: "p"}}}
	b := store.Snapshot{Seq: 2, Cards: map[string]store.Card{"p.w4": {ID: "p.w4", State: "done"}, "p": {ID: "p"}}}
	got := snapshotDifference(a, b)
	for _, want := range []string{"card p.w4", "Model", `"m"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q does not name %s", got, want)
		}
	}
	if d := snapshotDifference(a, a); d != "" {
		t.Fatalf("equal snapshots differ: %q", d)
	}
	b.Seq = 3
	b.Cards = a.Cards
	if d := snapshotDifference(a, b); !strings.Contains(d, "Seq") {
		t.Fatalf("a top-level difference: %q", d)
	}
}
