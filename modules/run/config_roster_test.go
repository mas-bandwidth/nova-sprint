package run

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	config "github.com/mas-bandwidth/nova-tools/v2/modules/config"
)

type fakeConfigSnapshot struct {
	calls  int
	closed bool
	err    error
}

// noFriends is a sprint store holding no friend settings.
type noFriends struct{}

func (noFriends) NewestData(context.Context, string) ([]store.PolicyVersion, error) { return nil, nil }

// friendData is a sprint store holding vs, or failing with err.
type friendData struct {
	vs  []store.PolicyVersion
	err error
}

func (f friendData) NewestData(_ context.Context, prefix string) ([]store.PolicyVersion, error) {
	if prefix != "friend:" {
		return nil, errors.New("wrong prefix " + prefix)
	}
	return f.vs, f.err
}

func TestConfigRosterSerializesConcurrentReads(t *testing.T) {
	t.Parallel()
	f := &fakeConfigSnapshot{}
	r, err := NewConfigRoster(f, noFriends{}, 8)
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
	t.Parallel()
	f := &fakeConfigSnapshot{}
	r, err := NewConfigRoster(f, noFriends{}, 8)
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

// pricedSnapshot is one friend, one priced model and one capped provider.
type pricedSnapshot struct{}

func (pricedSnapshot) Snapshot(context.Context) (config.Snapshot, error) {
	in, out := 10, 20
	return config.Snapshot{
		Friends:   []config.Friend{{Name: "stella", Harness: "claude", Enabled: true}},
		Models:    []config.Model{{Name: "deepseek/deepseek-chat", PriceIn: &in, PriceOut: &out}},
		Providers: []config.Provider{{Name: "deepseek", Concurrency: 3, Enabled: true}},
	}, nil
}
func (pricedSnapshot) Close() {}

// TestConfigRosterReadsFriendSettingsPricesAndCaps: one read gives each
// friend its sprint settings, and the roster carries the model records'
// prices and the provider records' caps; a store that cannot be read fails
// the read, never defaults every friend.
func TestConfigRosterReadsFriendSettingsPricesAndCaps(t *testing.T) {
	t.Parallel()
	r, err := NewConfigRoster(pricedSnapshot{}, friendData{vs: []store.PolicyVersion{{Version: 5, Kind: "data", Name: "friend:stella", Body: `{"width":3,"tiers":["pro"]}`}}}, 8)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadRoster(context.Background())
	if err != nil || got.Kin["stella"].Width != 3 || got.Prices["deepseek/deepseek-chat"].Out != 20 || got.Concurrency["deepseek"] != 3 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	r, err = NewConfigRoster(pricedSnapshot{}, friendData{err: errors.New("store: connection refused")}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadRoster(context.Background()); err == nil || err.Error() != "store: connection refused" {
		t.Fatalf("store failure: %v", err)
	}
	if _, err := NewConfigRoster(pricedSnapshot{}, nil, 8); err == nil {
		t.Fatal("no friend reader accepted")
	}
}
