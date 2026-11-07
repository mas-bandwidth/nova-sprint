package verbs

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestStreamingFoldMatchesStoreRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "p", Epoch: 1, Actor: "x", Kind: "policy", At: at, Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "p", Body: "body"}}})
	applyTest(t, f, store.Op{ID: "a", Epoch: 1, Actor: "x", Kind: "activate", At: at, Writes: []store.Write{store.SetActivePolicy{Version: 1}}})
	applyTest(t, f, store.Op{ID: "s", Epoch: 1, Actor: "x", Kind: "stream", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "repo", Base: "main", Pos: 1}}})
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
