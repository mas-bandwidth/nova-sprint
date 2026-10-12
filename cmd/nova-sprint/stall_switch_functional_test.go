//go:build functional

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// --- the hook-gate race on a server switch (#99) ---

// TestStallASwitchedServerNeverRollsBackOnTheHookGate is #99 with the real binary: server
// switch --rollback --window restarts the server, the restart drops the seat's hook, and the
// new server's first land pass, run as the coordinator before the seat hooks in again, was
// refused by the hook gate and counted as a land fault inside the window, so the v1.2.12
// switch rolled itself back 1.5 s after it began. The switched binary runs `run --land` on a
// store with a card queued to merge and no seat hooked in: its land passes go by, the
// switch is never rolled back, and the server keeps running.
func TestStallASwitchedServerNeverRollsBackOnTheHookGate(t *testing.T) {
	t.Parallel()
	r := newStallRig(t)
	r.do("init", "--readers", "reader-a,reader-b", "--members", "m1:1")
	r.do("add", "--stream", "s1", "--count", "1", "--one")
	r.beat("m1")
	r.do("start")
	r.tick() // presence: the members up
	r.tick() // the deal
	require.Equal(t, []string{"s1-1"}, r.work("m1"))
	r.toMerging([]string{"reader-a", "reader-b"}, "s1-1")
	// seed a route and the sprint git_identity, as nova-config apply writes on every live store
	r.do("apply", "--redis", r.addr, "--routes", "flash-a", "--git-identity", "nova-sprint <nova-sprint@mas-bandwidth.com>")

	// the switch: target is the server's binary, its previous build marked so a rollback shows
	bin, err := os.ReadFile(r.bin)
	require.NoError(t, err)
	dir := t.TempDir()
	target, candidate := filepath.Join(dir, "nova-sprint"), filepath.Join(dir, "nova-sprint-candidate")
	const prevMark = "\nthe previous build\n"
	require.NoError(t, os.WriteFile(target, append(append([]byte{}, bin...), prevMark...), 0o755))
	require.NoError(t, os.WriteFile(candidate, bin, 0o755))
	require.NoError(t, sprint.ServerSwitch(context.Background(), sprint.ServerSwitchOptions{
		Binary: candidate, Target: target, Rollback: true, Window: 15 * time.Minute,
	}))

	// the restarted server, the seat not hooked in: two land passes (LandEvery is 2 s)
	srv := r.startServer(target, "coordinator", "run", "--land")
	word, exited := srv.until(15*time.Second, "SERVER ROLLBACK", "LAND ")
	require.False(t, exited, "the switched server keeps running:\n%s", srv.text())
	require.NotEmpty(t, word, "a land pass ran:\n%s", srv.text())
	srv.until(2*LandEvery+time.Second, "SERVER ROLLBACK")
	assert.NotContains(t, srv.text(), "SERVER ROLLBACK", "a land the hook gate refused is no land fault:\n%s", srv.text())
	now, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.False(t, bytes.HasSuffix(now, []byte(prevMark)), "the switch is not rolled back to the previous build")
	assert.True(t, bytes.Equal(now, bin), "the target is still the candidate")
	_, exited = srv.until(0)
	assert.False(t, exited, "the switched server is still running:\n%s", srv.text())
}
