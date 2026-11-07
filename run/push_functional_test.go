package run

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/require"
)

type testBus struct {
	bin string
	env []string
}

func (b testBus) command(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, b.bin, args...)
	c.Env = b.env
	return c
}
func (b testBus) Send(ctx context.Context, to, subject, body string) (Delivery, error) {
	c := b.command(ctx, "send", "--as", "sprint", "--to", to, subject, "-")
	c.Stdin = strings.NewReader(body)
	out, err := c.Output()
	if err != nil {
		return Delivery{}, err
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return Delivery{}, fmt.Errorf("unexpected bus receipt")
	}
	return Delivery{Recipient: fields[0], ID: fields[1]}, nil
}
func testRedisBus(t *testing.T) testBus {
	t.Helper()
	redis, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server required")
	}
	bus, err := exec.LookPath("nova-bus")
	if err != nil {
		t.Skip("nova-bus required")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	server := exec.Command(redis, "--bind", "127.0.0.1", "--port", fmt.Sprint(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	server.Stdout = io.Discard
	server.Stderr = io.Discard
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "NOVA_BUS_") && !strings.HasPrefix(v, "CODEX_THREAD_ID=") && !strings.HasPrefix(v, "XDG_STATE_HOME=") && !strings.HasPrefix(v, "XDG_CONFIG_HOME=") {
			env = append(env, v)
		}
	}
	env = append(env, "NOVA_BUS_REDIS="+addr, "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
	b := testBus{bin: bus, env: env}
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
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		out, err := b.command(ctx, "who").Output()
		return err == nil && strings.Contains(string(out), name+"\t")
	}, 5*time.Second, 25*time.Millisecond)
}
func TestFunctionalPushRealBusAndPostgres(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := testRedisBus(t)
	hookTestName(t, b, "sprint", "true")
	received := filepath.Join(t.TempDir(), "received")
	hookTestName(t, b, "alice", `printf '%s\t%s\n' "$NOVA_BUS_ID" "$NOVA_BUS_BODY" >> `+shellWord(received))
	pg, err := store.Open(ctx, chooserTestPG(t), "")
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	require.NoError(t, pg.Migrate(ctx))
	pushSeed(t, pg, "a", "deal")
	pushSeed(t, pg, "b", "judgement")
	snap, err := pg.Snapshot(ctx)
	require.NoError(t, err)
	sender := &recordingBus{testBus: b}
	p := NewPusher(pg, noticeMap{"deal": {{Subject: "DEAL", Line: "take"}}, "judgement": {{Subject: "NEEDS YOU", Line: "judge"}}}, sender)
	require.NoError(t, p.Push(ctx, "alice", snap, time.Unix(1000, 0)))
	fresh, err := pg.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), fresh.Cursors["push:alice"])
	require.Len(t, sender.receipts, 2)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(received)
		if err != nil {
			return false
		}
		text := string(data)
		return strings.Contains(text, sender.receipts[0].ID+"\t"+"take") && strings.Contains(text, sender.receipts[1].ID+"\t"+"judge")
	}, 5*time.Second, 25*time.Millisecond)
}

type recordingBus struct {
	testBus
	receipts []Delivery
}

func (b *recordingBus) Send(ctx context.Context, to, subject, body string) (Delivery, error) {
	d, err := b.testBus.Send(ctx, to, subject, body)
	if err == nil {
		b.receipts = append(b.receipts, d)
	}
	return d, err
}
