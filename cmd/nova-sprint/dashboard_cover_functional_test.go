//go:build functional

package main

import (
	"bytes"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveDashboard (dashboard.go:193, the finding's 0.0%) starts one listener per address,
// says each one it listens on with the address it actually bound, and a listener that
// cannot start closes the others. The addresses are loopback's ephemeral ports: the only
// sockets are the ones the test and the function itself open, and each is closed again
// before its test returns.
func TestDashboardCoverServesEachListenerAndRefusesWhenOneCannotStart(t *testing.T) {
	t.Parallel()
	t.Run("each listener starts and says the address it bound", func(t *testing.T) {
		var out bytes.Buffer
		servers, err := (&app{}).serveDashboard([]listener{
			{flag: "--listen", addr: "127.0.0.1:0", h: http.NewServeMux(), says: "DASHBOARD listening on http://%s/\n"},
			{flag: "--pull", addr: "127.0.0.1:0", h: http.NewServeMux(), says: "DASHBOARD pull routes on http://%s/\n"},
		}, &out)
		require.NoError(t, err)
		require.Len(t, servers, 2)
		t.Cleanup(func() {
			for _, s := range servers {
				// ignored: the test's servers at its end; a page asking now asks again
				_ = s.Close()
			}
		})
		assert.Equal(t, 2, strings.Count(out.String(), "\n"), out.String())
		assert.Contains(t, out.String(), "DASHBOARD listening on http://127.0.0.1:", out.String())
		assert.Contains(t, out.String(), "DASHBOARD pull routes on http://127.0.0.1:", out.String())
	})
	t.Run("a listener that cannot start closes the others and refuses", func(t *testing.T) {
		// the first address is an ephemeral port, so it starts; the second is held
		// open, so serveDashboard's second Listen fails. The listener the first one
		// was given is kept, so its closing is read off the listener itself and no
		// port is ever released and taken again (another parallel test may take it)
		held, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() {
			// ignored: the test's own listener at its end
			_ = held.Close()
		})
		var opened []net.Listener
		a := &app{dashListen: func(network, addr string) (net.Listener, error) {
			ln, err := net.Listen(network, addr)
			if err == nil {
				opened = append(opened, ln)
			}
			return ln, err
		}}
		var out bytes.Buffer
		servers, err := a.serveDashboard([]listener{
			{flag: "--listen", addr: "127.0.0.1:0", h: http.NewServeMux(), says: "DASHBOARD listening on http://%s/\n"},
			{flag: "--pull", addr: held.Addr().String(), h: http.NewServeMux(), says: "DASHBOARD pull routes on http://%s/\n"},
		}, &out)
		require.Error(t, err)
		assert.Nil(t, servers)
		assert.Empty(t, out.String(), "a listener that cannot start is refused before anything is said")
		assert.Contains(t, err.Error(), "--pull "+held.Addr().String(), err.Error())
		assert.Contains(t, err.Error(), "address already in use", err.Error())
		// the listener already open was closed again: its Accept says it is closed
		// (the deadline turns a listener left open into a timeout, never a hang)
		require.Len(t, opened, 1, "the first listener opened and the second refused")
		tcp, ok := opened[0].(*net.TCPListener)
		require.True(t, ok, "the first listener is TCP")
		// ignored: a closed listener refuses its deadline as well; Accept says which it is
		_ = tcp.SetDeadline(time.Now())
		_, err = tcp.Accept()
		require.ErrorIs(t, err, net.ErrClosed, "the listener opened before the failed one is closed again")
	})
}
