package verbs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestNextMachineIncludesRosterAndAllConsumerStates(t *testing.T) {
	snap := store.Snapshot{Epoch: 3, Seq: 9, Rows: map[string]store.Row{"worker": {Name: "worker", Session: "s", Role: "worker", State: "up"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Priority: "high", Score: 8},
		"r": {ID: "r", Of: "p", Holder: "worker", Kind: "read", State: "ready", Gen: 2},
		"w": {ID: "w", Of: "p", Holder: "worker", Kind: "work", State: "working", Gen: 3},
		"f": {ID: "f", Of: "p", Holder: "worker", Kind: "work", State: "done", Gen: 4},
	}}
	e := Engine{Store: fakeReader{snap}, Env: fakeEnv{green: true, schema: true, hooked: true}, Roles: testRoles{role: "worker"}, Roster: fixtureRoster{}, Version: "v2.0.0", Actor: "worker"}
	got := e.Execute(context.Background(), []string{"next", "--json"})
	if got.Exit != 0 {
		t.Fatal(got)
	}
	var out machineNext
	if err := json.Unmarshal([]byte(got.Output), &out); err != nil {
		t.Fatal(err)
	}
	if out.Epoch != 3 || out.Seq != 9 || out.Width != 2 || out.Row != "worker" || out.Model != "sol" || out.Version != "v2.0.0" || len(out.Ready) != 1 || out.Ready[0].ID != "r" || len(out.Working) != 1 || len(out.Final) != 1 || len(out.Obligations) != 2 {
		t.Fatalf("%+v", out)
	}
}

func TestNextMachineAbsentRosterKeepsRecoveryInventory(t *testing.T) {
	snap := store.Snapshot{Epoch: 2, Rows: map[string]store.Row{"worker": {Name: "worker", Session: "s", Role: "worker", State: "up"}}, Cards: map[string]store.Card{
		"p":       {ID: "p", Kind: "producer"},
		"ready":   {ID: "ready", Of: "p", Holder: "worker", Kind: "work", State: "ready"},
		"working": {ID: "working", Of: "p", Holder: "worker", Kind: "work", State: "working"},
		"done":    {ID: "done", Of: "p", Holder: "worker", Kind: "read", State: "done"},
	}}
	e := Engine{Store: fakeReader{snap}, Env: fakeEnv{green: true, schema: true, hooked: true}, Roles: testRoles{role: "worker"}, Roster: emptyRoster{}, Version: "v2.0.0", Actor: "worker"}
	got := e.Execute(context.Background(), []string{"next", "--json"})
	if got.Exit != 0 {
		t.Fatal(got)
	}
	var out machineNext
	if err := json.Unmarshal([]byte(got.Output), &out); err != nil {
		t.Fatal(err)
	}
	if out.RosterPresent || out.Width != 0 || len(out.Ready) != 0 || len(out.Working) != 1 || len(out.Final) != 1 || len(out.Obligations) != 2 {
		t.Fatalf("%+v", out)
	}
}
