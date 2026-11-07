package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/store"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func op(id string, epoch int, writes ...store.Write) store.Op {
	return store.Op{ID: id, Epoch: epoch, Actor: "test", Kind: "test", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Writes: writes}
}
func open(t *testing.T, path string) *Store {
	t.Helper()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func apply(t *testing.T, s *Store, o store.Op) store.Receipt {
	t.Helper()
	r, e := s.Apply(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestReopenReplayAndClear(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "store.json")
	s := open(t, path)
	first := apply(t, s, op("policy", 1, store.PutPolicy{Kind: "data", Name: "p", Body: "{}"}, store.SetActivePolicy{Version: 1}))
	if !first.Accepted {
		t.Fatal(first)
	}
	cursor := apply(t, s, op("cursor", 1, store.SetCursor{Name: "feed", Seq: 1}))
	if !cursor.Accepted {
		t.Fatal(cursor)
	}
	refused := apply(t, s, op("refused", 2))
	if refused.Accepted {
		t.Fatal(refused)
	}
	clearOp := op("clear", 1)
	clearOp.Kind = "clear"
	clear, err := s.Clear(ctx, clearOp)
	if err != nil || !clear.Accepted {
		t.Fatalf("clear: %+v %v", clear, err)
	}
	s = open(t, path)
	for _, want := range []store.Receipt{first, cursor, refused, clear} {
		got, ok, e := s.Replay(ctx, want.Op)
		if e != nil || !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("replay %+v: %+v %v %v", want, got, ok, e)
		}
		got, e = s.Apply(ctx, op(want.Op, 999))
		if e != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("retry: %+v %v", got, e)
		}
	}
	snap, err := s.Snapshot(ctx)
	if err != nil || snap.Epoch != 2 || snap.Seq != 4 || snap.Cursors["feed"] != 1 || snap.Policy != 1 {
		t.Fatalf("snapshot %+v %v", snap, err)
	}
	events, err := s.Events(ctx, 1, 2)
	if err != nil || len(events) != 2 || events[0].Op != "cursor" || events[1].Op != "refused" {
		t.Fatalf("events %+v %v", events, err)
	}
	for _, event := range events {
		id, epoch, found, e := s.EventIdentity(ctx, event.Seq)
		if e != nil || !found || id != event.Op || epoch != event.Epoch {
			t.Fatalf("identity: %q %d %v %v, event %+v", id, epoch, found, e, event)
		}
	}
	if _, _, found, e := s.EventIdentity(ctx, 99); e != nil || found {
		t.Fatalf("missing identity: found=%v err=%v", found, e)
	}
	p, err := s.Policy(ctx, 1)
	if err != nil || !p.Active {
		t.Fatalf("policy %+v %v", p, err)
	}
	if _, err = s.Policy(ctx, 2); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	const n = 30
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, e := Open(path)
			if e == nil {
				_, e = s.Apply(context.Background(), op(fmt.Sprintf("op-%d", i), 1))
			}
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	s := open(t, path)
	snap, e := s.Snapshot(context.Background())
	if e != nil || snap.Seq != n {
		t.Fatalf("snapshot %+v %v", snap, e)
	}
	events, e := s.Events(context.Background(), 0, 0)
	if e != nil || len(events) != n {
		t.Fatalf("events %d %v", len(events), e)
	}
}

func TestCorruptionPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	s := open(t, path)
	apply(t, s, op("one", 1))
	original, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var d disk
	if e = json.Unmarshal(original, &d); e != nil {
		t.Fatal(e)
	}
	d.Snapshot.Epoch++
	mismatch, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range [][]byte{[]byte("{"), append(append([]byte(nil), original...), 'x'), mismatch} {
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Apply(context.Background(), op("two", 1)); e == nil {
			t.Fatal("corrupt state accepted")
		}
		after, e := os.ReadFile(path)
		if e != nil || !reflect.DeepEqual(after, b) {
			t.Fatalf("state changed %v", e)
		}
	}
}

func TestChooserCrashReleasesLock(t *testing.T) {
	path := os.Getenv("FILESTORE_CHILD_PATH")
	if path != "" {
		f, e := AcquireChooser(path)
		if e != nil {
			os.Exit(2)
		}
		_ = f
		os.Exit(0)
	}
	path = filepath.Join(t.TempDir(), "state")
	lease, e := AcquireChooser(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = AcquireChooser(path); e == nil {
		t.Fatal("second chooser acquired lease")
	}
	if e = lease.Close(); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestChooserCrashReleasesLock$")
	cmd.Env = append(os.Environ(), "FILESTORE_CHILD_PATH="+path)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("child: %s: %v", b, e)
	}
	lease, e = AcquireChooser(path)
	if e != nil {
		t.Fatal(e)
	}
	lease.Close()
}
