package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
)

// The server-record fence: a hand land cannot run while the server lands
// (docs/fixes.sexp, land-fenced-while-server-lands: the clone lock exists, the
// missing half is this fence; docs/SPEC-SPRINT.md, "One land at a time works
// in a clone", whose other half the clone lock is). On 2026-10-04 a land by
// hand ran beside the server's run --land and left the cache clone dirty. On
// the twin store: the server's heartbeat says it lands and names its process;
// a land by hand beside it is refused naming the server, before any clone is
// locked or touched, and a dry run is not; the server's own land loop is never
// refused; a server that does not land, or a landing server's record naming a
// pid on this host that is gone, fences nothing; one on another host does; a
// record older than store.ServerTTL is no server, and land runs.
func TestLandRefusesWhileTheServerLands(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "change.txt", "change\n")}, "s1-1")
	ctx := context.Background()
	st, err := r.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	host, _ := os.Hostname()

	// the server's heartbeat: its actor, --land and its process
	srv := &app{now: r.a.now, serverLands: true}
	srv.sayServer(ctx, st, time.Time{}, io.Discard)
	rec, ok, err := st.Server(ctx)
	require.NoError(t, err)
	require.True(t, ok, "the heartbeat writes a fresh record")
	assert.Equal(t, store.ServerRecord{Actor: sprint.MachineActor, At: r.a.now(), Land: true, PID: os.Getpid(), Host: host}, rec)

	before, applies := r.git(r.remote, "rev-parse", "main"), r.applies()
	code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "the server is running with --land")
	assert.Contains(t, errs, fmt.Sprintf("pid=%d", os.Getpid()))
	assert.Contains(t, errs, "actor="+sprint.MachineActor)
	assert.Equal(t, before, r.git(r.remote, "rev-parse", "main"), "nothing pushed")
	assert.Equal(t, applies, r.applies(), "nothing reported")
	assert.NoFileExists(t, filepath.Join(r.clone, ".git", landLockName), "refused before any clone is locked")
	r.ok("land --repo-dir " + r.clone + " --base main --dry-run")

	// the server's own land loop lands beside its record
	why, err := (&app{landLazy: true}).serverLanding(ctx, st)
	require.NoError(t, err)
	assert.Empty(t, why, "the server's own pass")

	// a server that does not land fences nothing
	(&app{now: r.a.now}).sayServer(ctx, st, time.Time{}, io.Discard)
	why, err = r.a.serverLanding(ctx, st)
	require.NoError(t, err)
	assert.Empty(t, why, "a server without --land")

	// a landing server's record naming a pid on this host that is gone
	child := subproc.Prepare(ctx, time.Minute, "sh", "-c", "exit 0")
	defer child.Cancel()
	require.NoError(t, child.Cmd.Run())
	dead := child.Cmd.Process.Pid
	require.NoError(t, st.SetServer(ctx, store.ServerRecord{Actor: "stella", Land: true, PID: dead, Host: host}))
	why, err = r.a.serverLanding(ctx, st)
	require.NoError(t, err)
	assert.Empty(t, why, "a dead server on this host")

	// a landing server on another host cannot be seen to be gone
	require.NoError(t, st.SetServer(ctx, store.ServerRecord{Actor: "stella", Land: true, PID: dead, Host: "elsewhere.invalid"}))
	why, err = r.a.serverLanding(ctx, st)
	require.NoError(t, err)
	assert.Contains(t, why, "host=elsewhere.invalid")

	// past store.ServerTTL the record is no server: land runs
	r.a.sleep(store.ServerTTL + time.Second)
	r.ok("land --repo-dir " + r.clone + " --base main")
	assert.NotEqual(t, before, r.git(r.remote, "rev-parse", "main"))
}
