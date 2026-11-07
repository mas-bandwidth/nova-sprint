package run

import (
	"context"
	"errors"
	"sync"
	"testing"

	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

type fakeConfigSnapshot struct {
	calls  int
	closed bool
	err    error
}

func TestConfigRosterSerializesConcurrentReads(t *testing.T) {
	f := &fakeConfigSnapshot{}
	r, err := NewConfigRoster(f, 8)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.ReadRoster(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.calls != 8 {
		t.Fatalf("calls=%d", f.calls)
	}
}

func (f *fakeConfigSnapshot) Snapshot(context.Context) (config.Snapshot, error) {
	f.calls++
	cores := 12
	return config.Snapshot{Machines: []config.Machine{{Name: "bench", Roles: "bench", Cores: &cores}}}, f.err
}
func (f *fakeConfigSnapshot) Close() { f.closed = true }

func TestConfigRosterReadsOneSnapshotAndCloses(t *testing.T) {
	f := &fakeConfigSnapshot{}
	r, err := NewConfigRoster(f, 8)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadRoster(context.Background())
	if err != nil || f.calls != 1 || got.Workers["bench"].Width != 8 {
		t.Fatalf("got=%+v calls=%d err=%v", got, f.calls, err)
	}
	f.err = errors.New("offline")
	if _, err = r.ReadRoster(context.Background()); err == nil {
		t.Fatal("snapshot error ignored")
	}
	r.Close()
	if !f.closed {
		t.Fatal("connection not closed")
	}
}
