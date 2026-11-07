package verbs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type barrierRoster struct {
	mu    sync.Mutex
	calls int
	ready chan struct{}
}

func (b *barrierRoster) ReadRoster(ctx context.Context) (roster.Roster, error) {
	b.mu.Lock()
	b.calls++
	if b.calls == 2 {
		close(b.ready)
	}
	b.mu.Unlock()
	select {
	case <-b.ready:
	case <-ctx.Done():
		return roster.Roster{}, ctx.Err()
	}
	return roster.Roster{Workers: map[string]roster.Worker{"w": {Name: "w", Width: 1, Tiers: []string{"pro"}}}}, nil
}
func TestConcurrentTakesRespectRosterWidth(t *testing.T) {
	ctx := context.Background()
	at := time.Unix(1000, 0)
	f := store.NewFake()
	applyTest(t, f, store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutRow{Name: "w", Session: "fixture", Role: "worker"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "task"}, store.PutCard{ID: "p.w1", Kind: "work", Of: "p", Stream: "s", Tier: "pro", Holder: "w", Brief: "task"}, store.PutCard{ID: "p.w2", Kind: "work", Of: "p", Stream: "s", Tier: "pro", Holder: "w", Brief: "task"}}})
	applyTest(t, f, store.Op{ID: "ready", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.SetRowState{Row: "w", State: "up"}, store.SetCardState{Card: "p.w1", State: "ready"}, store.SetCardState{Card: "p.w2", State: "ready"}}})
	r := &barrierRoster{ready: make(chan struct{})}
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: r, Actor: "w", Now: func() time.Time { return at }}
	var wg sync.WaitGroup
	results := make([]Result, 2)
	for i, id := range []string{"p.w1@1", "p.w2@1"} {
		wg.Add(1)
		go func(i int, id string) { defer wg.Done(); results[i] = e.Execute(ctx, []string{"take", id}) }(i, id)
	}
	wg.Wait()
	count := 0
	for _, x := range results {
		if x.Exit == 0 {
			count++
		}
	}
	if count != 1 {
		t.Fatal(results)
	}
	snap, _ := f.Snapshot(ctx)
	working := 0
	for _, c := range snap.Cards {
		if c.Holder == "w" && c.State == "working" {
			working++
		}
	}
	if working != 1 {
		t.Fatal(snap.Cards)
	}
}
