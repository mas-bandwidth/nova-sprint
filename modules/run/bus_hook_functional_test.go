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
	path := filepath.Join(t.TempDir(), "received")
	hookTestName(t, fixture, "alice", `printf '%s\t%s\n' "$NOVA_BUS_ID" "$NOVA_BUS_BODY" >> `+shellWord(path))
	bus := fixture.edge()
	lease, err := SprintHook{Bin: fixture.bin, Env: fixture.env, Reply: "/usr/bin/true"}.Acquire(ctx, "sprint")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	require.NoError(t, lease.Check(ctx))
	id, err := bus.Send(ctx, "alice", "DEAL", "take a card")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(b), id+"\ttake a card")
	}, 5*time.Second, 25*time.Millisecond)
	reply, err := bus.Reply(ctx, "alice", id, "REPLY", "replies are verbs: run nova-sprint next --as alice")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(b), reply+"\t"+"replies are verbs: run nova-sprint next --as alice")
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, lease.Close(ctx))
	require.Eventually(t, func() bool {
		names, e := bus.Who(ctx)
		_, held := names["sprint"]
		return e == nil && !held
	}, 5*time.Second, 25*time.Millisecond)
}

func TestFunctionalHookCallsHiddenReply(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bin := filepath.Join(t.TempDir(), "nova-sprint")
	build := exec.CommandContext(ctx, "go", "build", "-p", "4", "-o", bin, "../../cmd/nova-sprint")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "%s", out)
	fixture := testRedisBus(t)
	path := filepath.Join(t.TempDir(), "replies")
	hookTestName(t, fixture, "alice", `printf '%s\t%s\t%s\n' "$NOVA_BUS_ID" "$NOVA_BUS_RE" "$NOVA_BUS_BODY" >> `+shellWord(path))
	lease, err := SprintHook{Bin: fixture.bin, Env: fixture.env, Reply: bin}.Acquire(ctx, "sprint")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	send := fixture.command(ctx, "send", "--as", "alice", "--to", "sprint", "take p1@2", "-") // a verb: pointed at once (a reply or a non-verb is not answered: verbs.BusAnswer)
	send.Stdin = strings.NewReader("test body is ignored")
	out, err = send.Output()
	require.NoError(t, err)
	parts := strings.Fields(string(out))
	require.Len(t, parts, 2)
	require.Eventually(t, func() bool {
		data, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(data), "\t"+parts[1]+"\t"+"the sprint takes no verbs over the bus: run nova-sprint take p1@2 --as alice yourself")
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, lease.Close(ctx))
}
