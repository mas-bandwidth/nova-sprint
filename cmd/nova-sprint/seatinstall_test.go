package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// seat install records the seat on this machine (the sprint's server it uses) and writes
// no unit: the seat's push loop is retired (v1.3.6; the server sends the coordinator's
// judgments to its name on the bus). Given neither a server nor a config seat it refuses
// and writes nothing. seat uninstall removes the retired loop's unit where an older build
// left it, through the test's loader, never launchctl or systemctl.
func TestSeatInstallRecordsTheSeatAndUninstallRemovesTheRetiredLoop(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_ACTOR": "rowan", "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	a.seatLoginOn()
	a.goos = "darwin"
	a.home = func() (string, error) { return home, nil }
	var calls []string
	a.seatLoad = func(goos, op, path string) error { calls = append(calls, goos+" "+op+" "+path); return nil }
	do := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := runInto(a, args, &out, &errb)
		return code, out.String(), errb.String()
	}
	record := filepath.Join(home, ".config", "nova-sprint", seatRecordFile)

	code, out, errs := do("seat", "install")
	require.Equal(t, 2, code, out+errs)
	assert.Contains(t, errs, "was given neither; nothing was written")
	assert.NoFileExists(t, record)

	for _, retired := range [][]string{{"--harness", "opencode"}, {"--target", home}, {"--dir", home}} {
		code, _, errs = do(append([]string{"seat", "install", "--server", "127.0.0.1:7480"}, retired...)...)
		assert.Equal(t, 2, code, "%v is the retired push loop's flag: %s", retired, errs)
	}
	assert.NoFileExists(t, record)

	code, out, errs = do("seat", "install", "--server", "127.0.0.1:7480", "--dry-run")
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "SEAT RECORD DRY-RUN file="+record+" server=127.0.0.1:7480; nothing was written")
	assert.NoFileExists(t, record)

	code, out, errs = do("seat", "install", "--server", "127.0.0.1:7480")
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "SEAT RECORD OK file="+record+" server=127.0.0.1:7480 config-seat=-")
	b, err := os.ReadFile(record)
	require.NoError(t, err)
	var rec seatRecord
	require.NoError(t, json.Unmarshal(b, &rec))
	assert.Equal(t, seatRecord{Server: "127.0.0.1:7480"}, rec)
	assert.Equal(t, "127.0.0.1:7480", a.seatServer(), "the coordinator's verbs read the server from the record")
	assert.Empty(t, calls, "seat install loads nothing")
	assert.NoDirExists(t, filepath.Join(home, "Library", "LaunchAgents"), "seat install writes no unit")

	// the retired loop's unit, as an older build left it: uninstall unloads and removes it
	agents := t.TempDir()
	unit := filepath.Join(agents, sprint.SeatUnitFile("darwin"))
	require.NoError(t, os.WriteFile(unit, []byte("<plist/>"), 0o644))
	code, out, errs = do("seat", "uninstall", "--dir", agents, "--dry-run")
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "SEAT UNINSTALL DRY-RUN unit="+unit+" present=true")
	assert.FileExists(t, unit, "a dry run removes nothing")
	code, out, errs = do("seat", "uninstall", "--dir", agents, "--json")
	require.Equal(t, 0, code, errs)
	var got struct {
		Path    string `json:"path"`
		Removed bool   `json:"removed"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	assert.Equal(t, unit, got.Path)
	assert.True(t, got.Removed)
	assert.NoFileExists(t, unit)
	assert.Equal(t, []string{"darwin unload " + unit}, calls)
}

// The seat's push loop's word is refused by inbox --wait, naming what replaced it: a unit
// an older build installed fails at once with the remedy, and writes nothing.
func TestInboxWaitRefusesTheRetiredSeatPushLoop(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	code, out, errs := ta.do("inbox --wait --push " + retiredPushWord + " --timeout 200ms")
	assert.Equal(t, 2, code, out+errs)
	assert.Contains(t, errs, "the seat's push loop is retired (v1.3.6)")
	assert.Contains(t, errs, "nova-hook hook --as <you> --role coordinator")
	assert.Contains(t, errs, "nova-sprint seat uninstall")
	assert.NoDirExists(t, retiredPushWord)
}

// recordSeatLogin records a login for addr. A Redis unit is installed only when that
// login matches. The file names the secret and holds no value.
func recordSeatLogin(t *testing.T, a *app, addr string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "login.json")
	b, err := json.Marshal(storeLogin{Redis: addr, User: "coordinator", Store: dir, As: "studio", Key: filepath.Join(dir, "k"), Sops: "/usr/bin/sops", Secret: "PW"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(b, '\n'), 0o600))
	a.loginFile = func() (string, error) { return path, nil }
}
