package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/redisconn"
	"github.com/mas-bandwidth/nova-sprint/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seatEnvApp(t *testing.T, body string) *app {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "nova-sprint")
	require.NoError(t, os.MkdirAll(dir, 0700))
	if body != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "seat-environment.json"), []byte(body), 0600))
	}
	a := newApp(func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return root
		}
		return ""
	})
	a.home = func() (string, error) { return root, nil }
	return a
}

func TestSeatEnvironmentDefaultsAndExplicitPrecedence(t *testing.T) {
	t.Parallel()
	a := seatEnvApp(t, `{"version":1,"environment":{"NOVA_SPRINT_REDIS":"file:6380","NOVA_SPRINT_SERVER":"server:6390","NOVA_BUS_REDIS":"bus:6381"}}`)
	base := a.getenv
	a.getenv = func(k string) string {
		if k == "NOVA_SPRINT_REDIS" {
			return "process:6380"
		}
		return base(k)
	}
	require.NoError(t, a.loadSeatEnvironment())
	assert.Equal(t, "process:6380", a.getenv("NOVA_SPRINT_REDIS"))
	assert.Equal(t, "server:6390", a.getenv("NOVA_SPRINT_SERVER"))
	assert.Empty(t, a.getenv("NOVA_SPRINT_ACTOR"))
	fs, c := a.verbSetup("where")
	require.NoError(t, fs.Parse([]string{"--redis", "flag:6380"}))
	assert.Equal(t, "flag:6380", c.redis)
	require.NoError(t, fs.Parse([]string{"--redis="}))
	assert.Empty(t, c.redis, "explicit blank does not fall back")
}

func TestSeatEnvironmentOptionalAbsentAndHomeFallback(t *testing.T) {
	t.Parallel()
	a := seatEnvApp(t, "")
	require.NoError(t, a.loadSeatEnvironment())
	assert.Nil(t, a.seatEnvironment)
	a.getenv = func(string) string { return "" }
	path, err := a.seatEnvironmentPath()
	require.NoError(t, err)
	home, err := a.home()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "nova-sprint", "seat-environment.json"), path)
}

