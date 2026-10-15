//go:build functional

package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestNoVerbThroughTheHelperReachesAPointedServer pins the helper's guard: a
// machine with NOVA_SPRINT_SERVER set and a seat record naming a sprint server
// would make an unscrubbed verb forward there (forward.go:28-71). A verb run
// through the helper never reaches it: the helper adds --redis <the test's own
// store>, which never forwards, and scrubs the environment, so the pointed
// server hears nothing.
func TestNoVerbThroughTheHelperReachesAPointedServer(t *testing.T) {
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(t.Context(), c))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	var reached atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			reached.Add(1)
			_ = conn.Close()
		}
	}()

	ta := newFunctionalTestApp(t, addr)

	ta.Env["NOVA_SPRINT_SERVER"] = listener.Addr().String()
	ta.app().loginFile = ta.app().defaultLoginFile
	seatDir := filepath.Join(ta.home, ".config", "nova-sprint")
	require.NoError(t, os.MkdirAll(seatDir, 0o755))
	record := fmt.Sprintf(`{"server":%q}`, listener.Addr().String())
	require.NoError(t, os.WriteFile(filepath.Join(seatDir, "seat.json"), []byte(record), 0o644))

	addr2, from := ta.app().namedServer()
	require.Equal(t, listener.Addr().String(), addr2, "the setup points the server at the listener")
	require.Equal(t, "env", from)

	ta.runVerb("init --readers reader-a --members m1")
	ta.runVerb("where")
	ta.runVerb("add --stream s1 --count 2")
	ta.runVerb("tick")

	time.Sleep(200 * time.Millisecond)
	require.Zero(t, reached.Load(), "no verb run through the helper reached the pointed server")
}

// TestRunIntoWithoutRedisRefusesTheLiveServer is the red case the coordinator
// named: an app built the way an unscrubbed functional test builds one (the
// machine's home, NOVA_SPRINT_SERVER named in this process's environment, a
// seat record naming it, and no a.forward), run through runInto with a verb that
// names no --redis, is exactly the hole a test could forward on (forward.go).
// runInto scrubs the home and the machine's server variable first, so the call
// never reaches the pointed server.
func TestRunIntoWithoutRedisRefusesTheLiveServer(t *testing.T) {
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(t.Context(), c))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	var reached atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			reached.Add(1)
			_ = conn.Close()
		}
	}()

	home := t.TempDir()
	seatDir := filepath.Join(home, ".config", "nova-sprint")
	require.NoError(t, os.MkdirAll(seatDir, 0o755))
	record := fmt.Sprintf(`{"server":%q}`, listener.Addr().String())
	require.NoError(t, os.WriteFile(filepath.Join(seatDir, "seat.json"), []byte(record), 0o644))

	// the machine's own environment names the live server: this process's, which
	// newApp(os.Getenv) reads, the way an unscrubbed functional test reads it.
	t.Setenv(ServerEnv, listener.Addr().String())
	t.Setenv("NOVA_SPRINT_REDIS", addr)
	t.Setenv("NOVA_SPRINT_ACTOR", "coordinator")
	a := newApp(os.Getenv)
	defer a.close()
	a.home = func() (string, error) { return home, nil }

	var out, errb bytes.Buffer
	runInto(a, []string{"where", "--json"}, &out, &errb)

	time.Sleep(200 * time.Millisecond)
	require.Zero(t, reached.Load(), "a runInto call without --redis reached the pointed server")
}

// TestRunIntoUsesTheTestsOwnServer pins the other half of the guard, the fix for
// attempt 10's regression: a server a test set itself, in a getenv of its own to
// reach its own controlled listener, is not the machine's server and must not be
// scrubbed away. The run reaches it, the way
// TestTheHookNamesTheServerItUsedAndWhereItCameFrom and
// TestTheHookFromTheCommandLine need.
func TestRunIntoUsesTheTestsOwnServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"code":0,"stdout":"ok\n","stderr":""}]}`)
	}))
	t.Cleanup(srv.Close)

	env := map[string]string{ServerEnv: srv.Listener.Addr().String(), "NOVA_SPRINT_ACTOR": "coordinator"}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return t.TempDir(), nil }

	var out, errb bytes.Buffer
	code := runInto(a, []string{"where"}, &out, &errb)
	require.Zero(t, code, "the verb the test's own server answered: %s%s", out.String(), errb.String())
	require.Contains(t, out.String(), "ok", "runInto reached the test's own server")
}
