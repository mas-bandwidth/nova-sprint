package run

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/pgbin"
	"github.com/stretchr/testify/require"
)

// testDigest is a well-formed dump digest for the restore tests.
const testDigest = "0000000000000000000000000000000000000000000000000000000000000000"

// testBus is a throwaway Redis and the nova-bus on PATH, for the
// functional tier only.
type testBus struct {
	bin string
	env []string
}

func (b testBus) command(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, b.bin, args...)
	c.Env = b.env
	return c
}

// edge is the production bus edge on this fixture.
func (b testBus) edge() *bus.Edge { return bus.New(bus.Config{Bin: b.bin, Env: b.env}) }

func testRedisBus(t *testing.T) testBus {
	t.Helper()
	redis := pgbin.Program(t, "redis-server")
	bin := pgbin.Program(t, "nova-bus")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	server := exec.Command(redis, "--bind", "127.0.0.1", "--port", fmt.Sprint(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	server.Stdout, server.Stderr = io.Discard, io.Discard
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "NOVA_BUS_") && !strings.HasPrefix(v, "CODEX_THREAD_ID=") && !strings.HasPrefix(v, "XDG_STATE_HOME=") && !strings.HasPrefix(v, "XDG_CONFIG_HOME=") {
			env = append(env, v)
		}
	}
	env = append(env, fmt.Sprintf("NOVA_BUS_REDIS=127.0.0.1:%d", port), "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
	b := testBus{bin: bin, env: env}
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		return b.command(ctx, "who").Run() == nil
	}, 5*time.Second, 25*time.Millisecond)
	return b
}

func hookTestName(t *testing.T, b testBus, name, handler string) {
	t.Helper()
	cmd := b.command(context.Background(), "hook", "--as", name, "--exec", handler)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		out, err := b.command(ctx, "who").Output()
		return err == nil && strings.Contains(string(out), name+"\t")
	}, 5*time.Second, 25*time.Millisecond)
}
