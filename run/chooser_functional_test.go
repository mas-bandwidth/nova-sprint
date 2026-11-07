package run

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func chooserTestPG(t *testing.T) string {
	t.Helper()
	initdb, err := exec.LookPath("initdb")
	if err != nil {
		t.Skip("initdb required")
	}
	dir := filepath.Join(t.TempDir(), "data")
	out, err := exec.Command(initdb, "-D", dir, "-U", "nova", "--auth=trust", "--no-locale").CombinedOutput()
	require.NoError(t, err, "%s", out)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	ctl := filepath.Join(filepath.Dir(initdb), "pg_ctl")
	opts := fmt.Sprintf("-p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=off", port)
	out, err = exec.Command(ctl, "-D", dir, "-l", filepath.Join(dir, "server.log"), "-o", opts, "-w", "start").CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() { _ = exec.Command(ctl, "-D", dir, "-m", "immediate", "-w", "stop").Run() })
	return fmt.Sprintf("postgres://nova@127.0.0.1:%d/postgres?sslmode=disable", port)
}
func TestFunctionalChooserExclusionAndLoss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := chooserTestPG(t)
	h := &fakeHook{lease: &fakeLease{}}
	first, err := OpenChooser(ctx, dsn, "", "studio", 4121, h)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close(ctx) })
	second, err := OpenChooser(ctx, dsn, "", "bench", 4122, &fakeHook{lease: &fakeLease{}})
	require.Nil(t, second)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "studio (pid 4121, since "), "%v", err)
	require.NoError(t, first.Check(ctx))
	_, err = first.session.(*pgChooser).conn.Exec(ctx, "SELECT pg_advisory_unlock($1::integer, $2::integer)", chooserClass, chooserObject)
	require.NoError(t, err)
	require.ErrorContains(t, first.Check(ctx), "LOCK LOST")
	require.NoError(t, first.Close(ctx))
	third, err := OpenChooser(ctx, dsn, "", "bench", 4122, &fakeHook{lease: &fakeLease{}})
	require.NoError(t, err)
	require.NoError(t, third.Close(ctx))
}

func TestFunctionalHookFailureReleasesLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := chooserTestPG(t)
	_, err := OpenChooser(ctx, dsn, "", "first", 11, &fakeHook{err: errors.New("name held")})
	require.ErrorContains(t, err, "sprint is already hooked")
	next, err := OpenChooser(ctx, dsn, "", "second", 12, &fakeHook{lease: &fakeLease{}})
	require.NoError(t, err)
	require.NoError(t, next.Close(ctx))
}

func TestFunctionalRestoreAndChooserShareLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := chooserTestPG(t)
	restore, err := OpenRestoreLock(ctx, dsn, "", "bench", 20)
	require.NoError(t, err)
	chooser, err := OpenChooser(ctx, dsn, "", "other", 21, &fakeHook{lease: &fakeLease{}})
	require.Nil(t, chooser)
	require.ErrorContains(t, err, "restore bench 20")
	require.NoError(t, restore.Close(ctx))
	chooser, err = OpenChooser(ctx, dsn, "", "other", 21, &fakeHook{lease: &fakeLease{}})
	require.NoError(t, err)
	blocked, err := OpenRestoreLock(ctx, dsn, "", "bench", 20)
	require.Nil(t, blocked)
	require.ErrorContains(t, err, "SPRINT REFUSED restore: run chooses on other (pid 21")
	require.ErrorContains(t, err, ": stop it first")
	require.NoError(t, chooser.Close(ctx))
}

func TestFunctionalInspectChooserIsReadOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := chooserTestPG(t)
	status, err := InspectChooser(ctx, dsn, "")
	require.NoError(t, err)
	require.False(t, status.Held)
	lock, err := OpenChooser(ctx, dsn, "", "studio", 4121, &fakeHook{lease: &fakeLease{}})
	require.NoError(t, err)
	status, err = InspectChooser(ctx, dsn, "")
	require.NoError(t, err)
	require.True(t, status.Held)
	require.Equal(t, "run", status.Operation)
	require.Equal(t, "studio", status.Host)
	require.Equal(t, 4121, status.PID)
	require.Greater(t, status.BackendPID, 0)
	require.False(t, status.Since.IsZero())
	require.NoError(t, lock.Check(ctx))
	require.NoError(t, lock.Close(ctx))
	restore, err := OpenRestoreLock(ctx, dsn, "", "bench", 51)
	require.NoError(t, err)
	status, err = InspectChooser(ctx, dsn, "")
	require.NoError(t, err)
	require.Equal(t, "restore", status.Operation)
	require.Equal(t, "bench", status.Host)
	require.Equal(t, 51, status.PID)
	require.NoError(t, restore.Close(ctx))
}

func TestFunctionalChooserCloseReleasesBeforeReturning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := chooserTestPG(t)
	first, err := OpenChooser(ctx, dsn, "", "first", 11, &fakeHook{lease: &fakeLease{}})
	require.NoError(t, err)
	require.NoError(t, first.Close(ctx))
	second, err := OpenChooser(ctx, dsn, "", "second", 12, &fakeHook{lease: &fakeLease{}})
	require.NoError(t, err)
	require.NoError(t, second.Close(ctx))
}
