package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// twinWayOut is the hint the gate appends when the caller has no store to check a
// hook in, or named a mem twin that will not open: without Redis, the mem twin is
// the way (nova-sprint help, "trying it without a Redis"; docs/FIRST-LAP.md).
const twinWayOut = "learning without Redis? use --redis mem:<file> (nova-sprint help twin)"

// TestHookRefusalNamesTheTwinWayOut holds the hook gate's refusal to the reader who
// meant a twin: it keeps its first sentence and exit 1, and names --redis mem:<file>
// as the way out, whether no store was named or the mem twin named did not open.
func TestHookRefusalNamesTheTwinWayOut(t *testing.T) {
	t.Parallel()
	const actor = "twin-way-out"
	hookTests.Store(actor, true)
	t.Cleanup(func() { hookTests.Delete(actor) })

	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("my notes\n"), 0o600))

	for _, tc := range []struct {
		name, redis string
	}{
		{"no store was named", ""},
		{"the twin named has no file", "mem:"},
		{"the file named is not a twin", "mem:" + notes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"NOVA_SPRINT_ACTOR": actor}
			if tc.redis != "" {
				env["NOVA_SPRINT_REDIS"] = tc.redis
			}
			a := newApp(func(k string) string { return env[k] })
			defer a.close()
			var out, errb bytes.Buffer
			// the card's own cold line: a twin's add, the store not named in this
			// process (the reader init'ed the twin, then left --redis off)
			code := runInto(a, []string{"add", "--stream", "s1", "--count", "1", "--one"}, &out, &errb)
			assert.Equal(t, 1, code, "exit %d\n%s", code, errb.String())
			assert.Empty(t, out.String(), "a refusal writes nothing to stdout")
			assert.True(t, strings.HasPrefix(errb.String(), sprint.HookFirst), "the first sentence is unchanged:\n%s", errb.String())
			assert.Contains(t, errb.String(), twinWayOut, "the refusal does not name the twin way out:\n%s", errb.String())
		})
	}
}
