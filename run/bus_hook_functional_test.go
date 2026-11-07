package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFunctionalNovaBusAdapter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := testRedisBus(t)
	redis := ""
	for _, v := range fixture.env {
		if strings.HasPrefix(v, "NOVA_BUS_REDIS=") {
			redis = strings.TrimPrefix(v, "NOVA_BUS_REDIS=")
		}
	}
	path := filepath.Join(t.TempDir(), "received")
	hookTestName(t, fixture, "alice", `printf '%s\t%s\n' "$NOVA_BUS_ID" "$NOVA_BUS_BODY" >> `+shellWord(path))
	bus, err := NewNovaBus(BusConfig{Binary: fixture.bin, Redis: redis, ReplyExecutable: "/usr/bin/true"})
	require.NoError(t, err)
	lease, err := bus.Acquire(ctx, "sprint")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	require.NoError(t, lease.Check(ctx))
	receipt, err := bus.Send(ctx, "alice", "DEAL", "take a card")
	require.NoError(t, err)
	require.Equal(t, "alice", receipt.Recipient)
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(b), receipt.ID+"\ttake a card")
	}, 5*time.Second, 25*time.Millisecond)
	reply, err := bus.ReplyTo(ctx, "alice", receipt.ID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(b), reply.ID+"\t"+"replies are verbs: run nova-sprint next --as alice")
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, lease.Close(ctx))
	require.Eventually(t, func() bool {
		names, e := bus.Who(ctx)
		if e != nil {
			return false
		}
		for _, n := range names {
			if n == "sprint" {
				return false
			}
		}
		return true
	}, 5*time.Second, 25*time.Millisecond)
}

func TestFunctionalHookCallsHiddenReply(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bin := filepath.Join(t.TempDir(), "nova-sprint")
	build := exec.CommandContext(ctx, "go", "build", "-p", "4", "-o", bin, "../cmd/nova-sprint")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "%s", out)
	fixture := testRedisBus(t)
	redis := ""
	for _, v := range fixture.env {
		if strings.HasPrefix(v, "NOVA_BUS_REDIS=") {
			redis = strings.TrimPrefix(v, "NOVA_BUS_REDIS=")
		}
	}
	path := filepath.Join(t.TempDir(), "replies")
	hookTestName(t, fixture, "alice", `printf '%s\t%s\t%s\n' "$NOVA_BUS_ID" "$NOVA_BUS_RE" "$NOVA_BUS_BODY" >> `+shellWord(path))
	bus, err := NewNovaBus(BusConfig{Binary: fixture.bin, Redis: redis, ReplyExecutable: bin})
	require.NoError(t, err)
	lease, err := bus.Acquire(ctx, "sprint")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	send := fixture.command(ctx, "send", "--as", "alice", "--to", "sprint", "QUESTION", "-")
	send.Stdin = strings.NewReader("test body is ignored")
	out, err = send.Output()
	require.NoError(t, err)
	parts := strings.Fields(string(out))
	require.Len(t, parts, 2)
	require.Eventually(t, func() bool {
		data, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(data), "\t"+parts[1]+"\t"+"replies are verbs: run nova-sprint next --as alice")
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, lease.Close(ctx))
}
