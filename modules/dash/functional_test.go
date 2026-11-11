package dash

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/pgbin"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func startPG(t *testing.T) string {
	t.Helper()
	// pg_ctl stops it with -m immediate, never a signal.
	return pgbin.Start(t, pgbin.Options{Stop: "immediate"}).DSN
}
func TestFunctionalJSONGolden(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	ctx := context.Background()
	var s *store.PG
	var err error
	for i := 0; i < 100; i++ {
		s, err = store.Open(ctx, dsn, "")
		if err == nil {
			err = s.Migrate(ctx)
			if err == nil {
				break
			}
			s.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := apply(t, s, "seed", 1, store.PutStream{Name: "alpha"}, store.PutRow{Name: "w"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "alpha"})
	if !r.Accepted {
		t.Fatal(r.Reason)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFromFeed(Feed{Snap: snap, Roster: testRoster, At: at, Landings: []time.Time{}}, func() time.Time { return at })
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var body any
	if err = json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	body.(map[string]any)["dataAgeSeconds"] = 0
	got, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err = os.WriteFile("testdata/seed.json", got, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/seed.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
