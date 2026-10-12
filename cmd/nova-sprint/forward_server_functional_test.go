//go:build functional

// A hook process run against an httptest listener, on a unix socket of its own: a
// functional test (RULES 6a: no sockets in a unit test). The unit tier's forward
// tests are forward_server_test.go.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hook names the server it used and where it was named: with NOVA_SPRINT_SERVER unset
// and the seat's record naming one, it says from=seat-record and connects there; the
// environment's server wins when it names one.
func TestTheHookNamesTheServerItUsedAndWhereItCameFrom(t *testing.T) {
	t.Parallel()
	const name = "hook-s"
	ta, env := hookSprint(t, name)
	srv := httptest.NewServer(localHandler{ta.a})
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()

	dir := t.TempDir()
	b, err := json.Marshal(seatRecord{Server: addr})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, seatRecordFile), b, 0o600))
	sockdir, err := os.MkdirTemp(".", ".hks")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(sockdir) })
	n := 0

	// connected runs a hook on its own socket (a second hook replaces the first) and
	// returns the line that names the server it used.
	connected := func() string {
		t.Helper()
		client := newApp(ta.a.getenv)
		client.busOpen = ta.a.busOpen // the twin's bus store: the hook arms its bus push there
		t.Cleanup(client.close)
		client.loginFile = func() (string, error) { return filepath.Join(dir, "login.json"), nil }
		client.hookSock = func(nm string) (string, error) {
			n++
			return filepath.Join(sockdir, nm+strconv.Itoa(n)+".sock"), nil
		}
		pr, pw := io.Pipe()
		go func() {
			var errb bytes.Buffer
			_ = runInto(client, []string{"hook"}, pw, &errb)
			_ = pw.Close()
		}()
		sc := bufio.NewScanner(pr)
		line := ""
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "HOOK CONNECTED") {
				line = sc.Text()
				break
			}
		}
		_ = pr.Close()
		return line
	}

	line := connected()
	assert.Contains(t, line, "server="+addr, line)
	assert.Contains(t, line, "from=seat-record", line)

	env[ServerEnv] = addr
	line = connected()
	assert.Contains(t, line, "server="+addr, line)
	assert.Contains(t, line, "from=env", line)
}
