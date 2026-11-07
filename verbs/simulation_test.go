package verbs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestSimulationCheckUpAndClear(t *testing.T) {
	f := store.NewFake()
	var preview string
	e, err := Simulation(f, "rowan", "dev+abcdef123456", "v1.6.29", func() time.Time { return time.Unix(1000, 0) }, func(_ context.Context, text string) error { preview = text; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got := e.Execute(ctx, []string{"check"}); got.Exit != 0 {
		t.Fatal(got)
	}
	if got := e.Execute(ctx, []string{"up"}); got.Exit != 0 || !strings.Contains(got.Output, "UP DONE run on simulation") {
		t.Fatal(got)
	}
	got := e.Execute(ctx, []string{"clear", "--confirm", "1"})
	if got.Exit != 0 || !strings.HasPrefix(preview, "CLEAR epoch=1") {
		t.Fatal(got, preview)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Epoch != 2 {
		t.Fatal(snap.Epoch)
	}
}
