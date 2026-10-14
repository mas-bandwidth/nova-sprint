package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// Every verb's -h carries its own exit table (verbExits), and that table names
// only the codes the verb's code can return: 0 (done) and 2 (usage) are the
// codes every verb returns on the paths below, and 3 (or more) is a per-verb
// code, stated in the verb's own table and never in the one shared banner line
// (docs/STANDARD.md section 2, "Every verb's -h quotes the table, or the verb's
// own"). The shared line once named code 3 with three meanings at once (fleet
// sync, friend sync, run); it must name only the common codes. The test drives
// each verb's done and usage paths, and the paths that return 3 or more, and
// fails when a verb's table omits a code a path returned or names a code no
// path returns.
func TestEveryVerbsExitTableNamesOnlyCodesItReturns(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	// The banner's one shared line names only the common codes. Code 3 (and any
	// code above 2) means different things per verb, so it must not ride on the
	// shared line the whole tool prints.
	for _, code := range []int{3, 4} {
		assert.NotContains(t, exitLine, " "+strconv.Itoa(code)+" ",
			"the shared banner line names code %d for the whole tool, but it is a per-verb code", code)
	}

	// Every verb's -h prints its own table and exits 0 (the done path), and a
	// usage error exits 2 (the usage path). The table names both codes.
	for _, v := range verbs {
		v := v
		t.Run(v.name, func(t *testing.T) {
			line := verbExits(v.name)
			code, out, errs := ta.do(v.name + " -h")
			require.Equal(t, 0, code, "%s -h: %d %q %q", v.name, code, out, errs)
			require.Contains(t, out, line, "%s -h drops its own exit table %q", v.name, line)
			code, _, _ = ta.do(v.name + " --no-such-flag")
			require.Equal(t, 2, code, "%s --no-such-flag: %d", v.name, code)
			assert.Contains(t, line, "0 ", "%s: table %q names no 0", v.name, line)
			assert.Contains(t, line, "2 ", "%s: table %q names no 2", v.name, line)
		})
	}

	// Drive the paths that return 3 or more, and hold each verb's table to the
	// codes its code returns: a table that omits a code a path returned, or
	// names a code no path returns, is wrong.
	extra := driveExtraExitCodes(t)
	for _, v := range verbs {
		line := verbExits(v.name)
		for _, code := range []int{3, 4} {
			digit := " " + strconv.Itoa(code) + " "
			if extra[v.name][code] {
				assert.Contains(t, line, digit, "%s returns %d but its table %q omits it", v.name, code, line)
			} else {
				assert.NotContains(t, line, digit, "%s names %d but no path returns it", v.name, code)
			}
		}
	}
}

// driveExtraExitCodes drives each verb's path that returns 3 or more and returns
// the codes each verb returned, by verb name.
func driveExtraExitCodes(t *testing.T) map[string]map[int]bool {
	t.Helper()
	extra := map[string]map[int]bool{}
	add := func(name string, code int) {
		if extra[name] == nil {
			extra[name] = map[int]bool{}
		}
		extra[name][code] = true
	}

	// fleet sync: the config cannot be read
	ta, inv := syncApp(t)
	inv.fail = errors.New("connection refused")
	code, _, _ := ta.do("fleet sync")
	require.Equal(t, 3, code)
	add("fleet sync", code)

	// friend sync: the config holds no friend row
	ta2, _ := friendApp(t)
	code, _, _ = ta2.do("friend sync")
	require.Equal(t, 3, code)
	add("friend sync", code)

	// friend clean: the config holds no friend row
	ta3 := newTestApp(t)
	ta3.a.friends = friendRows()
	dir := t.TempDir()
	code, _, _ = ta3.do("friend clean --root " + dir)
	require.Equal(t, 3, code)
	add("friend clean", code)

	// collect: the config cannot be read
	ta4 := newTestApp(t)
	ta4.ok("init --readers reader-a,reader-b --members m1")
	ta4.a.friends = func(context.Context, string) ([]config.Row, error) { return nil, errors.New("connection refused") }
	code, _, _ = ta4.do("collect")
	require.Equal(t, 3, code)
	add("collect", code)

	// brief check: the store did not answer
	ta5 := newTestApp(t)
	dir5 := t.TempDir()
	writeBriefFile(t, dir5, "c", passingBrief("RESULT: c sha=0123456789ab tier: pro\nTHE TASK. Fix."))
	code, _, _ = ta5.bare("brief check --stream s1 " + dir5)
	require.Equal(t, 3, code)
	add("brief check", code)

	// run: its binary replaced (3), and three wedged ticks in a row (4)
	add("run", runReplacedCode(t))
	add("run", runWedgedCode(t))

	// dashboard: its binary replaced (3)
	add("dashboard", dashboardReplacedCode(t))

	return extra
}

