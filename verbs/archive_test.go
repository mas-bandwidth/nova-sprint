package verbs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type archiveFake struct {
	calls []string
	info  BackupInfo
	empty bool
	fail  string
}

func (f *archiveFake) step(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == name {
		return errors.New("failed")
	}
	return nil
}
func (f *archiveFake) Backup(context.Context, string) (BackupInfo, error) {
	return f.info, f.step("backup")
}
func (f *archiveFake) AcquireRestore(context.Context) (RestoreLease, error) { return f, f.step("lock") }
func (f *archiveFake) Empty(context.Context) (bool, error)                  { return f.empty, f.step("empty") }
func (f *archiveFake) Verify(context.Context, string) (BackupInfo, error) {
	return f.info, f.step("verify")
}
func (f *archiveFake) Restore(context.Context, *VerifiedDump) error   { return f.step("restore") }
func (f *archiveFake) VerifyTarget(context.Context, BackupInfo) error { return f.step("target") }
func (f *archiveFake) Check(context.Context) error                    { return f.step("check") }
func (f *archiveFake) Close() error                                   { return f.step("close") }
func TestRestoreOrdering(t *testing.T) {
	a := &archiveFake{empty: true, info: BackupInfo{Verified: &VerifiedDump{}}}
	e := Engine{Archive: a}
	got := e.Execute(context.Background(), []string{"restore", "backup.dump"})
	if got.Exit != 0 {
		t.Fatal(got)
	}
	want := []string{"lock", "verify", "check", "restore", "check", "close"}
	if !reflect.DeepEqual(a.calls, want) {
		t.Fatal(a.calls)
	}
	a.calls = nil
	a.fail = "restore"
	got = e.Execute(context.Background(), []string{"restore", "backup.dump"})
	if got.Exit != 2 || !reflect.DeepEqual(a.calls, []string{"lock", "verify", "check", "restore", "close"}) {
		t.Fatal(got, a.calls)
	}
}
func TestClearRequiresMatchingVerifiedBackup(t *testing.T) {
	f := store.NewFake()
	a := &archiveFake{info: BackupInfo{Epoch: 1, Seq: 1, Path: "x"}}
	e := Engine{Store: f, Archive: a, Env: fakeEnv{hooked: true}, Preview: func(context.Context, string) error { return nil }, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }}
	r := e.Execute(context.Background(), []string{"clear", "--confirm", "1"})
	if r.Exit != 1 {
		t.Fatal(r)
	}
	snap, _ := f.Snapshot(context.Background())
	if snap.Epoch != 1 {
		t.Fatal(snap.Epoch)
	}
	a.info.Seq = 0
	r = e.Execute(context.Background(), []string{"clear", "--confirm", "1"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	snap, _ = f.Snapshot(context.Background())
	if snap.Epoch != 2 {
		t.Fatal(snap.Epoch)
	}
}

func TestPostgresSubprocessEnvironmentIsAllowlisted(t *testing.T) {
	source := []string{"PATH=/bin", "HOME=/tmp/home", "NOVA_ADMIN_PASSWORD=synthetic", "NOVA_API_KEY=synthetic", "PGPASSWORD=unrelated"}
	env, err := pgProcessEnv(source, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(env, []string{"PATH=/bin", "HOME=/tmp/home"}) {
		t.Fatal(env)
	}
	env, err = pgProcessEnv(source, "host=example user=backup password=dedicated dbname=sprint sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "synthetic") || strings.Contains(joined, "unrelated") || !strings.Contains(joined, "PGPASSWORD=dedicated") {
		t.Fatal(env)
	}
}
