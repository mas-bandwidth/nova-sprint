package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/onboarding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlobalFlagsBeforeVerb verifies that global flags placed before the verb
// (--redis, --actor, --json, --dry-run, etc.) parse identically to verb-first ordering
// without "unknown verb" refusal (card sprint-accepts-global-flags-before-verb).
func TestGlobalFlagsBeforeVerb(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	twinFile := filepath.Join(dir, "flags.twin")

	// 1. Initialise store with global flags before verb:
	// nova-sprint --actor boss --redis mem:<path> init --readers r1 --members m1:64
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--actor", "boss", "--redis", "mem:" + twinFile, "init", "--readers", "r1", "--members", "m1:64"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "init stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
	}

	// 2. Add with --redis before verb:
	// nova-sprint --redis mem:<path> add --stream s1 --count 1 --one --actor boss
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--redis", "mem:" + twinFile, "add", "--stream", "s1", "--count", "1", "--one", "--actor", "boss"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "add stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
	}

	// 3. Where with --redis before verb:
	// nova-sprint --redis mem:<path> where
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--redis", "mem:" + twinFile, "where"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "where stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
		assert.Contains(t, out.String(), "SPRINT TABLE")
		assert.Contains(t, out.String(), "s1")
	}

	// 4. Where with --redis=<path> before verb:
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--redis=mem:" + twinFile, "where"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "where= stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
		assert.Contains(t, out.String(), "SPRINT TABLE")
		assert.Contains(t, out.String(), "s1")
	}

	// 5. Where with --json and --redis before verb:
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--json", "--redis", "mem:" + twinFile, "where"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "where --json stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
		assert.True(t, strings.HasPrefix(strings.TrimSpace(out.String()), "{"), "expected json object: %s", out.String())
	}

	// 6. Multi-word verb: lane list with --redis before verb:
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--redis", "mem:" + twinFile, "lane", "list"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "lane list stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
	}

	// 7. Tick with --shadow before verb:
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--shadow", "--redis", "mem:" + twinFile, "tick"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "tick --shadow stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
	}

	// 8. Help with --redis before verb:
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--redis", "mem:" + twinFile, "help", "where"}, &out, &errb)
		a.close()
		require.Equal(t, 0, code, "help where stdout:\n%s\nstderr:\n%s", out.String(), errb.String())
		assert.Contains(t, out.String(), "nova-sprint where")
	}

	// 9. Unknown verb after flag reports unknown verb for the verb, not the flag:
	{
		var out, errb bytes.Buffer
		a := newApp(func(string) string { return "" })
		code := runInto(a, []string{"--redis", "mem:" + twinFile, "notaverb"}, &out, &errb)
		a.close()
		require.Equal(t, 2, code)
		assert.Contains(t, errb.String(), "unknown verb notaverb")
		assert.NotContains(t, errb.String(), "unknown verb --redis")
	}
}

// TestTwinRunsWithoutBusHookOrServer verifies that a mem:<path> twin store
// runs cleanly without requiring NOVA_BUS_REDIS, nova-sprint hook, or NOVA_SPRINT_SERVER
// (card sprint-twin-runs-without-bus-hook-or-server).
func TestTwinRunsWithoutBusHookOrServer(t *testing.T) {
	// Arm hook checks as in production to prove mem: stores bypass hook checks
	hookTests.Store("boss", true)
	defer hookTests.Delete("boss")

	dir := t.TempDir()
	twinFile := filepath.Join(dir, "twin.sprint")

	// Hostile environment: bus points to non-existent port, server points to non-existent port
	env := map[string]string{
		"NOVA_SPRINT_REDIS":  "mem:" + twinFile,
		"NOVA_SPRINT_ACTOR":  "boss",
		"NOVA_SPRINT_SERVER": "127.0.0.1:1",
		"NOVA_BUS_REDIS":     "127.0.0.1:1",
	}

	runCli := func(args ...string) (int, string, string) {
		a := newApp(func(k string) string { return env[k] })
		defer a.close()
		var out, errb bytes.Buffer
		code := runInto(a, args, &out, &errb)
		return code, out.String(), errb.String()
	}

	// 1. Direct CLI calls with --redis flag before verb
	code, out, errs := runCli("--redis", "mem:"+twinFile, "init", "--readers", "reader-m2", "--members", "m1,m2")
	require.Equal(t, 0, code, "init exit %d\nstdout: %s\nstderr: %s", code, out, errs)

	code, out, errs = runCli("--redis", "mem:"+twinFile, "add", "--stream", "s1", "--count", "1", "--one")
	require.Equal(t, 0, code, "add exit %d\nstdout: %s\nstderr: %s", code, out, errs)

	code, out, errs = runCli("--redis", "mem:"+twinFile, "start")
	require.Equal(t, 0, code, "start exit %d\nstdout: %s\nstderr: %s", code, out, errs)

	code, out, errs = runCli("--redis", "mem:"+twinFile, "where")
	require.Equal(t, 0, code, "where exit %d\nstdout: %s\nstderr: %s", code, out, errs)
	assert.Contains(t, out, "SPRINT TABLE")
	assert.Contains(t, out, "s1")

	// 2. Full documented walkthrough from cmd/nova-sprint/verbs.go:255 (twinSteps)
	// executed in an environment with hostile server and bus settings
	walkthroughFile := filepath.Join(dir, "walkthrough.twin")
	env["NOVA_SPRINT_REDIS"] = "mem:" + walkthroughFile
	for _, line := range twinSteps {
		args, err := onboarding.SplitShell(strings.TrimPrefix(line, prog+" "))
		require.NoError(t, err, "split shell line: %s", line)
		code, out, errs = runCli(args...)
		require.Equal(t, 0, code, "%s: exit %d\nstdout: %s\nstderr: %s", line, code, out, errs)
	}
	assert.Contains(t, out, "\nDONE\n", "the walkthrough flow ends with the card landed")
}
