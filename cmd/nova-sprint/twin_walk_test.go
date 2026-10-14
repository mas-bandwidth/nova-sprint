package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/onboarding"
)

// twin_walk_test.go pins the order the no-Redis walkthrough teaches: the
// card is given its brief by the add that admits it (--brief-file, before
// the first tick deals it), so the worker the take hands it to is briefed
// from the packet, and the help says in one sentence that a twin member
// added is up at the next tick, never that it is down until it beats. The
// docs/TESTS.md `### First run` transcript is the same walkthrough the help
// shows (twinWords, twinSteps), and its commands run as written, one
// process a verb, over one mem twin file, through the package's rig.

func TestTwinWalkBriefsTheCardBeforeTheFirstTickAndSaysTheMemberIsUp(t *testing.T) {
	t.Parallel()

	// the transcript's add line carries a brief, and the file it names is
	// the one the walkthrough ships with
	add := twinSteps[1]
	require.True(t, strings.HasPrefix(add, prog+" add "), "the walkthrough's second command is the add: %q", add)
	args, err := onboarding.SplitShell(strings.TrimPrefix(add, prog+" "))
	require.NoError(t, err)
	i := indexOf(args, "--brief-file")
	require.GreaterOrEqual(t, i, 0, "the add line carries a brief: %q", add)
	require.NotEqual(t, "", args[i+1], "the add line names the brief's file")
	_, err = os.Stat(args[i+1])
	require.NoError(t, err, "the brief the add names is in the tree: %s", args[i+1])

	// its text says the twin member is up at the next tick, and never that
	// it is down until it beats
	words := twinWords()
	assert.Contains(t, words, "added is up from the next tick", "one sentence says the member is up at the next tick")
	assert.NotContains(t, words, "down until it beats", "the twin walkthrough never says a member is down until it beats")

	// the docs/TESTS.md transcript is the one in twinWords(): command for
	// command, the flow the help shows
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "TESTS.md"))
	require.NoError(t, err)
	lines, err := onboarding.FirstRun(string(raw), prog)
	require.NoError(t, err)
	steps, err := onboarding.Steps(prog, lines)
	require.NoError(t, err)
	walk := twinSteps[:len(twinSteps)-2] // to the merge; the last two are the clock's
	require.Len(t, steps, len(walk), "the transcript holds the walkthrough's commands")
	for i, s := range steps {
		require.Equal(t, "$ "+walk[i], s.Line, "command %d of the transcript", i+1)
		assert.Contains(t, words, "  "+walk[i]+"\n", "the help shows command %d of the transcript", i+1)
	}

	// the walkthrough's commands run as written, over one mem twin, one
	// process a verb: the add briefs the card before the first tick deals it
	file := filepath.Join(t.TempDir(), "sprint.twin")
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:" + file, "NOVA_SPRINT_ACTOR": "boss"}
	var took string
	for _, l := range walk {
		a := newApp(func(k string) string { return env[k] })
		var out, errb bytes.Buffer
		code := runInto(a, args0(l), &out, &errb)
		a.close()
		require.Equal(t, 0, code, "%s\n%s%s", l, out.String(), errb.String())
		if strings.Contains(l, " add ") {
			assert.NotContains(t, out.String(), "the cards have no brief", "the add briefs its card")
		}
		if strings.Contains(l, " take ") {
			took += out.String()
		}
	}
	assert.Contains(t, took, "  brief:\n", "the packet briefs the card the take hands the worker")
	assert.NotContains(t, took, "  notes: none\n    report it:", "a briefed card is never handed out with notes: none")
}

// args0 splits one walkthrough line the way a shell would hand the tool.
func args0(line string) []string {
	args, err := onboarding.SplitShell(strings.TrimPrefix(line, prog+" "))
	if err != nil {
		panic(err)
	}
	return args
}

// indexOf is the index of want in s, or -1.
func indexOf(s []string, want string) int {
	for i, v := range s {
		if v == want {
			return i
		}
	}
	return -1
}
