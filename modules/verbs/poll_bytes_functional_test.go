package verbs

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// countingProxy forwards a PostgreSQL socket in its own directory to the one
// in dir and counts the bytes the server sends back: what a client pays.
type countingProxy struct {
	dir  string
	down atomic.Int64
}

// countingWriter adds every byte written through it to n.
type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c countingWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n.Add(int64(n))
	return n, err
}

func startCountingProxy(t *testing.T, dir string) *countingProxy {
	t.Helper()
	p := &countingProxy{}
	var err error
	if p.dir, err = os.MkdirTemp("", "nsp-"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(p.dir) }) // ignored: a temp dir
	l, err := net.Listen("unix", filepath.Join(p.dir, ".s.PGSQL.5432"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() { _ = l.Close(); wg.Wait() }) // ignored: closing the test's listener
	wg.Go(func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			s, err := net.Dial("unix", filepath.Join(dir, ".s.PGSQL.5432"))
			if err != nil {
				_ = c.Close() // ignored: the server is gone
				continue
			}
			wg.Go(func() { _, _ = io.Copy(s, c); _ = s.Close() })                          // ignored: a closed side ends the copy
			wg.Go(func() { _, _ = io.Copy(countingWriter{c, &p.down}, s); _ = c.Close() }) // ignored: a closed side ends the copy
		}
	})
	return p
}

// TestFunctionalIdlePollBytes: the worker's idle poll (next --json every
// WakeEvery, beat every BeatEvery, each its own process and connection) read
// every judgement of the epoch, closed ones too, on every poll; on
// 2026-11-10 the studio sent each bench 3-5 MB a poll, 41-73 MB a minute.
// An idle poll's bytes do not grow with the epoch's closed judgements, and
// the coordinator's next still lists the open one.
func TestFunctionalIdlePollBytes(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir, _ := startBackupPG(ctx, t)
	proxy := startCountingProxy(t, dir)
	pg, err := store.Open(ctx, "host="+proxy.dir+" dbname=postgres sslmode=disable", "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	apply := func(op store.Op) {
		t.Helper()
		if r, err := pg.Apply(ctx, op); err != nil || !r.Accepted {
			t.Fatal(r, err)
		}
	}
	at := time.Unix(1000, 0)
	apply(store.Op{ID: "seed", Epoch: 1, Actor: "carol", Kind: "seed", At: at, Writes: []store.Write{
		store.PutStream{Name: "s", Repo: "o/r", Base: "main", Pos: 1}, store.PutRow{Name: "carol", Session: "s-carol", Role: "coordinator"},
		store.PutRow{Name: "alice", Session: "s-alice", Role: "worker"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "task"},
		store.SetIdentity(testIdentity), store.PutJudgement{ID: "open.1", Kind: "ask", About: "p", Question: "still open", Command: "nova-sprint judge open.1"}}})
	closed := 0
	closeMore := func(n int) { // n judgements opened and answered, 2 KB of question each
		t.Helper()
		for range n {
			closed++
			id := fmt.Sprintf("old.%d", closed)
			apply(store.Op{ID: "open." + id, Epoch: 1, Actor: "carol", Kind: "judgement", At: at, Writes: []store.Write{
				store.PutJudgement{ID: id, Kind: "ask", About: "p", Question: strings.Repeat("q", 2048), Command: "nova-sprint judge " + id}}})
			apply(store.Op{ID: "answer." + id, Epoch: 1, Actor: "carol", Kind: "judgement", At: at, Writes: []store.Write{
				store.AnswerJudgement{ID: id, Answer: "yes"}}})
		}
	}
	w := newWorld(t)
	w.e.Store = pg
	poll := func() (next, beat int64) {
		t.Helper()
		before := proxy.down.Load()
		want(t, w.do("alice", "next", "--json"), 0, `"row":"alice"`)
		mid := proxy.down.Load()
		want(t, w.do("alice", "beat", "--load1", "0.5", "--cores", "8", "--slots-used", "0", "--slots-total", "4"), 0, "BEAT alice")
		return mid - before, proxy.down.Load() - mid
	}
	poll() // the first beat writes; every poll after it is idle
	closeMore(50)
	next1, beat1 := poll()
	closeMore(200) // 400 KB more of closed judgements
	next2, beat2 := poll()
	t.Logf("idle poll bytes with %d and %d closed judgements: next %d -> %d, beat %d -> %d", 50, closed, next1, next2, beat1, beat2)
	if grew := next2 - next1; grew > 4096 {
		t.Errorf("an idle next --json read %d bytes more after 200 more closed judgements: it reads closed judgements", grew)
	}
	if grew := beat2 - beat1; grew > 4096 {
		t.Errorf("an idle beat read %d bytes more after 200 more closed judgements: it reads closed judgements", grew)
	}
	want(t, w.do("carol", "next", "--json"), 0, `"id":"open.1"`)
}
