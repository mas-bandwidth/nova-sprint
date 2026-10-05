package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprintwire"
	"github.com/mas-bandwidth/nova-sprint/internal/work"
)

// TestWorkVerbsAreNovaSprintVerbs pins the work verbs on nova-sprint.
// repos, issues and roadmap are the program. export and import are the
// round-trip. None of them is built: a run exits 1 and writes nothing.
// work verify is not a verb here. nova-work verify stays on nova-work.
// The design those verbs refuse to run is internal/work.Note.
func TestWorkVerbsAreNovaSprintVerbs(t *testing.T) {
	t.Parallel()
	require.Contains(t, work.Note, work.Unbuilt)
	require.Contains(t, work.Note, `(sprint-program "v1"`)
	require.Contains(t, work.Note, "What stays only in Redis")
	require.Contains(t, work.Note, "When it is written")

	help, errb := workSprint("help")
	require.Empty(t, errb)
	for _, line := range []string{
		"\n  nova-sprint work repos [--tree <file>]\n",
		"\n  nova-sprint work issues [--tree <file>]\n",
		"\n  nova-sprint work roadmap [--tree <file>]\n",
		"\n  nova-sprint work export [--tree <file>]\n",
		"\n  nova-sprint work import [--tree <file>]\n",
	} {
		require.Contains(t, help, line, "nova-sprint help")
	}
	assert.NotContains(t, help, "\n  nova-sprint work verify", "verify stays nova-work's")

	for _, name := range []string{"work repos", "work issues", "work roadmap", "work export", "work import"} {
		h, herr := workSprint(strings.Fields(name + " -h")...)
		require.Empty(t, herr, name)
		first, _, _ := strings.Cut(h, "\n")
		assert.Equal(t, "usage: nova-sprint "+name+" [--tree <file>]", first, name)
		assert.Contains(t, h, "effect: inspection:", name)
		assert.Contains(t, h, work.Unbuilt, name)

		out, errs := workSprint(strings.Fields(name)...)
		assert.Empty(t, out, name)
		assert.Contains(t, errs, "nova-sprint "+name+" REFUSED: "+work.Unbuilt, name)

		// --tree is accepted and not opened: a path that is not there is not an error
		out, errs = workSprint(strings.Fields(name + " --tree /no/such/sprint-program")...)
		assert.Empty(t, out, name)
		assert.Contains(t, errs, "nova-sprint "+name+" REFUSED: "+work.Unbuilt, name)
		assert.NotContains(t, errs, "no such", name)
	}

	// a bare group names its verbs and does not run one
	_, bareErr := workSprint("work")
	assert.Contains(t, bareErr, "its verbs are work repos, work issues, work roadmap, work export, work import")
	groupHelp, groupErr := workSprint("help", "work")
	assert.Empty(t, groupErr)
	assert.Contains(t, groupHelp, "nova-sprint work repos [--tree <file>]")
	assert.Contains(t, groupHelp, "round-trip is not built")

	// the old top-level words are not these verbs, and verify was not folded
	for _, name := range []string{"repos", "issues", "roadmap", "import", "export", "verify"} {
		out, errs := workSprint(name)
		assert.Empty(t, out, name)
		assert.Contains(t, errs, "unknown verb "+name, name)
	}

	// a server named does not take them: they are this machine's tree, and
	// the round-trip is not sent across the wire to be invented there
	a := newApp(func(k string) string {
		if k == ServerEnv {
			return "127.0.0.1:1"
		}
		return ""
	})
	sent := false
	a.forward = func(context.Context, string, ...[]string) ([]sprintwire.Result, error) {
		sent = true
		return nil, assert.AnError
	}
	var out, errb2 bytes.Buffer
	code := a.run([]string{"work", "repos"}, &out, &errb2)
	assert.Equal(t, 1, code, errb2.String())
	assert.False(t, sent, "work repos was sent to the server")
	assert.Empty(t, out.String())
	assert.Contains(t, errb2.String(), work.Unbuilt)
}

func workSprint(args ...string) (string, string) {
	var out, errb bytes.Buffer
	newApp(func(string) string { return "" }).run(args, &out, &errb)
	return out.String(), errb.String()
}
