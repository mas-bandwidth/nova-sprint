package dash

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicDashboardRealHTTPAndShutdown(t *testing.T) {
	t.Parallel()
	var pulls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pulls.Add(1)
		if r.URL.Path != "/api/sprint" {
			t.Errorf("upstream path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"ok":true,"data":{"at":"1970-01-01T00:16:40Z","tables":{"fleet":{},"friends":{},"merge":{},"readers":{},"work":{}},"streams":[],"epoch":1},"dataAgeSeconds":0}`)
	}))
	defer upstream.Close()
	cache, err := NewPullCache(upstream.URL, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Advance(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveDashboardListener(ctx, listener, cache.Handler(), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	}()
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 20; i++ {
		resp, err := client.Get("http://" + listener.Addr().String() + "/api/sprint")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}
	resp, err := client.Get("http://" + listener.Addr().String() + "/internal/run-status")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("private route exposed: %d", resp.StatusCode)
	}
	if pulls.Load() != 1 {
		t.Fatalf("viewer requests triggered %d upstream reads", pulls.Load())
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get("http://" + listener.Addr().String() + "/api/sprint"); err == nil {
		t.Fatal("server remained reachable")
	}
}
func TestPullCommandRejectsConflictingStoreFlag(t *testing.T) {
	if err := Command(context.Background(), []string{"--pull", "http://example.test", "--pg", "secret"}); err == nil {
		t.Fatal("expected conflicting flags refusal")
	}
}
