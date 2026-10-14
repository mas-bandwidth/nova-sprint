package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A mem twin (st.ByHand: --redis mem:<file>) runs the coordinator's verbs with
// no seat hook (docs/SPEC-SPRINT.md, "The hook" and "The two pushes"; the twin
// exemption is in cmd/nova-sprint/hook.go, the gate is modelled in
// tla/SeatHook.tla): the gate is armed for the coordinator, yet its verbs run,
// and an omitted actor resolves to the coordinator's name with no hook, because
// a twin is ticked by hand and has no live store to prove a push against. A
// live (non-twin) store with the gate armed still refuses the same verbs with
// the hook's one line.
func TestTwinVerbsRunWithoutTheSeatHook(t *testing.T) {
	t.Parallel()
	const name = "twin-gate"
	file := filepath.Join(t.TempDir(), "sprint.twin")
	hookTests.Store(name, true)
	t.Cleanup(func() { hookTests.Delete(name) })

	run := func(line string) (int, string, string) {
		env := map[string]string{"NOVA_SPRINT_REDIS": "mem:" + file}
		a := newApp(func(k string) string { return env[k] })
		defer a.close()
		var out, errb bytes.Buffer
		code := runInto(a, split(line), &out, &errb)
		return code, out.String(), errb.String()
	}

	// the first init names the coordinator; its twin's later verbs run without
	// hooking in, and without naming an actor each time (the default is the
	// coordinator's name, no hook proven).
	code, out, errs := run("init --readers reader-m2 --members m1,m2 --actor " + name)
	require.Equal(t, 0, code, "init: exit %d\n%s%s", code, out, errs)

	for _, line := range []string{"add --stream s1 --count 1 --one", "start"} {
		code, out, errs = run(line)
		require.Equal(t, 0, code, "%s: exit %d\n%s%s", line, code, out, errs)
		assert.NotContains(t, out+errs, sprint.HookFirst, "%s named the seat hook", line)
	}
	assert.Contains(t, errs, "ACTOR "+name, "the twin's default actor resolved to the coordinator with no hook")

	// a live (non-twin) store still refuses the coordinator's verbs for want of
	// the seat's hook: nothing on a live store changes.
	ta, _ := hookSprint(t, "twin-gate-live")
	ta.refusedFirst("add --stream s1 --count 2", sprint.HookFirst)
}
