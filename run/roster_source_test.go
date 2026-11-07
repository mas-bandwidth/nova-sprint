package run

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/roster"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

func TestConfigRosterSourceSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	cores := 12
	snap := config.Snapshot{Machines: []config.Machine{{Name: "bench-a", Roles: "bench,runner", Cores: &cores, OS: "linux", Arch: "amd64"}, {Name: "service", Roles: "services"}}, Friends: []config.Friend{{Name: "friend", Enabled: true, Width: 3, Tiers: "flash,pro", Model: "m", Harness: "h", Pronouns: "she/her"}, {Name: "disabled", Enabled: false, Width: 1, Tiers: "pro"}}}
	src := ConfigRosterSource(snap)
	cores = 2
	snap.Machines[0].Name = "changed"
	snap.Friends[0].Tiers = "heavy"
	machines, err := src.Machines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	friends, err := src.Friends(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if machines[0].Name != "bench-a" || machines[0].Cores != 12 || !reflect.DeepEqual(machines[0].Roles, []string{"bench", "runner"}) || friends[0].Tiers[0] != "flash" {
		t.Fatal(machines, friends)
	}
	machines[0].Roles[0] = "changed"
	friends[0].Tiers[0] = "changed"
	r, err := roster.Load(ctx, src, 8)
	if err != nil {
		t.Fatal(err)
	}
	if r.Workers["bench-a"].Width != 8 || r.Workers["bench-a"].Harness != roster.WorkerHarness || r.Kin["friend"].Width != 3 || r.Kin["friend"].Model != "m" || len(r.Kin) != 1 {
		t.Fatal(r)
	}
}

func TestConfigRosterSourceUnknownCoresAndCancellation(t *testing.T) {
	src := ConfigRosterSource(config.Snapshot{Machines: []config.Machine{{Name: "bench", Roles: "bench"}}})
	if _, err := roster.Load(context.Background(), src, 8); err == nil {
		t.Fatal("unknown cores accepted")
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
