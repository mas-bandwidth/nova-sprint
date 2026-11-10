package verbs

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// movingStore is a store a running sprint moves: before the first add op
// reaches it, the op in between lands, as a tick, a take or a landing does
// while add reads the repository and the store.
type movingStore struct {
	*store.Fake
	between store.Op
	moved   bool
}

func (m *movingStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "add" && !m.moved {
		m.moved = true
		if r, err := m.Fake.Apply(ctx, m.between); err != nil || !r.Accepted {
			return r, err
		}
	}
	return m.Fake.Apply(ctx, op)
}

// TestAddCommitsOnAMovingStore is add on a busy store (2026-11-10: "the
// store is at seq N, expected seq M", eleven refusals running): an op that
// touches none of the add's records lands between add's read and its
// commit, and the add still commits; one that puts the same card id is
// refused, as the add's own fence on that card says.
func TestAddCommitsOnAMovingStore(t *testing.T) {
	t.Parallel()
	t.Run("an unrelated op lands between", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		w.e.Store = &movingStore{Fake: w.f, between: store.Op{ID: "beat", Epoch: 1, Actor: "alice", Kind: "tick", At: t0,
			Writes: []store.Write{store.SetRowFlaps{Row: "alice", Flaps: 1}}}}
		want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a1", "p1")), 0, "ADDED 1 cards")
		if c := w.snap(t).Cards["a1"]; c.Kind != "producer" || len(c.Needs) != 1 {
			t.Fatalf("card a1 %+v", c)
		}
	})
	t.Run("the same card lands between", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		w.e.Store = &movingStore{Fake: w.f, between: store.Op{ID: "other-add", Epoch: 1, Actor: "carol", Kind: "add", At: t0,
			Writes: []store.Write{store.PutCard{ID: "a1", Kind: "sentinel", Stream: "s", Tier: "flash", Needs: []string{"p1"}}}}}
		want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a1", "")), 1, "a1")
		if c := w.snap(t).Cards["a1"]; c.Kind != "sentinel" {
			t.Fatalf("card a1 %+v, want the sentinel that landed first", c)
		}
	})
}