func TestSeatEnvironmentRejectsUnsafeDataWithoutEchoingValues(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"version":2}`, `{"version":1,"version":1}`, `{"version":1,"Version":2}`,
		`{"version":1,"actor":"INJECTED_SECRET"}`,
		`{"version":1,"environment":{"NOVA_SPRINT_REDIS_PASSWORD":"INJECTED_SECRET"}}`,
		`{"version":1,"environment":{"NOVA_SPRINT_REDIS":"redis://user:INJECTED_SECRET@host"}}`,
		`{"version":1,"environment":{"NOVA_BUS_REDIS":"host:1","NOVA_BUS_REDIS":"host:2"}}`,
		`{"version":1,"login":{"redis":"host:1","user":"user"}}`,
		`{"version":1} {"password":"INJECTED_SECRET"}`,
	} {
		a := seatEnvApp(t, body)
		err := a.loadSeatEnvironment()
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "INJECTED_SECRET")
		assert.Nil(t, a.seatEnvironment)
	}
}

func TestSeatEnvironmentRejectsPermissiveAndSymlinkFiles(t *testing.T) {
	t.Parallel()
	a := seatEnvApp(t, `{"version":1}`)
	path, err := a.seatEnvironmentPath()
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0644))
	require.Error(t, a.loadSeatEnvironment())
	require.NoError(t, os.Chmod(path, 0600))
	target := path + ".target"
	require.NoError(t, os.Rename(path, target))
	require.NoError(t, os.Symlink(target, path))
	require.Error(t, a.loadSeatEnvironment())
}

func TestSeatEnvironmentCredentialReferenceIsBoundToEachAddress(t *testing.T) {
	t.Parallel()
	l := storeLogin{Redis: "seat:6380", User: "coordinator", Store: "/secret-store", As: "seat", Key: "/seat.key", Sops: "sops", Secret: "PASSWORD"}
	b, err := json.Marshal(seatEnvironment{Version: 1, Login: &l})
	require.NoError(t, err)
	a := seatEnvApp(t, string(b))
	require.NoError(t, a.loadSeatEnvironment())
	a.seatLoginOn()
	reads := 0
	a.loginSecret = func(got secrets.Login) (secrets.Secret, error) {
		reads++
		assert.Equal(t, l.secrets(), got)
		return secrets.NewSecret("NEVER_PRINT"), nil
	}
	for _, addr := range []string{"other:6380", l.Redis} {
		o, env, err := a.storeOptions(addr)
		require.NoError(t, err)
		if addr == l.Redis {
			assert.Equal(t, l.User, o.User)
			assert.Equal(t, "NEVER_PRINT", env(o.PasswordEnv))
		} else {
			assert.Empty(t, o.User)
		}
		o, env, err = a.seatBusOptions(redisconn.Options{Addr: addr})
		require.NoError(t, err)
		if addr == l.Redis {
			assert.Equal(t, l.User, o.User)
			assert.Equal(t, "NEVER_PRINT", env(o.PasswordEnv))
		} else {
			assert.Empty(t, o.User)
		}
	}
	assert.Equal(t, 2, reads, "alternate endpoints do not resolve the old credential")
	b, err = json.Marshal(storeLogin{Redis: "existing:6380", User: "existing", Store: l.Store, As: l.As, Key: l.Key, Sops: l.Sops, Secret: l.Secret})
	require.NoError(t, err)
	path, err := a.defaultLoginFile()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0600))
	got, ok, err := a.recordedLogin()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "existing:6380", got.Redis)
}

func TestSeatEnvironmentLeavesLegacySeatRecordIntact(t *testing.T) {
	t.Parallel()
	a := seatEnvApp(t, "")
	a.seatLoginOn()
	path, err := a.seatRecordPath()
	require.NoError(t, err)
	body := []byte(`{"server":"legacy:6390","config_seat":"legacy-config"}`)
	require.NoError(t, os.WriteFile(path, body, 0600))
	require.NoError(t, a.loadSeatEnvironment())
	r, ok, err := a.recordedSeat()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "legacy:6390", r.Server)
	assert.Equal(t, "legacy-config", r.ConfigSeat)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, body, after)
}

func TestSeatEnvironmentDoesNotOverrideExplicitBusLogin(t *testing.T) {
	t.Parallel()
	l := storeLogin{Redis: "seat:6380", User: "coordinator", Store: "/store", As: "seat", Key: "/key", Sops: "sops", Secret: "PASSWORD"}
	b, err := json.Marshal(seatEnvironment{Version: 1, Login: &l})
	require.NoError(t, err)
	a := seatEnvApp(t, string(b))
	require.NoError(t, a.loadSeatEnvironment())
	a.seatLoginOn()
	a.loginSecret = func(secrets.Login) (secrets.Secret, error) {
		t.Fatal("explicit bus login must not read seat credential")
		return secrets.Secret{}, nil
	}
	o, _, err := a.seatBusOptions(redisconn.Options{Addr: l.Redis, User: "explicit", PasswordEnv: "EXPLICIT_PASSWORD"})
	require.NoError(t, err)
	assert.Equal(t, "explicit", o.User)
	assert.Equal(t, "EXPLICIT_PASSWORD", o.PasswordEnv)
}

func TestSeatEnvironmentForgeReferenceIsOnlyADefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b, err := json.Marshal(seatEnvironment{Version: 1, ForgeIdentity: dir})
	require.NoError(t, err)
	a := seatEnvApp(t, string(b))
	require.NoError(t, a.loadSeatEnvironment())
	assert.Equal(t, dir, a.forgeIdentityDefault())
	assert.Empty(t, a.getenv("GH_CONFIG_DIR"), "file does not export a process variable")
	base := a.getenv
	a.getenv = func(k string) string {
		if k == "GH_CONFIG_DIR" {
			return "/explicit/config"
		}
		return base(k)
	}
	assert.Equal(t, "/explicit/config", a.forgeIdentityDefault())
	assert.Empty(t, a.getenv("NOVA_SPRINT_ACTOR"))
}

func TestSeatEnvironmentExplicitStoreAuthWinsWithoutReadingFileSecret(t *testing.T) {
	t.Parallel()
	l := storeLogin{Redis: "seat:6380", User: "file-user", Store: "/store", As: "seat", Key: "/key", Sops: "sops", Secret: "PASSWORD"}
	b, err := json.Marshal(seatEnvironment{Version: 1, Login: &l})
	require.NoError(t, err)
	a := seatEnvApp(t, string(b))
	base := a.getenv
	a.getenv = func(k string) string {
		switch k {
		case "NOVA_SPRINT_REDIS_USER":
			return "explicit-user"
		case "NOVA_SPRINT_REDIS_PASSWORD_ENV":
			return "EXPLICIT_PASSWORD"
		case "EXPLICIT_PASSWORD":
			return "explicit-value"
		}
		return base(k)
	}
	require.NoError(t, a.loadSeatEnvironment())
	a.seatLoginOn()
	a.loginSecret = func(secrets.Login) (secrets.Secret, error) {
		t.Fatal("explicit process auth must not read file credential")
		return secrets.Secret{}, nil
	}
	o, env, err := a.storeOptions(l.Redis)
	require.NoError(t, err)
	resolved, err := redisconn.Resolve(o, env)
	require.NoError(t, err)
	assert.Equal(t, "explicit-user", resolved.User)
	assert.Equal(t, "explicit-value", env(resolved.PasswordEnv))
}

func TestSeatEnvironmentKeepsMetadataAndHelpUsable(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"help"}, {"version"}, {"--version"}, {"where", "--help"}} {
		assert.False(t, seatEnvironmentNeeded(args))
	}
	assert.False(t, seatEnvironmentNeeded([]string{"unknown-verb"}))
	assert.False(t, seatEnvironmentNeeded([]string{"where", "--unknown-flag"}))
	assert.True(t, seatEnvironmentNeeded([]string{"where"}))
	assert.True(t, seatEnvironmentNeeded([]string{"hold", "m1", "--reason", "-h"}), "a string value is not a request for help")
	assert.True(t, seatEnvironmentNeeded([]string{"hold", "--", "-h"}), "words after -- are not flags")
}

type seatFileInfo struct {
	os.FileInfo
	stat *syscall.Stat_t
}

func (f seatFileInfo) Sys() any { return f.stat }

func TestSeatEnvironmentRejectsOtherOwnerWithoutHostPrivileges(t *testing.T) {
	t.Parallel()
	a := seatEnvApp(t, `{"version":1}`)
	path, err := a.seatEnvironmentPath()
	require.NoError(t, err)
	fi, err := os.Lstat(path)
	require.NoError(t, err)
	assert.True(t, seatFileOwned(fi, os.Getuid()))
	st := *fi.Sys().(*syscall.Stat_t)
	st.Uid++
	assert.False(t, seatFileOwned(seatFileInfo{FileInfo: fi, stat: &st}, os.Getuid()))
}

func TestSeatEnvironmentKeepsLegacyProcessAddressAheadOfFile(t *testing.T) {
	t.Parallel()
	a := seatEnvApp(t, `{"version":1,"environment":{"NOVA_SPRINT_REDIS":"file:6380"}}`)
	base := a.getenv
	a.getenv = func(k string) string {
		if k == "NOVA_REDIS_ADDR" {
			return "legacy-process:6380"
		}
		return base(k)
	}
	require.NoError(t, a.loadSeatEnvironment())
	_, c := a.verbSetup("where")
	assert.Equal(t, "legacy-process:6380", c.redis)
}

func TestSeatEnvironmentExplicitPasswordReferenceKeepsDefaultUser(t *testing.T) {
	t.Parallel()
	l := storeLogin{Redis: "seat:6380", User: "coordinator", Store: "/store", As: "seat", Key: "/key", Sops: "sops", Secret: "FILE_PASSWORD"}
	b, err := json.Marshal(seatEnvironment{Version: 1, Login: &l})
	require.NoError(t, err)
	a := seatEnvApp(t, string(b))
	base := a.getenv
	a.getenv = func(k string) string {
		switch k {
		case "NOVA_SPRINT_REDIS_PASSWORD_ENV":
			return "EXPLICIT_PASSWORD"
		case "EXPLICIT_PASSWORD":
			return "explicit-default-user-value"
		}
		return base(k)
	}
	require.NoError(t, a.loadSeatEnvironment())
	a.seatLoginOn()
	reads := 0
	a.loginSecret = func(secrets.Login) (secrets.Secret, error) {
		reads++
		return secrets.NewSecret("file-coordinator-value"), nil
	}
	o, env, err := a.storeOptions(l.Redis)
	require.NoError(t, err)
	resolved, err := redisconn.Resolve(o, env)
	require.NoError(t, err)
	assert.Empty(t, resolved.User, "an explicit password reference without a user means default-user auth")
	assert.Equal(t, "explicit-default-user-value", env(resolved.PasswordEnv))
	assert.Zero(t, reads, "explicit auth must never resolve the recorded coordinator secret")
}

func TestSeatEnvironmentRejectsUntrustedConfigDirectory(t *testing.T) {
	t.Parallel()
	a := seatEnvApp(t, `{"version":1}`)
	path, err := a.seatEnvironmentPath()
	require.NoError(t, err)
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(dir, 0775))
	require.Error(t, a.loadSeatEnvironment())
	assert.Nil(t, a.seatEnvironment)
	require.NoError(t, os.Chmod(dir, 0700))
	require.NoError(t, a.loadSeatEnvironment())
	fi, err := os.Lstat(dir)
	require.NoError(t, err)
	st := *fi.Sys().(*syscall.Stat_t)
	st.Uid++
	assert.False(t, seatDirectoryOwned(seatFileInfo{FileInfo: fi, stat: &st}, os.Getuid()))
}

func TestSeatEnvironmentDoesNotReadRelativeHomeConfig(t *testing.T) {
	t.Parallel()
	a := newApp(func(string) string { return "" })
	a.home = func() (string, error) { return ".", nil }
	_, err := a.seatEnvironmentPath()
	require.Error(t, err)
}

func TestSeatEnvironmentExplicitBusPasswordReferenceKeepsDefaultUser(t *testing.T) {
	t.Parallel()
	l := storeLogin{Redis: "seat:6380", User: "coordinator", Store: "/store", As: "seat", Key: "/key", Sops: "sops", Secret: "FILE_PASSWORD"}
	b, err := json.Marshal(seatEnvironment{Version: 1, Login: &l})
	require.NoError(t, err)
	a := seatEnvApp(t, string(b))
	base := a.getenv
	a.getenv = func(k string) string {
		switch k {
		case "NOVA_BUS_REDIS":
			return l.Redis
		case "NOVA_BUS_REDIS_PASSWORD_ENV":
			return "EXPLICIT_PASSWORD"
		case "EXPLICIT_PASSWORD":
			return "explicit-default-user-value"
		}
		return base(k)
	}
	require.NoError(t, a.loadSeatEnvironment())
	a.seatLoginOn()
	a.loginSecret = func(secrets.Login) (secrets.Secret, error) {
		t.Fatal("explicit bus password reference must not read the recorded secret")
		return secrets.Secret{}, nil
	}
	o, env, err := a.seatBusOptions(busOptions(a.getenv))
	require.NoError(t, err)
	resolved, err := redisconn.Resolve(o, env)
	require.NoError(t, err)
	assert.Empty(t, resolved.User)
	assert.Equal(t, "explicit-default-user-value", env(resolved.PasswordEnv))
}
