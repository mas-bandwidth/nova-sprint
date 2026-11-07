package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/run/filestore"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestFunctionalFakeBackupPreservesAcceptedAndRefusedReceipts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fake.json")
	s, err := filestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	op := store.Op{ID: "accepted", Epoch: 1, Actor: "rowan", Kind: "test", At: time.Unix(1000, 0), Writes: []store.Write{store.PutStream{Name: "s"}}}
	if r, err := s.Apply(ctx, op); err != nil || !r.Accepted {
		t.Fatalf("accepted: %+v %v", r, err)
	}
	op.ID = "refused"
	op.Expect = []store.Expect{store.AtSeq{Seq: 0}}
	if r, err := s.Apply(ctx, op); err != nil || r.Accepted {
		t.Fatalf("refused: %+v %v", r, err)
	}
	a := fakeArchive{source: s, directory: fakeBackupDir(path)}
	info, err := a.Backup(ctx, "")
	if err != nil || info.Seq != 2 || info.Epoch != 1 {
		t.Fatalf("backup: %+v %v", info, err)
	}
	backup, err := filestore.Open(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"accepted", "refused"} {
		if _, found, err := backup.Replay(ctx, id); err != nil || !found {
			t.Fatalf("replay %s found=%v err=%v", id, found, err)
		}
	}
	op.ID = "later"
	op.Expect = nil
	if _, err = s.Apply(ctx, op); err != nil {
		t.Fatal(err)
	}
	snap, err := backup.Snapshot(ctx)
	if err != nil || snap.Seq != 2 {
		t.Fatalf("backup changed: %+v %v", snap, err)
	}
	if _, err = a.Verify(ctx, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing backup verified")
	}
}
