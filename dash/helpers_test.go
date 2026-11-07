package dash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// tapStore counts Snapshot calls, records every Events cursor and can fail Events.
type tapStore struct {
	store.Store
	snapshots int
	cursors   []int64
	fail      bool
}

func (s *tapStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	s.snapshots++
	return s.Store.Snapshot(ctx)
}

func (s *tapStore) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	s.cursors = append(s.cursors, after)
	if s.fail {
		return nil, errors.New("connection reset")
	}
	return s.Store.Events(ctx, after, max)
}

func mustApply(t *testing.T, s store.Store, id string, epoch int, w ...store.Write) {
	t.Helper()
	if r := apply(t, s, id, epoch, w...); !r.Accepted {
		t.Fatalf("%s refused: %s", id, r.Reason)
	}
}

// seedSprint writes n producers in 50 streams, two rows, and a work and a read consumer for every tenth producer.
func seedSprint(t *testing.T, s store.Store, n int) {
	t.Helper()
	w := []store.Write{
		store.PutRow{Name: "amd-1"},
		store.PutRow{Name: "friendG"},
	}
	for i := 0; i < 50; i++ {
		w = append(w, store.PutStream{Name: fmt.Sprintf("s%02d", i)})
	}
	for i := 0; i < n; i++ {
		w = append(w, store.PutCard{ID: fmt.Sprintf("p%05d", i), Kind: "producer", Tier: "pro", Stream: fmt.Sprintf("s%02d", i%50)})
	}
	mustApply(t, s, "seed", 1, w...)
	w = nil
	for i := 0; i < n; i += 10 {
		p, st := fmt.Sprintf("p%05d", i), fmt.Sprintf("s%02d", i%50)
		w = append(w, store.PutCard{ID: "w" + p, Kind: "work", Of: p, Stream: st, Tier: "pro", Holder: "amd-1"},
			store.PutCard{ID: "r" + p, Kind: "read", Of: p, Stream: st, Tier: "pro", Holder: "friendG"})
	}
	mustApply(t, s, "consumers", 1, w...)
}

func viewJSON(t *testing.T, s store.Snapshot) string {
	t.Helper()
	b, err := json.Marshal(mapSnapshot(s, testRoster, time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// readJSON decodes a JSON object from a file.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