// runReplacedCode drives run's binary-replaced path and returns its exit code.
func runReplacedCode(t *testing.T) int {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 3")
	ta.ok("start")
	exe := filepath.Join(t.TempDir(), "nova-sprint")
	require.NoError(t, os.WriteFile(exe, []byte("the build the loop began with"), 0o755))
	ta.a.executable = func() (string, error) { return exe, nil }
	st, _, code := ta.a.machineVerb("run", nil, &bytes.Buffer{})
	require.NotNil(t, st, "run: %d", code)
	ta.a.ticked = func(n int, _ time.Time, _ string) {
		if n == 1 {
			require.NoError(t, os.WriteFile(exe, []byte("the build installed under it, longer"), 0o755))
		}
	}
	var out, errb bytes.Buffer
	require.True(t, ta.a.runLoop(context.Background(), st, 20, 5, &out, &errb), "a replaced binary stops the loop")
	return exitReplaced
}

// runWedgedCode drives run's wedged-ticks path and returns its exit code.
func runWedgedCode(t *testing.T) int {
	t.Helper()
	l := newDeadlineLoop(t)
	s := newAfterScript("deadline", "further", "stop")
	l.ta.a.after = s.after
	l.ta.a.tickFn = func(ctx context.Context, _ *store.Store) (store.TickResult, error) {
		l.tick()
		return s.deaf(ctx)
	}
	var out, errb bytes.Buffer
	l.ta.a.runLoop(context.Background(), l.st, 20, 10, &out, &errb)
	require.Equal(t, []int{exitTickDeadline}, l.exits, "three wedged ticks in a row exit 4")
	return exitTickDeadline
}

// fakeListener is a listener that accepts nothing, for a dashboard whose page
// is never asked: the test holds the listener in place of net.Listen.
type fakeListener struct{ addr net.Addr }

func (f *fakeListener) Accept() (net.Conn, error) { return nil, errors.New("closed") }
func (f *fakeListener) Close() error              { return nil }
func (f *fakeListener) Addr() net.Addr            { return f.addr }

// dashboardReplacedCode drives dashboard's binary-replaced path and returns its
// exit code.
func dashboardReplacedCode(t *testing.T) int {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	exe := filepath.Join(t.TempDir(), "nova-sprint")
	require.NoError(t, os.WriteFile(exe, []byte("the build the dashboard began with"), 0o755))
	ta.a.executable = func() (string, error) { return exe, nil }
	ta.a.dashListen = func(network, addr string) (net.Listener, error) {
		return &fakeListener{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	ta.a.notify = func(context.Context) (context.Context, context.CancelFunc) { return ctx, cancel }
	// the first pause installs a new build under the running dashboard
	ta.a.sleep = func(d time.Duration) {
		require.NoError(t, os.WriteFile(exe, []byte("the build installed under it, longer"), 0o755))
	}
	var out, errb bytes.Buffer
	code := ta.a.cmdDashboard([]string{"--listen", "127.0.0.1:0", "--pull", "none", "--every", "1s"}, &out, &errb)
	require.Equal(t, exitReplaced, code, "%s%s", out.String(), errb.String())
	return exitReplaced
}
