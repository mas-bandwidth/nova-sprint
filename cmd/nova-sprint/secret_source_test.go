package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/redisconn"
	"github.com/mas-bandwidth/nova-tools/pkg/secrets"
)

// unitSecret is a random fake password made for one test.
func unitSecret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "fake-" + hex.EncodeToString(b)
}

// writeMode writes v to path with exactly mode perm, whatever the umask.
func writeMode(t *testing.T, path, v string, perm os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(v), perm))
	require.NoError(t, os.Chmod(path, perm))
}

// revealed is s's value, for a test's comparison.
func revealed(s secrets.Secret) string {
	v := ""
	_ = s.Use(func(p string) error { v = p; return nil }) // ignored: the function never fails
	return v
}

// The password file is a regular file of this user's alone: group- or other-readable,
// a symbolic link, a relative path or an empty file is refused naming the path and the
// remedy and never the content; 0600 is read with one trailing newline cut.
func TestPasswordFileMustBeOwnerOnly(t *testing.T) {
	t.Parallel()
	secret, dir := unitSecret(t), t.TempDir()

	open := filepath.Join(dir, "open")
	writeMode(t, open, secret+"\n", 0o644)
	_, err := readPasswordFile(open)
	require.Error(t, err, "0644 is refused")
	assert.Contains(t, err.Error(), open, "the refusal names the path")
	assert.Contains(t, err.Error(), "chmod 600 "+open, "and the remedy")
	assert.False(t, strings.Contains(err.Error(), secret), "and never the content")

	group := filepath.Join(dir, "group")
	writeMode(t, group, secret, 0o640)
	_, err = readPasswordFile(group)
	require.Error(t, err, "0640 is refused")
	assert.False(t, strings.Contains(err.Error(), secret))

	private := filepath.Join(dir, "private")
	writeMode(t, private, secret+"\n", 0o600)
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(private, link))
	_, err = readPasswordFile(link)
	require.Error(t, err, "a symbolic link is refused, even to a 0600 file")
	assert.Contains(t, err.Error(), "symbolic link")
	assert.False(t, strings.Contains(err.Error(), secret))

	_, err = readPasswordFile("private")
	require.Error(t, err, "a relative path is refused")
	assert.Contains(t, err.Error(), "not an absolute path")

	_, err = readPasswordFile(dir)
	require.Error(t, err, "a directory is refused")

	empty := filepath.Join(dir, "empty")
	writeMode(t, empty, "\n", 0o600)
	_, err = readPasswordFile(empty)
	require.Error(t, err, "an empty file is refused")
	assert.Contains(t, err.Error(), "is empty")

	s, err := readPasswordFile(private)
	require.NoError(t, err, "0600 is read")
	assert.Equal(t, secret, revealed(s), "one trailing newline cut")
	assert.False(t, strings.Contains(fmt.Sprintf("%v %+v %#v %s", s, s, s, s), secret), "the value never prints")

	twoLines := filepath.Join(dir, "two")
	writeMode(t, twoLines, secret+"\n\n", 0o400)
	s, err = readPasswordFile(twoLines)
	require.NoError(t, err, "0400 is read")
	assert.Equal(t, secret+"\n", revealed(s), "only one trailing newline is cut")
}

// sourceApp is a login app whose environment names the password file: the store is
// opened through the real path (redisBackend, storeOptions), each dial recorded.
func sourceApp(t *testing.T, env map[string]string) *loginApp {
	la := newLoginApp(t, t.TempDir())
	for k, v := range env {
		la.env[k] = v
	}
	return la
}

