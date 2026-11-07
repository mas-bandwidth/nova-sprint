package verbs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type testRoles struct {
	role string
	gen  int
	err  error
}

func (r testRoles) HookRole(context.Context, string) (string, int, error) {
	return r.role, r.gen, r.err
}
func TestRoleAuthorizationAndFence(t *testing.T) {
	ctx := context.Background()
	e := Engine{Env: fakeEnv{hooked: true}, Roles: testRoles{role: "worker", gen: 7}}
	claim, _, ok := e.authorizeRole(ctx, "alice", "worker", "take")
	if !ok {
		t.Fatal("worker refused")
	}
	op := store.Op{Actor: "alice"}
	if err := fenceRole(claim, &op); err != nil {
		t.Fatal(err)
	}
	if len(op.Expect) != 1 || op.Expect[0] != (store.RowGen{Row: "alice", Gen: 7}) {
		t.Fatal(op.Expect)
	}
	if _, result, ok := e.authorizeRole(ctx, "alice", "coordinator", "priority"); ok || result.Exit != 1 {
		t.Fatal(result)
	}
	e.Roles = testRoles{role: "coordinator", gen: 8}
	if _, _, ok := e.authorizeRole(ctx, "alice", "worker", "take"); !ok {
		t.Fatal("coordinator must be a worker")
	}
	e.Roles = testRoles{err: errors.New("offline")}
	if _, result, ok := e.authorizeRole(ctx, "alice", "worker", "take"); ok || result.Exit == 0 {
		t.Fatal(result)
	}
}
func TestRoleActorDoesNotInterpretFlagValues(t *testing.T) {
	args := []string{"drop", "a", "--reason", "--as", "--waive", "--as", "bob"}
	if got, ok := roleActor(args, "alice"); !ok || got != "bob" {
		t.Fatal(got)
	}
}

func TestRoleRejectsDuplicateActorAndMismatch(t *testing.T) {
	if _, ok := roleActor([]string{"drop", "p", "--as", "alice", "--as", "bob"}, ""); ok {
		t.Fatal("duplicate accepted")
	}
	required := context.WithValue(context.Background(), roleRequiredKey{}, true)
	if err := fenceRole(required, &store.Op{Actor: "alice"}); err == nil {
		t.Fatal("missing claim accepted")
	}
	ctx := context.WithValue(context.Background(), roleClaimKey{}, roleClaim{"alice", 2})
	op := store.Op{Actor: "bob"}
	if err := fenceRole(ctx, &op); err == nil || len(op.Expect) != 0 {
		t.Fatal(op, err)
	}
}

func TestRoleFenceRefusesChangedHook(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "role-seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "alice", Session: "fixture", Role: "coordinator"}}})
	snap, _ := f.Snapshot(ctx)
	e := Engine{Env: fakeEnv{hooked: true}, Roles: testRoles{role: "coordinator", gen: snap.Rows["alice"].Gen}}
	claim, _, ok := e.authorizeRole(ctx, "alice", "coordinator", "stream")
	if !ok {
		t.Fatal("not authorized")
	}
	applyTest(t, f, store.Op{ID: "hook-change", Epoch: 1, Actor: "test", Kind: "hook", At: at, Writes: []store.Write{store.PutRow{Name: "alice"}}})
	op := store.Op{ID: "role-fence", Epoch: 1, Actor: "alice", Kind: "stream add", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}}}
	rec, err := applyExact(claim, f, op)
	if err != nil || rec.Accepted {
		t.Fatal(rec, err)
	}
	snap, _ = f.Snapshot(ctx)
	if _, ok := snap.Streams["s"]; ok {
		t.Fatal("stale role mutated state")
	}
}

func TestClearRoleSnapshotAndAtSeqFence(t *testing.T) {
	ctx := context.Background()
	at := time.Unix(1000, 0)
	f := store.NewFake()
	applyTest(t, f, store.Op{ID: "clear-role-row", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "alice", Session: "fixture", Role: "coordinator"}}})
	snap, _ := f.Snapshot(ctx)
	a := &archiveFake{info: BackupInfo{Epoch: 1, Seq: snap.Seq, Path: "backup"}}
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: testRoles{role: "coordinator", gen: snap.Rows["alice"].Gen}, Actor: "alice", Now: func() time.Time { return at }, Archive: a, Preview: func(context.Context, string) error { return nil }}
	if got := e.Execute(ctx, []string{"clear", "--confirm", "1"}); got.Exit != 0 {
		t.Fatal(got)
	}
	snap, _ = f.Snapshot(ctx)
	if snap.Epoch != 2 {
		t.Fatal(snap.Epoch)
	}
}
func TestClearRefusesStaleRoleBeforeBackup(t *testing.T) {
	ctx := context.Background()
	at := time.Unix(1000, 0)
	f := store.NewFake()
	applyTest(t, f, store.Op{ID: "clear-role-row", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "alice", Session: "fixture", Role: "coordinator"}}})
	snap, _ := f.Snapshot(ctx)
	old := snap.Rows["alice"].Gen
	applyTest(t, f, store.Op{ID: "hook-change", Epoch: 1, Actor: "test", Kind: "hook", At: at, Writes: []store.Write{store.PutRow{Name: "alice"}}})
	a := &archiveFake{}
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: testRoles{role: "coordinator", gen: old}, Actor: "alice", Now: func() time.Time { return at }, Archive: a}
	if got := e.Execute(ctx, []string{"clear", "--confirm", "1"}); got.Exit != 1 || len(a.calls) != 0 {
		t.Fatal(got, a.calls)
	}
}

func TestNextRequiresRoleAfterPrerequisites(t *testing.T) {
	ctx := context.Background()
	e := Engine{Actor: "alice", Env: fakeEnv{green: true, schema: true, hooked: true}, Store: store.NewFake(), Roles: testRoles{}}
	got := e.Execute(ctx, []string{"next"})
	if got.Exit != 1 || !strings.Contains(got.Output, "nova-sprint hook --as alice --role worker") {
		t.Fatal(got)
	}
	e.Roles = testRoles{role: "worker"}
	got = e.Execute(ctx, []string{"next"})
	if got.Exit != 0 || !strings.Contains(got.Output, "next:") {
		t.Fatal(got)
	}
	e.Env = fakeEnv{green: true, schema: false, hooked: true}
	got = e.Execute(ctx, []string{"next"})
	if got.Exit != 1 || !strings.Contains(got.Output, "nova-sprint up --as alice") {
		t.Fatal(got)
	}
}

func TestWorkerCanReadPolicyButCannotActivate(t *testing.T) {
	_, e := policyFixture(t)
	e.Actor = "other"
	e.Roles = testRoles{role: "worker"}
	if got := e.Execute(context.Background(), []string{"policy", "show"}); got.Exit != 0 {
		t.Fatal(got)
	}
	if got := e.Execute(context.Background(), []string{"policy", "activate", "1", "--reason", "test"}); got.Exit != 1 || !strings.Contains(got.Output, "--role coordinator") {
		t.Fatal(got)
	}
}
