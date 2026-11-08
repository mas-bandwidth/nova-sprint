package dash

import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func requireDashLoad(t *testing.T) {
	t.Helper()
	if os.Getenv("NOVA_LOAD") != "1" {
		t.Skip("dashboard load measurement: set NOVA_LOAD=1 on a quiet Linux bench")
	}
}

type countedStore struct {
	store.Store
	snapshots int
}

func (s *countedStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	s.snapshots++
	return s.Store.Snapshot(ctx)
}
func TestFunctionalDashboardIncremental5000(t *testing.T) {
	t.Parallel()
	requireDashLoad(t)
	ctx := context.Background()
	f := store.NewFake()
	writes := []store.Write{store.PutStream{Name: "s"}}
	for i := 0; i < 5000; i++ {
		writes = append(writes, store.PutCard{ID: fmt.Sprintf("p%05d", i), Kind: "producer", Tier: "pro", Stream: "s"})
	}
	r := apply(t, f, "bulk", 1, writes...)
	if !r.Accepted {
		t.Fatal(r.Reason)
	}
	s := &countedStore{Store: f}
	c, e := New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		r = apply(t, f, fmt.Sprintf("change-%d", i), 1, store.SetCardReason{Card: fmt.Sprintf("p%05d", i), Reason: "changed"})
		if !r.Accepted {
			t.Fatal(r.Reason)
		}
		start := time.Now()
		if e = c.Advance(ctx, s); e != nil {
			t.Fatal(e)
		}
		mid := time.Now()
		out := httptest.NewRecorder()
		Handler(c).ServeHTTP(out, httptest.NewRequest("GET", "/api/sprint", nil))
		t.Logf("trial=%d advance=%s json=%s bytes=%d", i, mid.Sub(start), time.Since(mid), out.Body.Len())
		if out.Code != 200 {
			t.Fatal(out.Code)
		}
	}
	if s.snapshots != 1 {
		t.Fatalf("Snapshot calls=%d; want one", s.snapshots)
	}
}