// The file is read once per process: a second store open reads the value the first
// read, the file changed or not; and no output of a verb, no refusal and no print of
// the options holds the value.
func TestPasswordFileIsReadOnceAndNeverEchoed(t *testing.T) {
	t.Parallel()
	secret, dir := unitSecret(t), t.TempDir()
	file := filepath.Join(dir, "pw")
	writeMode(t, file, secret+"\n", 0o600)
	la := sourceApp(t, map[string]string{"NOVA_SPRINT_REDIS": "127.0.0.1:6380", "NOVA_SPRINT_REDIS_USER": "sprinter", passwordFileEnv: file})

	o, getenv, err := la.a.storeOptions("127.0.0.1:6380")
	require.NoError(t, err)
	r, err := redisconn.Resolve(o, getenv)
	require.NoError(t, err)
	assert.Equal(t, "sprinter", r.User)
	assert.Equal(t, secret, getenv(r.PasswordEnv), "the password reaches redisconn through getenv")
	assert.False(t, strings.Contains(fmt.Sprintf("%v %+v %#v", o, r, la.a.sourceRead), secret), "the options and the holder never print it")
	assert.Empty(t, la.env[sourcePassword], "the environment never holds it")

	// the file changed, and then made unreadable to the check: the value read stays
	writeMode(t, file, "another-"+secret, 0o644)
	o, getenv, err = la.a.storeOptions("127.0.0.1:6380")
	require.NoError(t, err, "read once: the file is not read again")
	assert.Equal(t, secret, getenv(o.PasswordEnv))

	// a verb's whole output, and a refusal over the same file read fresh
	code, out, errs := la.do("init --readers reader-a --members m1")
	require.Equal(t, 0, code, errs)
	require.Len(t, la.dials, 1)
	assert.Equal(t, dialedLogin{"127.0.0.1:6380", "sprinter", secret}, la.dials[0])
	assert.False(t, strings.Contains(out+errs, secret), "no output holds it")

	fresh := sourceApp(t, map[string]string{"NOVA_SPRINT_REDIS": "127.0.0.1:6380", passwordFileEnv: file})
	code, out, errs = fresh.do("where")
	assert.Equal(t, 2, code, "a 0644 file is refused at the store's open")
	assert.Contains(t, errs, "chmod 600 "+file)
	assert.False(t, strings.Contains(out+errs, secret), "the refusal holds no byte of it")
	assert.Empty(t, fresh.dials, "no store is dialed without the password")
}

// Two sources at once is a refusal, never a guess; the keychain source needs the user
// and reads through the app's keychain reader (a test's here: no test runs
// /usr/bin/security), once.
func TestPasswordSourcesAreOneAtATime(t *testing.T) {
	t.Parallel()
	secret, dir := unitSecret(t), t.TempDir()
	file := filepath.Join(dir, "pw")
	writeMode(t, file, secret, 0o600)

	both := sourceApp(t, map[string]string{passwordFileEnv: file, passwordKeychainEnv: "nova-sprint-store"})
	_, _, err := both.a.storeOptions("127.0.0.1:6380")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both set")

	withEnv := sourceApp(t, map[string]string{passwordFileEnv: file, "NOVA_SPRINT_REDIS_PASSWORD_ENV": "FAKE_STORE_PASSWORD"})
	_, _, err = withEnv.a.storeOptions("127.0.0.1:6380")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOVA_SPRINT_REDIS_PASSWORD_ENV and a password source")

	noUser := sourceApp(t, map[string]string{passwordKeychainEnv: "nova-sprint-store"})
	_, _, err = noUser.a.storeOptions("127.0.0.1:6380")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs NOVA_SPRINT_REDIS_USER")

	kc := sourceApp(t, map[string]string{passwordKeychainEnv: "nova-sprint-store", "NOVA_SPRINT_REDIS_USER": "sprinter"})
	var asked []string
	kc.a.keychain = func(service, account string) (secrets.Secret, error) {
		asked = append(asked, service+"/"+account)
		return secrets.NewSecret(secret), nil
	}
	for range 2 {
		o, getenv, err := kc.a.storeOptions("127.0.0.1:6380")
		require.NoError(t, err)
		assert.Equal(t, secret, getenv(o.PasswordEnv))
	}
	assert.Equal(t, []string{"nova-sprint-store/sprinter"}, asked, "the keychain is read once, for the service and the ACL user")

	gone := sourceApp(t, map[string]string{passwordKeychainEnv: "nova-sprint-store", "NOVA_SPRINT_REDIS_USER": "sprinter"})
	gone.a.keychain = func(service, account string) (secrets.Secret, error) { return secrets.Secret{}, errors.New("no item") }
	_, _, err = gone.a.storeOptions("127.0.0.1:6380")
	require.Error(t, err, "a keychain without the item is a refusal, never a login without a password")
}
