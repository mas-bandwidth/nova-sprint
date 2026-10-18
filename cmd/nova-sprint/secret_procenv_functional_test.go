//go:build functional

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// fakeSecret is a random password made for one test: no store, keychain or seat holds it.
func fakeSecret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 18)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "fake-" + hex.EncodeToString(b)
}

// A running server holds no store secret where ps reads (docs/SPEC-SPRINT.md, "The
// seat's store login"): `nova-sprint run --listen` started as the fleet launcher starts
// it today, the password in its environment under the name NOVA_SPRINT_REDIS_PASSWORD_ENV
// gives, and started with NOVA_SPRINT_REDIS_PASSWORD_FILE naming a 0600 file, logs in and
// beats; then its argv and its environment, read through the same OS interfaces ps uses
// (Linux: /proc/<pid>/cmdline and /proc/<pid>/environ; macOS: sysctl kern.procargs2),
// hold no byte of the fake secret. The store is a redis-server of the test's own (a
// miniredis has no FUNCTION LOAD, which every store open checks), its one ACL user's
// password the fake secret; each case reads and stops only the child it started.
func TestARunningServerHoldsNoSecretInArgvOrEnv(t *testing.T) {
	ctx := context.Background()
	secret, user := fakeSecret(t), "sprinter"
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	require.NoError(t, fn.Load(ctx, admin))
	require.NoError(t, admin.Do(ctx, "ACL", "SETUSER", user, "on", ">"+secret, "~*", "&*", "+@all").Err())
	require.NoError(t, admin.Do(ctx, "ACL", "SETUSER", "default", "off").Err())
	_ = admin.Close() // ignored: the store is reached as the ACL user from here on
	store := redis.NewClient(&redis.Options{Addr: addr, Username: user, Password: secret})
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Ping(ctx).Err(), "the ACL user logs in")
	require.Error(t, redis.NewClient(&redis.Options{Addr: addr}).Ping(ctx).Err(), "the store refuses a login without the password")

	bin := filepath.Join(t.TempDir(), "nova-sprint")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	home := t.TempDir()
	base := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"NOVA_TEST_NO_HOST=1", "NOVA_SPRINT_ACTOR=coordinator", "NOVA_SPRINT_REDIS=" + addr, "NOVA_SPRINT_REDIS_USER=" + user}
	envLogin := append(append([]string{}, base...), "NOVA_SPRINT_REDIS_PASSWORD_ENV=FAKE_STORE_PASSWORD", "FAKE_STORE_PASSWORD="+secret)
	file := filepath.Join(t.TempDir(), "store-password")
	require.NoError(t, os.WriteFile(file, []byte(secret+"\n"), 0o600))
	require.NoError(t, os.Chmod(file, 0o600))
	fileLogin := append(append([]string{}, base...), "NOVA_SPRINT_REDIS_PASSWORD_FILE="+file)

	// the sprint, made in the launcher's shape: a short-lived verb, scrubbed and not re-exec'd
	initCmd := exec.Command(bin, "init", "--readers", "reader-a", "--members", "m1", "--redis", addr)
	initCmd.Env, initCmd.Dir = envLogin, home
	out, err = initCmd.CombinedOutput()
	require.NoError(t, err, "nova-sprint init: %s", out)
	assert.False(t, strings.Contains(string(out), secret), "a verb's output holds no byte of the secret")

	for _, c := range []struct {
		name string
		env  []string
	}{{"password in the launcher's environment", envLogin}, {"password file", fileLogin}} {
		t.Run(c.name, func(t *testing.T) {
			if keys := heartbeatKeys(t, ctx, store); len(keys) > 0 {
				require.NoError(t, store.Del(ctx, keys...).Err(), "the last case's heartbeat cleared")
			}
			listen := "127.0.0.1:" + strings.TrimPrefix(testutil.FreePort(t), "127.0.0.1:")
			cmd := exec.Command(bin, "run", "--listen", listen, "--land=false", "--redis", addr)
			cmd.Env, cmd.Dir = c.env, home
			var logs bytes.Buffer
			cmd.Stdout, cmd.Stderr = &logs, &logs
			require.NoError(t, cmd.Start())
			pid := cmd.Process.Pid
			t.Cleanup(func() {
				_ = cmd.Process.Signal(syscall.SIGTERM) // ignored: it may have exited already
				done := make(chan struct{})
				go func() { _ = cmd.Wait(); close(done) }() // ignored: the exit is not the subject
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					_ = cmd.Process.Kill() // ignored: the test's own child, stopped at last
					<-done
				}
			})

			// it logged in and beats: the heartbeat is in the store
			require.Eventually(t, func() bool { return len(heartbeatKeys(t, ctx, store)) > 0 }, 60*time.Second, 100*time.Millisecond,
				"the server's heartbeat in the store; its output: %s", &logs)

			argv, env, err := procArgsEnv(pid)
			require.NoError(t, err, "the child's argv and environment, read as ps reads them")
			require.True(t, bytes.Contains(argv, []byte("run")), "the read is of the server's own argv")
			require.True(t, bytes.Contains(env, []byte("NOVA_SPRINT_REDIS_USER="+user)), "the read is of the server's own environment")
			assert.False(t, bytes.Contains(argv, []byte(secret)), "the server's argv holds no byte of the secret")
			assert.False(t, bytes.Contains(env, []byte(secret)), "the server's environment holds no byte of the secret")
			assert.False(t, strings.Contains(logs.String(), secret), "the server's output holds no byte of the secret")
		})
	}
}

// heartbeatKeys is the store's heartbeat keys, every sprint's.
func heartbeatKeys(t *testing.T, ctx context.Context, c *redis.Client) []string {
	t.Helper()
	keys, err := c.Keys(ctx, "*heartbeat").Result()
	require.NoError(t, err)
	return keys
}
