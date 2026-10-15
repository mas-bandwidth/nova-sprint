//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/goenv"
)

// builtSprint is how this package's functional test runs nova-sprint verbs when
// it needs the real binary: it builds it once, starts the test's own Redis, and
// runs every verb through a scrubbed environment with
// --redis <the test's own store>, so a verb can never reach a live sprint server
// however the machine is set up (forward.go). It is the counterpart of
// cmd/nova-sprint's in-process helper for a test that lives outside that package.
type builtSprint struct {
	t     *testing.T
	bin   string
	env   []string
	store string
}

// newBuiltSprint builds the real binary and starts an isolated functional store,
// with no host NOVA_* variables and a temporary home and configuration.
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
	store := testutil.Start(t)
	rc := redis.NewClient(&redis.Options{Addr: store})
	t.Cleanup(func() { _ = rc.Close() })
	require.NoError(t, fn.Load(context.Background(), rc))
	home := t.TempDir()
	env := append(withoutNOVA(goenv.Clean(os.Environ())),
		"NOVA_SPRINT_REDIS="+store,
		"NOVA_BUS_REDIS="+store,
		"NOVA_SPRINT_ACTOR=coordinator",
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
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

// proveCoordinator gives the isolated functional store the record that nova-hook owns.
// This test exercises admission, not the heartbeat or delivery adapters.
func (s *builtSprint) proveCoordinator() {
	s.t.Helper()
	ctx := context.Background()
	rc := redis.NewClient(&redis.Options{Addr: s.store})
	defer rc.Close()
	store := hook.Store{C: rc}
	_, _, err := store.HookAs(ctx, "coordinator", hook.Coordinator, "fixture")
	require.NoError(s.t, err)
	holder := hook.Holder{Store: store, Name: "coordinator", Role: hook.Coordinator, Pushes: hook.PushesOf(hook.Coordinator), By: "fixture"}
	require.NoError(s.t, holder.Start(ctx))
	require.NoError(s.t, holder.Beat(ctx, map[hook.Push]bool{hook.Bus: true, hook.Sprint: true}))
	now, err := rc.Time(ctx).Result()
	require.NoError(s.t, err)
	require.NoError(s.t, rc.HSet(ctx, hook.ServeKey, "at", now.UnixMilli(), "every", hook.BeatEvery.Milliseconds(), "by", "fixture").Err())
	require.NoError(s.t, rc.HSet(ctx, hook.RecordKey("coordinator"), "answered_at", now.UnixMilli()).Err())
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
