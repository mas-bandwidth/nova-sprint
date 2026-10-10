package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveUsesSeatLoginForLibrary asserts that nova-sprint live calls
// nova-redis fn check with the user and password-env from the recorded
// seat login when the environment does not name NOVA_SPRINT_REDIS_USER.
//
// When the store requires authentication (the default ACL user has NOAUTH)
// and live only checked bare env vars (which are empty), the library state
// would be UNKNOWN. With seat-login, live gets credentials the same way
// every other verb does: storeOptions resolves the user from the login
// and reads the password in process.
//
// seat-store-login-built-in: live uses the same store login the verbs do;
// the library state is read (loaded, wanted, match).
func TestLiveUsesSeatLoginForLibrary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	env := map[string]string{
		"HOME":              dir,
		"NOVA_SPRINT_REDIS": "s:1",
		// No NOVA_SPRINT_REDIS_USER or NOVA_SPRINT_REDIS_PASSWORD_ENV:
		// the login must supply credentials.
		"XDG_CONFIG_HOME": filepath.Join(dir, "cfg"),
	}
	a := newApp(func(k string) string { return env[k] })
	a.now = func() time.Time { return now }
	// Resolve the seat login's secret in tests (real nova-secrets is not available):
	a.loginSecret = func(l secrets.Login) (secrets.Secret, error) {
		return secrets.NewSecret("xpw"), nil
	}

	// Record the login whose redis matches c.redis (= s:1).
	rec := storeLogin{
		Redis:  "s:1",
		User:   "coordinator",
		Store:  "/tmp/s",
		As:     "st",
		Key:    "/tmp/k",
		Sops:   "/usr/bin/sops",
		Secret: "NOVA_REDIS_COORDINATOR_PASSWORD",
	}
	loginPath := filepath.Join(dir, "cfg", "nova-sprint", "login.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(loginPath), 0o700))
	lb, _ := json.MarshalIndent(rec, "", "  ")
	require.NoError(t, os.WriteFile(loginPath, append(lb, '\n'), 0o600))

	localBin := filepath.Join(dir, ".local", "bin")
	require.NoError(t, os.MkdirAll(localBin, 0o755))
	sprintBin := filepath.Join(localBin, "nova-sprint")
	require.NoError(t, os.WriteFile(sprintBin, []byte("x"), 0o755))
	dashDir := filepath.Join(dir, "dash")
	require.NoError(t, os.MkdirAll(dashDir, 0o755))
	agents := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agents, 0o755))

	var ran []string
	fake := adoptRunner(func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		ran = append(ran, line)
		switch {
		case strings.HasSuffix(name, "nova-sprint") && len(args) >= 1 && args[0] == "version":
			return "nova-sprint v1.2.0-dev.abcdef1 linux/amd64 go1.27.1", nil
		case strings.Contains(line, "fn check"):
			src, _ := fn.Source()
			return "OK nova_sprint loaded=" + fn.Sum(src) + " want=" + fn.Sum(src) + " store=s:1", nil
		case strings.HasPrefix(name, "launchctl") || strings.Contains(name, "ps") || strings.Contains(name, "lsof"):
			return "", errors.New("no such process")
		}
		return "", errors.New("unexpected runner call: " + line)
	})
	liveRunnerOf.Store(a, fake)
	defer liveRunnerOf.Delete(a)

	var out, errs bytes.Buffer
	code := a.run([]string{"live", "--json", "--bin-dir", localBin, "--dashboard", filepath.Join(dashDir, "nova-sprint-int"), "--agents-dir", agents}, &out, &errs)
	require.Equal(t, 0, code, "%s", errs.String())

	var m liveManifest
	require.NoError(t, json.Unmarshal(out.Bytes(), &m))

	// Before the fix: fn check runs with --addr only (no --user/--password-env),
	// the command fails, no line matches OK/STALE/MISSING, so Loaded="" and Match=false.
	// After the fix: fn check carries --user coordinator from the seat login,
	// returns OK with loaded=want non-empty and Match=true.
	assert.NotEmpty(t, m.Library.Loaded,
		"library loaded digest should not be empty (without login credentials fn check fails): %s", errs.String())
	assert.Equal(t, m.Library.Want, m.Library.Loaded,
		"library loaded=%s want=%s should match: %s",
		m.Library.Loaded, m.Library.Want, errs.String())
	assert.True(t, m.Library.Match,
		"library match should be true: %s", errs.String())

	// The fn check command carried --user coordinator from the seat login,
	// not bare env vars (which were empty).
	fnCheckArgs := ""
	for _, r := range ran {
		if strings.Contains(r, "fn check") {
			fnCheckArgs = r
			break
		}
	}
	require.NotEmpty(t, fnCheckArgs, "expected a fn check invocation in runs")
	assert.Contains(t, fnCheckArgs, "--user coordinator",
		"fn check should carry --user from the recorded seat login: %s", fnCheckArgs)
	assert.Contains(t, fnCheckArgs, "--password-env",
		"fn check should carry --password-env from the recorded seat login: %s", fnCheckArgs)
}
