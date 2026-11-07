package verbs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/docs"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestHookPlaybookRoleChangeAndColdUnhook(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Now: func() time.Time { return at }, Roles: SnapshotRoles{Store: f}}
	got := e.Execute(ctx, []string{"hook", "--as", "alice", "--role", "coordinator"})
	text, _ := docs.Playbook("coordinator")
	if got.Exit != 0 || !strings.HasPrefix(got.Output, text) {
		t.Fatal(got.Exit)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Rows["alice"].Role != "coordinator" || snap.Rows["alice"].Session == "" {
		t.Fatal(snap.Rows["alice"])
	}
	got = e.Execute(ctx, []string{"hook", "--as", "alice", "--role", "worker"})
	if got.Exit != 1 || !strings.Contains(got.Output, "unhook --as alice") {
		t.Fatal(got)
	}
	e.Env = nil
	if got = e.Execute(ctx, []string{"unhook", "--as", "alice"}); got.Exit != 0 {
		t.Fatal(got)
	}
	snap, _ = f.Snapshot(ctx)
	if snap.Rows["alice"].Session != "" || snap.Rows["alice"].Role != "" {
		t.Fatal(snap.Rows["alice"])
	}
}
func TestUnhookOtherRequiresCoordinator(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "seed-hooks", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "alice", Session: "a", Role: "worker"}, store.PutRow{Name: "bob", Session: "b", Role: "worker"}, store.PutRow{Name: "co", Session: "c", Role: "coordinator"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Now: func() time.Time { return at }}
	got := e.Execute(ctx, []string{"unhook", "bob", "--as", "alice"})
	if got.Exit != 1 || !strings.Contains(got.Output, "--role coordinator") {
		t.Fatal(got)
	}
	got = e.Execute(ctx, []string{"unhook", "bob", "--as", "co"})
	if got.Exit != 0 {
		t.Fatal(got)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Rows["bob"].Session != "" || snap.Rows["co"].Role != "coordinator" {
		t.Fatal(snap.Rows)
	}
}

func TestEnsureCoordinatorHookDoesNotPromoteWorker(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "worker-hook", Epoch: 1, Actor: "test", Kind: "hook", At: at, Writes: []store.Write{store.PutRow{Name: "alice", Session: "fixture", Role: "worker"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Now: func() time.Time { return at }}
	got := e.EnsureCoordinatorHook(ctx, SetupInput{Actor: "alice"})
	if got.Exit != 1 || !strings.Contains(got.Output, "unhook --as alice") {
		t.Fatal(got)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Rows["alice"].Role != "worker" {
		t.Fatal(snap.Rows["alice"])
	}
}

func TestHookRefusalsNameBusRecipeAndRoles(t *testing.T) {
	env := ExecEnvironment{Getenv: func(string) string { return "" }, Command: func(context.Context, string, ...string) ([]byte, error) { return []byte("WHO OK present=0\n"), nil }}
	e := Engine{Store: store.NewFake(), Env: env, Now: func() time.Time { return time.Unix(1000, 0) }}
	got := e.Execute(context.Background(), []string{"hook", "--as", "alice", "--role", "worker"})
	want := "SPRINT REFUSED hook: hook in to the bus first: run NOVA_BUS_REDIS=<addr> nova-bus hook --as alice as the Monitor tool command, then hook in to the sprint again\n"
	if got.Exit != 1 || got.Output != want {
		t.Fatal(got)
	}
	got = e.Execute(context.Background(), []string{"hook", "--as", "alice", "--role", "other"})
	if got.Exit != 1 || !strings.Contains(got.Output, "unknown role other: choose coordinator or worker") {
		t.Fatal(got)
	}
	if got := unhookCommand("bob", "alice"); got != "nova-sprint unhook bob --as alice" {
		t.Fatal(got)
	}
}

func TestHookUsesCodexPresenceRemedy(t *testing.T) {
	env := ExecEnvironment{Getenv: func(key string) string {
		if key == "CODEX_THREAD_ID" {
			return "synthetic"
		}
		return ""
	}, Command: func(context.Context, string, ...string) ([]byte, error) { return []byte("WHO OK present=0\n"), nil }}
	e := Engine{Store: store.NewFake(), Env: env, Now: func() time.Time { return time.Unix(1000, 0) }}
	got := e.Execute(context.Background(), []string{"hook", "--as", "alice", "--role", "worker"})
	if got.Exit != 1 || !strings.Contains(got.Output, "nova-bus show codex-agents") {
		t.Fatal(got)
	}
}
