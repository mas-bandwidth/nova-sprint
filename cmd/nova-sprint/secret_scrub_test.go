package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// environOf is env as NAME=value pairs, in name order.
func environOf(env map[string]string) []string {
	var out []string
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// The startup scrub, over a fake environment: every password-bearing name (the one
// NOVA_SPRINT_REDIS_PASSWORD_ENV names, the bench and coordinator passwords, any
// NOVA_*_PASSWORD) is gone from the environment and answered by the holder; the
// pointers and every other variable stay; nothing held prints its value.
func TestStartupScrubUnsetsEveryPasswordName(t *testing.T) {
	t.Parallel()
	v := func(n string) string { return unitSecret(t) + "-" + n }
	secrets := map[string]string{
		"FAKE_STORE_PASSWORD":             v("named"),
		"NOVA_REDIS_BENCH_PASSWORD":       v("bench"),
		"NOVA_REDIS_COORDINATOR_PASSWORD": v("coordinator"),
		"NOVA_BUS_REDIS_PASSWORD":         v("bus"),
	}
	stay := map[string]string{
		"NOVA_SPRINT_REDIS_PASSWORD_ENV":      "FAKE_STORE_PASSWORD",
		"NOVA_SPRINT_REDIS_PASSWORD_FILE":     "/run/fake/pw",
		"NOVA_SPRINT_REDIS_PASSWORD_KEYCHAIN": "nova-sprint-store",
		"NOVA_SPRINT_REDIS_USER":              "sprinter",
		"PATH":                                "/usr/bin",
		"OTHER_PASSWORD":                      "not a nova name and not the named one",
	}
	env := map[string]string{"NOVA_EMPTY_PASSWORD": ""}
	for _, m := range []map[string]string{secrets, stay} {
		for k, x := range m {
			env[k] = x
		}
	}
	var unset []string
	held, err := scrubEnv(environOf(env), func(k string) error { unset = append(unset, k); delete(env, k); return nil })
	require.NoError(t, err)

	assert.Equal(t, stay, env, "every value gone, every pointer and other variable kept")
	assert.Contains(t, unset, "NOVA_EMPTY_PASSWORD", "an empty one is unset too")
	assert.Len(t, held, len(secrets), "an empty one is not held")
	getenv := held.getenv(func(k string) string { return env[k] })
	for k, want := range secrets {
		assert.Equal(t, want, getenv(k), "the holder answers %s", k)
	}
	assert.Equal(t, "sprinter", getenv("NOVA_SPRINT_REDIS_USER"), "every other name reads through")
	printed := fmt.Sprintf("%v %+v %#v", held, held, held)
	for _, x := range secrets {
		assert.False(t, strings.Contains(printed, x), "a held value never prints")
	}

	// the values cross a re-exec whole, and nothing else does
	round, err := decodeHeld(held.encode())
	require.NoError(t, err)
	got := map[string]string{}
	for k, s := range round {
		got[k] = revealed(s)
	}
	assert.Equal(t, secrets, got)
	_, err = decodeHeld([]byte("no pair\x00"))
	assert.Error(t, err)

	// a refused unset stops the scrub, naming the variable and never its value
	_, err = scrubEnv([]string{"NOVA_REDIS_BENCH_PASSWORD=" + secrets["FAKE_STORE_PASSWORD"]}, func(string) error { return fmt.Errorf("refused") })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOVA_REDIS_BENCH_PASSWORD")
	assert.False(t, strings.Contains(err.Error(), secrets["FAKE_STORE_PASSWORD"]))
}

// Which names bear a password, and which verbs run long enough to re-exec.
func TestPasswordNamesAndLongRunningVerbs(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"FAKE_STORE_PASSWORD": true, "NOVA_REDIS_BENCH_PASSWORD": true, "NOVA_REDIS_COORDINATOR_PASSWORD": true, "NOVA_PG_PASSWORD": true,
		"NOVA_SPRINT_REDIS_PASSWORD_ENV": false, "NOVA_SPRINT_REDIS_PASSWORD_FILE": false, "NOVA_SPRINT_REDIS_PASSWORD_KEYCHAIN": false,
		"NOVA_BUS_REDIS_PASSWORD_ENV": false, "NOVA_SPRINT_SECRET_FD": false, "PASSWORD": false, "": false,
	} {
		assert.Equal(t, want, passwordName(name, "FAKE_STORE_PASSWORD"), name)
	}
	assert.False(t, passwordName("", ""), "no name is named when the pointer is unset")
	for line, want := range map[string]bool{
		"run": true, "run --listen 10.0.0.1:6390": true, "dashboard --listen 127.0.0.1:0": true, "watch": true, "wait": true,
		"where --watch": true, "inbox --wait=5m --as m1": true, "inbox -wait 5m": true, "play": true,
		"answer --every 1m": true, "friend sync --every=30s": true, "snapshot --every 10m": true,
		"where": false, "tick": false, "add --stream s1 watch": false, "card x -- --watch": false, "": false,
	} {
		assert.Equal(t, want, longRunning(strings.Fields(line)), line)
	}
}

