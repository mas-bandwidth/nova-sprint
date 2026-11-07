package verbs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestMachineTakeUsesImmutableReceiptMetadata(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	applyTest(t, f, store.Op{ID: "stream-settings", Epoch: 2, Actor: "test", Kind: "setup", At: at, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "stream:s", Body: `{"repo":"owner/repo","base":"main","pr":true,"mode":"auto"}`}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: fixtureRoster{}, Actor: "worker", Now: func() time.Time { return at }}
	id := consumerOpID("take", 2, "p.w1", 0)
	f.Drop(id, true) // The accepted take is recovered through its exact receipt.
	args := []string{"take", "p.w1@0", "--as", "worker", "--epoch", "2", "--id", id, "--json"}
	got := e.Execute(ctx, args)
	if got.Exit != 0 {
		t.Fatal(got)
	}
	var data machineTake
	if err := json.Unmarshal([]byte(got.Output), &data); err != nil {
		t.Fatal(err)
	}
	if data.Consumer != "p.w1" || data.Gen != 1 || data.Epoch != 2 || data.Repo != "owner/repo" || data.Base != "main" || !data.RequirePR || data.StreamRef != "sprint/s/group" {
		t.Fatal(data)
	}
	applyTest(t, f, store.Op{ID: "stream-changed", Epoch: 2, Actor: "test", Kind: "change", At: time.Unix(1001, 0), Writes: []store.Write{store.PutStream{Name: "s", Repo: "other/repo", Base: "newbase", Pos: 1}}})
	if again := e.Execute(ctx, args); again != got {
		t.Fatal(got, again)
	}
}
func TestMachineTakeRejectsWrongIDAndUnversionedReceipt(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: fixtureRoster{}, Actor: "worker", Now: func() time.Time { return at }}
	before, _ := f.Snapshot(ctx)
	got := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2", "--id", "wrong", "--json"})
	after, _ := f.Snapshot(ctx)
	if got.Exit != 1 || !strings.Contains(got.Output, "op id must be consumer:") || before.Seq != after.Seq {
		t.Fatal(got)
	}
	if _, ok := parseTakeNote("take:v1:{\"version\":1,\"repo\":\"x\",\"base\":\"main\"} trailing"); ok {
		t.Fatal("accepted malformed note")
	}
}
