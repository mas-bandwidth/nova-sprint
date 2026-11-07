package verbs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestStopStartPersistsAndNeedsCoordinator(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "hook", Epoch: 1, Actor: "test", Kind: "hook", At: at, Writes: []store.Write{store.PutRow{Name: "rowan", Session: "fixture", Role: "coordinator"}, store.PutRow{Name: "worker", Session: "fixture2", Role: "worker"}}})
	e := Engine{Actor: "rowan", Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Now: func() time.Time { return at }}
	if got := e.Execute(ctx, []string{"stop", "--reason", "maintenance"}); got.Exit != 0 {
		t.Fatal(got)
	}
	snap, _ := f.Snapshot(ctx)
	if !snap.Stopped || snap.StopReason != "maintenance" {
		t.Fatal(snap.Stopped, snap.StopReason)
	}
	if got := e.Execute(ctx, []string{"start", "--as", "worker"}); got.Exit != 1 || !strings.Contains(got.Output, "--role coordinator") {
		t.Fatal(got)
	}
	if got := e.Execute(ctx, []string{"start"}); got.Exit != 0 {
		t.Fatal(got)
	}
	snap, _ = f.Snapshot(ctx)
	if snap.Stopped {
		t.Fatal("still stopped")
	}
}
