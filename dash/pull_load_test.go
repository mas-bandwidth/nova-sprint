package dash

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestFunctionalPublicDashboardThousandRequests drives a pull board with a
// thousand loopback requests, each client from its own source address, and
// checks that viewers cause no extra upstream pulls. It runs only with
// NOVA_DASH_LOAD=1.
func TestFunctionalPublicDashboardThousandRequests(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_DASH_LOAD") != "1" {
		t.Skip("set NOVA_DASH_LOAD=1 for the bounded HTTP load")
	}
	var pulls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pulls.Add(1)
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
	server := httptest.NewServer(cache.Handler())
	defer server.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	start := time.Now()
	for i := 1; i <= 40; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs <- loadPublicClient(server.URL, i) }(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	t.Logf("1000 real HTTP requests in %s; upstream reads=%d", elapsed, pulls.Load())
	if pulls.Load() != 1 {
		t.Fatalf("upstream reads=%d", pulls.Load())
	}
}
func loadPublicClient(url string, n int) error {
	dial := &net.Dialer{Timeout: 5 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(fmt.Sprintf("127.0.0.%d", n))}}
	transport := &http.Transport{DialContext: dial.DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for i := 0; i < 25; i++ {
		resp, err := client.Get(url + "/api/sprint")
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
	}
	return nil
}
