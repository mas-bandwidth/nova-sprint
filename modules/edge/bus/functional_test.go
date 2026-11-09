package bus

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findNovaBusBinary_internal(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("nova-bus"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	cand := filepath.Join(home, ".local", "bin", "nova-bus")
	if _, err := os.Stat(cand); err == nil {
		return cand
	}
	if os.Getenv("CI") != "" {
		t.Fatal("nova-bus required: missing on a CI runner (not on PATH or in ~/.local/bin)")
	}
	t.Skip("nova-bus binary not found on PATH or ~/.local/bin")
	return ""
}

func freeLocalPort_internal(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startThrowawayRedis_internal(t *testing.T) string {
	t.Helper()
	redisBin, err := exec.LookPath("redis-server")
	if err != nil && os.Getenv("CI") != "" {
		t.Fatal("redis-server required: missing on a CI runner")
	} else if err != nil {
		t.Skip("redis-server not installed")
	}
	port := freeLocalPort_internal(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cmd := exec.Command(redisBin,
		"--port", fmt.Sprintf("%d", port),
		"--bind", "127.0.0.1",
		"--save", "",
		"--appendonly", "no",
		"--dir", t.TempDir(),
	)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return addr
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("redis-server at %s did not start in time", addr)
	return ""
}

func startHookProcess_internal(t *testing.T, novaBusBin, redisAddr, name, execCmd string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, novaBusBin, "hook", "--as", name, "--exec", execCmd)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CODEX_THREAD_ID=") && !strings.HasPrefix(kv, "NOVA_BUS_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "NOVA_BUS_REDIS="+redisAddr)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		cancel()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
}

func TestFunctionalNovaBusWhoAndSend(t *testing.T) {
	t.Parallel()
	novaBusBin := findNovaBusBinary_internal(t)
	redisAddr := startThrowawayRedis_internal(t)
	inboxFile := filepath.Join(t.TempDir(), "inbox.txt")

	startHookProcess_internal(t, novaBusBin, redisAddr, "sprint", "/bin/true")
	startHookProcess_internal(t, novaBusBin, redisAddr, "amd-1.ds",
		fmt.Sprintf("cat >> %q", inboxFile))

	ctx := context.Background()
	b := New(Config{
		Bin:      novaBusBin,
		Sender:   "sprint",
		Env:      []string{"NOVA_BUS_REDIS=" + redisAddr},
		AllowNew: true,
	})

	// Wait until both hooks register in `nova-bus who`.
	require.Eventually(t, func() bool {
		who, err := b.Who(ctx)
		return err == nil && who["sprint"].Name == "sprint" && who["amd-1.ds"].Name == "amd-1.ds"
	}, 5*time.Second, 50*time.Millisecond, "expected sprint and amd-1.ds in nova-bus who")

	id, err := b.Send(ctx, "amd-1.ds", "DEAL", "DEAL: 1 card for amd-1.ds (epoch 1)")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	require.Eventually(t, func() bool {
		raw, readErr := os.ReadFile(inboxFile)
		return readErr == nil && strings.Contains(string(raw), "DEAL: 1 card for amd-1.ds (epoch 1)")
	}, 5*time.Second, 50*time.Millisecond, "expected amd-1.ds hook to receive the message")
}
