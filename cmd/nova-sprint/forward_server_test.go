package main

import (
	"bufio"
	"bytes"
	"context"
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

	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// seatRecordedApp is a coordinator's side with no server in its environment and a seat
// record seat install wrote in a temp store-login directory (never the real one), as a
// cold coordinator has (coordinator-config-through-the-seat; docs/SPEC-SPRINT.md, "The
// server"): the seat names the sprint's server, and a forwarded verb must be sent there.
func seatRecordedApp(t *testing.T, server string) (*app, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:0", "NOVA_SPRINT_ACTOR": "boss"}
	a := newApp(func(k string) string { return env[k] })
	t.Cleanup(a.close)
	a.loginFile = func() (string, error) { return filepath.Join(dir, "login.json"), nil }
	if server != "" {
		b, err := json.Marshal(seatRecord{Server: server})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, seatRecordFile), b, 0o600))
	}
	return a, env
}

// When NOVA_SPRINT_SERVER is unset, a coordinator's verb goes to the server the seat's
// record names, in the same order seat check uses; the environment, when it names one,
// wins over the record; and a verb given its own --redis is run here, not forwarded.
func TestASeatRecordedServerIsUsedWhenTheEnvironmentNamesNone(t *testing.T) {
	t.Parallel()
	a, env := seatRecordedApp(t, "127.0.0.1:6399")
	var sent []string
	a.forward = func(_ context.Context, addr string, _ ...[]string) ([]sprintwire.Result, error) {
		sent = append(sent, addr)
		return []sprintwire.Result{{}}, nil
	}
	run := func(args ...string) {
		t.Helper()
		var out, errb bytes.Buffer
		_ = runInto(a, args, &out, &errb)
	}

	run("where")
	assert.Equal(t, []string{"127.0.0.1:6399"}, sent, "the recorded server is the one the verb went to")

	env[ServerEnv] = "127.0.0.1:7500"
	run("where")
	assert.Equal(t, []string{"127.0.0.1:6399", "127.0.0.1:7500"}, sent, "the environment's server wins over the record")

	delete(env, ServerEnv)
	run("where", "--redis", "mem:"+filepath.Join(t.TempDir(), "other.twin"))
	assert.Len(t, sent, 2, "a verb given its own --redis is run here, never forwarded")
}

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

// With neither source naming a server, hook refuses in one line naming both and the
// commands that set one (docs/SPEC-SPRINT.md, "The server").
func TestHookRefusesWhenNeitherSourceNamesAServer(t *testing.T) {
	t.Parallel()
	a, _ := seatRecordedApp(t, "")
	rec, err := a.seatRecordPath()
	require.NoError(t, err)
	var out, errb bytes.Buffer
	code := runInto(a, []string{"hook"}, &out, &errb)
	require.Equal(t, 2, code, out.String()+errb.String())
	assert.Contains(t, errb.String(), "no sprint server named: "+ServerEnv+" is not set and the seat's record "+rec+" names none")
	assert.Contains(t, errb.String(), "export "+ServerEnv+"=<host:port>")
	assert.Contains(t, errb.String(), "nova-sprint seat install --harness <name> --target <dir> --server <host:port>")
}
