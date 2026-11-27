package run

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	config "github.com/mas-bandwidth/nova-tools/v2/modules/config"
)

func TestConfigRosterSourceSnapshotIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cores := 12
	snap := config.Snapshot{Machines: []config.Machine{{Name: "bench-a", Roles: "bench,runner", Routes: "ipad,usb", Cores: &cores, OS: "linux", Arch: "amd64"}, {Name: "service", Roles: "services"}}, Friends: []config.Friend{{Name: "friend", Enabled: true, Model: "m", Harness: "h", Pronouns: "she/her"}, {Name: "disabled", Enabled: false}}}
	friends := map[string]roster.FriendRecord{"friend": {Setting: roster.FriendSetting{Width: 3, Tiers: []string{"flash", "pro"}}, Version: 4}}
	src := ConfigRosterSource(snap, friends)
	cores = 2
	snap.Machines[0].Name = "changed"
	friends["friend"].Setting.Tiers[0] = "heavy"
	machines, err := src.Machines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := src.Friends(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if machines[0].Name != "bench-a" || machines[0].Cores != 12 || !reflect.DeepEqual(machines[0].Roles, []string{"bench", "runner"}) || !reflect.DeepEqual(machines[0].Routes, []string{"ipad", "usb"}) || fs[0].Tiers[0] != "flash" {
		t.Fatal(machines, fs)
	}
	machines[0].Roles[0] = "changed"
	fs[0].Tiers[0] = "changed"
	r, err := roster.Load(ctx, src, 8)
	if err != nil {
		t.Fatal(err)
	}
	if r.Workers["bench-a"].Width != 8 || r.Workers["bench-a"].Harness != roster.WorkerHarness || !reflect.DeepEqual(r.Workers["bench-a"].Routes, []string{"ipad", "usb"}) || r.Kin["friend"].Width != 3 || r.Kin["friend"].Model != "m" || len(r.Kin) != 1 {
		t.Fatal(r)
	}
}

// TestConfigRosterSourceFriendSettings: a friend's width and tiers are its
// sprint settings; a friend with none recorded works at width 1 on every
// tier; one whose record cannot be read is refused, never defaulted.
func TestConfigRosterSourceFriendSettings(t *testing.T) {
	t.Parallel()
	snap := config.Snapshot{Friends: []config.Friend{{Name: "set", Harness: "claude", Enabled: true}, {Name: "none", Harness: "codex", Enabled: true}, {Name: "bad", Harness: "claude", Enabled: true}}}
	recs := roster.FriendSettings([]store.PolicyVersion{
		{Version: 2, Kind: "data", Name: "friend:set", Body: `{"width":2,"tiers":["pro","heavy"]}`},
		{Version: 3, Kind: "data", Name: "friend:bad", Body: `{"width":0,"tiers":["pro"]}`},
	})
	r, err := roster.Load(context.Background(), ConfigRosterSource(snap, recs), 8)
	if err != nil {
		t.Fatal(err)
	}
	if k := r.Kin["set"]; k.Width != 2 || !reflect.DeepEqual(k.Tiers, []string{"pro", "heavy"}) {
		t.Fatalf("recorded: %+v", k)
	}
	if k := r.Kin["none"]; k.Width != 1 || !reflect.DeepEqual(k.Tiers, store.Tiers) {
		t.Fatalf("defaults: %+v", k)
	}
	want := roster.Refused{Name: "bad", Kind: roster.KindKin, Reason: "friend bad's sprint settings cannot be read: policy v3 (friend:bad): width 0 is below 1: a friend works at least one card at once"}
	if _, ok := r.Kin["bad"]; ok || !reflect.DeepEqual(r.Refused, []roster.Refused{want}) {
		t.Fatalf("unreadable: %+v", r.Refused)
	}
}

// TestConfigPricesAndConcurrency: a model record prices its model only with
// both prices set; a provider caps runs only when enabled with a cap.
func TestConfigPricesAndConcurrency(t *testing.T) {
	t.Parallel()
	in, out, cached := 270000, 1100000, 6000
	snap := config.Snapshot{
		Models: []config.Model{{Name: "deepseek/deepseek-chat", PriceIn: &in, PriceOut: &out}, {Name: "deepseek/half", PriceIn: &in},
			{Name: "deepseek/flash", PriceIn: &in, PriceCacheRead: &cached, PriceOut: &out}, {Name: "ollama/qwen"}},
		Providers: []config.Provider{{Name: "deepseek", Concurrency: 4, Enabled: true}, {Name: "off", Concurrency: 2}, {Name: "open", Enabled: true}},
	}
	want := map[string]roster.Price{"deepseek/deepseek-chat": {In: 270000, CacheRead: 270000, Out: 1100000}, "deepseek/flash": {In: 270000, CacheRead: 6000, Out: 1100000}}
	if got := ConfigPrices(snap); !reflect.DeepEqual(got, want) {
		t.Fatalf("prices %v", got)
	}
	if got := ConfigConcurrency(snap); !reflect.DeepEqual(got, map[string]int{"deepseek": 4}) {
		t.Fatalf("concurrency %v", got)
	}
}

func TestConfigRosterSourceUnknownCoresAndCancellation(t *testing.T) {
	t.Parallel()
	src := ConfigRosterSource(config.Snapshot{Machines: []config.Machine{{Name: "bench", Roles: "bench"}}}, nil)
	if r, err := roster.Load(context.Background(), src, 8); err != nil || len(r.Refused) != 0 || r.Workers["bench"].Width != 0 || r.Workers["bench"].Held != roster.NoCores {
		t.Fatalf("unknown cores: want bench held at width 0, got %+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Machines(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := src.Friends(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
