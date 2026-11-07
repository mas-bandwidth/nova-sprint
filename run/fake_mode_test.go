package run

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestFunctionalFakeChooserIsExclusiveAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	ctx := context.Background()
	first, err := OpenFakeChooser(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if second, err := OpenFakeChooser(ctx, path); err == nil {
		_ = second.Close(ctx)
		t.Fatal("second chooser acquired")
	}
	if err = first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := OpenFakeChooser(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Close(ctx)
}
func TestFakeSenderPrintsWithoutBus(t *testing.T) {
	var out bytes.Buffer
	s := &FakeSender{Out: &out}
	receipt, err := s.Send(context.Background(), "alice", "DEAL", "work")
	if err != nil || receipt.Recipient != "alice" || receipt.ID == "" || !strings.Contains(out.String(), "PUSH to=alice subject=DEAL\nwork") {
		t.Fatalf("receipt=%+v output=%q err=%v", receipt, out.String(), err)
	}
}
