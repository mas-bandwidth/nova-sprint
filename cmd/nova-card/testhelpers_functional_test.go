//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/goenv"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// syncedBuf is a bytes.Buffer safe for the server and hook children, which write
// from several goroutines at once.
type syncedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncedBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncedBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// builtSprint is how this package's functional test runs nova-sprint verbs when
// it needs the real binary: it builds it once, starts the test's own Redis, and
// runs every verb, the server and the hook through a scrubbed environment with
// --redis <the test's own store>, so a verb can never reach a live sprint server
// however the machine is set up (forward.go). It is the counterpart of
// cmd/nova-sprint's in-process helper for a test that lives outside that package.
type builtSprint struct {
	t     *testing.T
	bin   string
	env   []string
	store string
}

// newBuiltSprint builds the real nova-sprint binary, starts the test's own Redis
// (with the function library loaded), and makes the helper: an environment with
// no NOVA_* variable from the machine, a home and config directory of the test's
// own, and a cache directory short enough for the hook's unix socket (hookSocket
// puts it at <cache>/nova-sprint/hook-<actor>.sock), so the seat record is the
// test's and there is no named server. One store serves the whole test.
func newBuiltSprint(t *testing.T) *builtSprint {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "nova-sprint")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "../nova-sprint")
	build.Env = goenv.Clean(os.Environ())
	out, err := build.CombinedOutput()
	require.NoError(t, err, "building nova-sprint: %s", out)
	// the store is the test's own redis: the built binary arms the hook gate for
	// every name (hook.go), and proving the seat needs the sprint's server, which
	// run --listen refuses on a twin.
	store := testutil.Start(t)
	rc := redis.NewClient(&redis.Options{Addr: store})
	t.Cleanup(func() { _ = rc.Close() })
	require.NoError(t, fn.Load(context.Background(), rc))
	home := t.TempDir()
	cache := shortCache(t)
	env := append(withoutNOVA(goenv.Clean(os.Environ())),
		"NOVA_SPRINT_REDIS="+store,
		"NOVA_SPRINT_ACTOR=coordinator",
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+cache,
	)
	return &builtSprint{t: t, bin: bin, env: env, store: store}
}

// run runs a verb on the built binary through the helper: the verb first, then
// --redis <the test's own store>, so the verb runs on the test's store and never
// forwards, and it returns the verb's exit code and combined output.
func (s *builtSprint) run(args ...string) (int, string) {
	s.t.Helper()
	argv := append(args, "--redis", s.store)
	cmd := exec.Command(s.bin, argv...)
	cmd.Env = s.env
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), b.String()
	}
	require.NoError(s.t, err, "nova-sprint %s", strings.Join(args, " "))
	return 0, b.String()
}

// serve starts the sprint's server on a loopback port and waits until it is
// listening, so the hook and the seat's proof have a server to reach; it returns
// the server's address. The server stays live for the rest of the test.
func (s *builtSprint) serve() string {
	s.t.Helper()
	addr := net.JoinHostPort("127.0.0.1", testutil.FreePort(s.t))
	serverEnv := append(s.env, "NOVA_SPRINT_SERVER="+addr)
	server := exec.Command(s.bin, "run", "--listen", addr, "--redis", s.store)
	server.Env = serverEnv
	buf := &syncedBuf{}
	server.Stdout, server.Stderr = buf, buf
	require.NoError(s.t, server.Start())
	s.t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	require.Eventually(s.t, func() bool {
		return strings.Contains(buf.String(), "SERVER listening")
	}, 30*time.Second, 50*time.Millisecond, "the server did not listen")
	return addr
}

// hook starts the seat's hook against the server at addr and returns its buffer,
// which the caller reads after its wait for the challenge. The hook holds the
// seat's subscription for the rest of the test.
func (s *builtSprint) hook(addr string) *syncedBuf {
	s.t.Helper()
	hookEnv := append(s.env, "NOVA_SPRINT_SERVER="+addr)
	hook := exec.Command(s.bin, "hook", "--redis", s.store)
	hook.Env = hookEnv
	buf := &syncedBuf{}
	hook.Stdout, hook.Stderr = buf, buf
	require.NoError(s.t, hook.Start())
	s.t.Cleanup(func() { _ = hook.Process.Kill(); _ = hook.Wait() })
	return buf
}

// shortCache is the cache directory handed to the built binary: short enough that
// the hook's unix socket (<cache>/nova-sprint/hook-<actor>.sock) stays under the
// platform's path limit, wherever the checkout sits. os.TempDir (=$TMPDIR) is deep
// on this sandbox and overflows a unix socket's path, and /tmp, /dev/shm and
// /var/tmp are not writable here, so the directory is made under the first short
// writable base that holds the socket: /tmp, then the GOCACHE directory's parent,
// then os.TempDir, each checked before it is used. The bound is twinSocketMax, the
// codebase's conservative unix-socket path limit (macOS holds fewer than Linux).
func shortCache(t *testing.T) string {
	t.Helper()
	for _, base := range []string{"/tmp", gocacheParent(), os.TempDir()} {
		if base == "" {
			continue
		}
		dir, err := os.MkdirTemp(base, "nsc")
		if err != nil {
			continue
		}
		sock := filepath.Join(dir, "nova-sprint", "hook-coordinator.sock")
		if !filepath.IsAbs(dir) || len(sock) > 100 {
			_ = os.RemoveAll(dir)
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	t.Fatal("no directory short enough for the hook's unix socket could be made")
	return ""
}

// gocacheParent is the directory that holds the build cache (os.Getenv("GOCACHE")),
// the one short writable location on this sandbox, or "" when it is unset or not
// absolute.
func gocacheParent() string {
	dir := os.Getenv("GOCACHE")
	if dir == "" {
		return ""
	}
	parent := filepath.Dir(dir)
	if !filepath.IsAbs(parent) {
		return ""
	}
	return parent
}

// withoutNOVA is the environment minus every NOVA_* variable: no variable from
// the machine (a server named by NOVA_SPRINT_SERVER, a seat credential, a
// NOVA_* provider key) reaches the built binary. The values are never read.
func withoutNOVA(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(name, "NOVA_") {
			continue
		}
		out = append(out, entry)
	}
	return out
}
