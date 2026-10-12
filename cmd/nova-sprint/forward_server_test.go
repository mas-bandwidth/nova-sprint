package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
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
