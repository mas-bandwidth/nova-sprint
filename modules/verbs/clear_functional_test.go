package verbs

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestFunctionalClearWhileWorkersBeat: the live failure of 2026-11-08. A
// worker beats every few milliseconds against a real store while clear
// makes its pg_dump backup and clears; every beat lands after the backup's
// seq, and clear still opens the new epoch, with the row carried.
func TestFunctionalClearWhileWorkersBeat(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, dsn := startBackupPG(ctx, t)
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := pg.Apply(ctx, store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: time.Unix(1000, 0), Writes: []store.Write{
		store.PutStream{Name: "s", Repo: "o/r", Base: "main", Pos: 1}, store.PutRow{Name: "carol", Session: "s-carol", Role: "coordinator"},
		store.PutRow{Name: "alice", Session: "s-alice", Role: "worker"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "task"}}})
	if err != nil || !r.Accepted {
		t.Fatal(r, err)
	}
	w := newWorld(t)
	w.e.Store = pg
	w.e.Archive = NewPGArchive(dsn, "", "v2.0.0", t.TempDir(), nil)

	beats, stop := make(chan int), make(chan struct{})
	go func() {
		n := 0
		defer func() { beats <- n }()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			op := store.Op{ID: fmt.Sprintf("beat.%d", i), Epoch: 1, Actor: "alice", Kind: "beat", At: time.Unix(1000, 0),
				Writes: []store.Write{store.SetRowLoad{Row: "alice", Load1Centi: i, Cores: 8}}}
			if r, err := pg.Apply(ctx, op); err == nil && r.Accepted {
				n++
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	res := w.do("carol", "clear", "--confirm", "1")
	close(stop)
	n := <-beats
	want(t, res, 0, "CLEARED epoch=2 opened")
	s, err := pg.Snapshot(ctx)
	if err != nil || s.Epoch != 2 || len(s.Cards) != 0 || s.Rows["alice"].Cores != 8 {
		t.Fatalf("after %d beats: %+v %v", n, s, err)
	}
	if n == 0 {
		t.Fatal("no beat landed during the clear: the test proves nothing")
	}
}
