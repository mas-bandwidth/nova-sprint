package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// friend sync uninstall's --dry-run and --json say the unit's file and whether
// it is there, and change nothing, as seat uninstall's do (seatinstall.go):
// the path the two verbs share after their copies were folded into one helper
// (uninstallUnit, friendsync_install.go). The exit codes and the lines are the
// rule both verbs keep: 0 done, nothing unloaded or removed on a dry run.
func TestFriendSyncUninstallSaysTheUnitAndChangesNothing(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t)
	home := t.TempDir()
	ta.a.goos = "linux"
	ta.a.home = func() (string, error) { return home, nil }
	ta.a.executable = func() (string, error) { return "/opt/nova/bin/nova-sprint", nil }
	env := map[string]string{"XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	before := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if v, ok := env[k]; ok {
			return v
		}
		return before(k)
	}
	var calls []string
	ta.a.seatLoad = func(goos, op, path string) error { calls = append(calls, goos+" "+op+" "+path); return nil }
	unit := filepath.Join(home, ".config", "systemd", "user", sprint.FriendSyncService)

	// nothing there: the dry run says so and touches nothing
	code, o, e := ta.do("friend sync uninstall --dry-run")
	require.Equal(t, 0, code, e)
	assert.Contains(t, o, "FRIEND-SYNC UNINSTALL DRY-RUN unit="+unit+" present=false; nothing was unloaded or removed")
	assert.Empty(t, calls, "a dry run unloads nothing")

	// installed: the dry run says it is there and still touches nothing
	code, _, e = ta.do("friend sync install --every 15s --redis 127.0.0.1:6380")
	require.Equal(t, 0, code, e)
	require.FileExists(t, unit)
	calls = nil
	code, o, e = ta.do("friend sync uninstall --dry-run")
	require.Equal(t, 0, code, e)
	assert.Contains(t, o, "FRIEND-SYNC UNINSTALL DRY-RUN unit="+unit+" present=true; nothing was unloaded or removed")
	assert.Empty(t, calls, "a dry run unloads nothing")
	assert.FileExists(t, unit, "a dry run removes nothing")

	// --json says the path and that it was removed, and the file is gone
	code, o, e = ta.do("friend sync uninstall --json")
	require.Equal(t, 0, code, e)
	var got struct {
		Path    string `json:"path"`
		Removed bool   `json:"removed"`
	}
	require.NoError(t, json.Unmarshal([]byte(o), &got), o)
	assert.Equal(t, unit, got.Path)
	assert.True(t, got.Removed)
	assert.NoFileExists(t, unit)
	assert.Equal(t, []string{"linux unload " + unit}, calls)
}
