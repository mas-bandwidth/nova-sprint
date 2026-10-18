package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

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
		"where --watch": true, "inbox --wait=5m --as m1": true, "inbox -wait 5m": true,
		"where": false, "tick": false, "add --stream s1 watch": false, "card x -- --watch": false, "": false,
	} {
		assert.Equal(t, want, longRunning(strings.Fields(line)), line)
	}
}