// A nova-sprint child (server switch's and doctor --preflight's shadow tick) of a process
// whose environment login the scrub took: the child is handed the password through a
// 0600 file and NOVA_SPRINT_REDIS_PASSWORD_FILE, its environment holds no byte of it nor
// the variable NOVA_SPRINT_REDIS_PASSWORD_ENV named, and the child's own storeOptions
// reads it back for the same ACL user; done removes the file. Another login is environ
// unchanged.
func TestShadowChildGetsTheEnvLoginThroughAPasswordFile(t *testing.T) {
	t.Parallel()
	pw := unitSecret(t)
	held := heldSecrets{"FAKE_STORE_PASSWORD": secrets.NewSecret(pw)}
	tmp := t.TempDir()
	environ := []string{"PATH=/usr/bin", "NOVA_SPRINT_REDIS_USER=sprinter", "NOVA_SPRINT_REDIS_PASSWORD_ENV=FAKE_STORE_PASSWORD"}

	env, done, err := childStoreEnv(environ, held, tmp)
	require.NoError(t, err)
	child := map[string]string{}
	for _, kv := range env {
		assert.NotContains(t, kv, pw, "the child's environment holds no byte of the password")
		k, v, _ := strings.Cut(kv, "=")
		child[k] = v
	}
	assert.NotContains(t, child, "NOVA_SPRINT_REDIS_PASSWORD_ENV", "the child is not pointed at a variable it lacks")
	assert.Equal(t, "sprinter", child["NOVA_SPRINT_REDIS_USER"])
	path := child[passwordFileEnv]
	require.NotEmpty(t, path, "the child is pointed at the file")
	assert.True(t, strings.HasPrefix(path, tmp+string(os.PathSeparator)), "the file is under the directory named: %s", path)
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())

	c := newApp(func(k string) string { return child[k] })
	o, getenv, err := c.storeOptions("s:1")
	require.NoError(t, err)
	assert.Equal(t, "sprinter", getenv(o.Env.User), "the same ACL user")
	assert.Equal(t, pw, getenv(o.PasswordEnv), "the child's storeOptions reads the password from the file")

	done()
	_, err = os.Stat(filepath.Dir(path))
	assert.True(t, os.IsNotExist(err), "done removes the file and its directory")

	// a user with no password name is the default password variable's (storeOptions)
	env, done, err = childStoreEnv([]string{"NOVA_SPRINT_REDIS_USER=sprinter"}, heldSecrets{"NOVA_REDIS_BENCH_PASSWORD": secrets.NewSecret(pw)}, tmp)
	require.NoError(t, err)
	assert.NotContains(t, strings.Join(env, "\n"), pw)
	assert.Contains(t, strings.Join(env, "\n"), passwordFileEnv+"=")
	done()

	for _, e := range [][]string{
		{"PATH=/usr/bin"}, // the seat login or the default user: the child reads its own
		{"NOVA_SPRINT_REDIS_USER=sprinter", "NOVA_SPRINT_REDIS_PASSWORD_FILE=/run/fake/pw"}, // a file already named
		{"NOVA_SPRINT_REDIS_USER=sprinter", "NOVA_SPRINT_REDIS_PASSWORD_ENV=NOT_HELD"},      // nothing held to hand
	} {
		got, done, err := childStoreEnv(e, held, tmp)
		require.NoError(t, err)
		assert.Equal(t, e, got, "another login is the environment unchanged")
		done()
	}
	left, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, left, "nothing is left behind")
}

// live under an environment login the scrub took: nova-redis fn check (which reads only
// --password-env) is handed the password the app holds, in that child's environment
// alone, so the library is read rather than UNKNOWN.
func TestLiveHandsTheScrubbedEnvLoginToFnCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pw := unitSecret(t)
	base := map[string]string{
		"HOME":                           dir,
		"NOVA_SPRINT_REDIS":              "s:1",
		"NOVA_SPRINT_REDIS_USER":         "sprinter",
		"NOVA_SPRINT_REDIS_PASSWORD_ENV": "FAKE_STORE_PASSWORD",
		"XDG_CONFIG_HOME":                filepath.Join(dir, "cfg"),
	}
	held := heldSecrets{"FAKE_STORE_PASSWORD": secrets.NewSecret(pw)}
	a := newApp(held.getenv(func(k string) string { return base[k] }))
	a.now = func() time.Time { return time.Unix(1000, 0).UTC() }
	var fnArgs string
	var fnEnv []string
	fake := adoptRunner(func(ctx context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		if strings.HasSuffix(name, "nova-sprint") && len(args) > 0 && args[0] == "version" {
			return "nova-sprint v1.2.0-dev.abcdef1 linux/amd64 go1.27.1", nil
		}
		if strings.Contains(line, "fn check") {
			fnArgs, fnEnv = line, adoptEnv(ctx)
			src, _ := fn.Source()
			return "OK nova_sprint loaded=" + fn.Sum(src) + " want=" + fn.Sum(src) + " store=s:1", nil
		}
		return "", errors.New("no such process")
	})
	liveRunnerOf.Store(a, fake)
	t.Cleanup(func() { liveRunnerOf.Delete(a) })
	bin, agents := filepath.Join(dir, "bin"), filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	require.NoError(t, os.MkdirAll(agents, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "nova-sprint"), []byte("x"), 0o755))

	var out, errs bytes.Buffer
	code := runInto(a, []string{"live", "--json", "--bin-dir", bin, "--agents-dir", agents}, &out, &errs)
	require.Equal(t, 0, code, "%s", errs.String())
	assert.Contains(t, fnArgs, "--user sprinter --password-env FAKE_STORE_PASSWORD")
	assert.NotContains(t, fnArgs, pw, "never in argv")
	assert.Equal(t, []string{"FAKE_STORE_PASSWORD=" + pw}, fnEnv, "the fn check child alone is handed the held password")
	var m liveManifest
	require.NoError(t, json.Unmarshal(out.Bytes(), &m))
	assert.Equal(t, "OK", m.Library.State)
	assert.NotContains(t, out.String()+errs.String(), pw, "never printed")
}
