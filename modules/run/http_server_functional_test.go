package run

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFunctionalDashboardServerStartsAndStops(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- (HTTPServer{Address: "127.0.0.1:0", Ready: ready}).Serve(ctx, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	}()
	addr := <-ready
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + addr)
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "ok", string(b))
	cancel()
	require.NoError(t, <-done)
}
