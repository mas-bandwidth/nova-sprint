package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// friendFixtureDiff is the line the motivating finding added at bb01282b62: `nova-friend
// acceptance` in fleet/seat-friend.yml, when cmd/nova-friend had no acceptance verb.
const friendFixtureDiff = "diff --git a/fleet/seat-friend.yml b/fleet/seat-friend.yml\n" +
	"new file mode 100644\n" +
	"index 0000000..0000000\n" +
	"--- /dev/null\n" +
	"+++ b/fleet/seat-friend.yml\n" +
	"@@ -0,0 +1,2 @@\n" +
	"+plays:\n" +
	"+  - nova-friend acceptance\n"

// friendVerbs is cmd/nova-friend's verb table at bb01282b62, before acceptance existed.
func friendVerbs(tool string) []string {
	if tool == "friend" {
		return []string{"ping", "pong", "sync"}
	}
	return nil
}

func TestCalledVerbsKnownYesRefusesAnUnregisteredVerb(t *testing.T) {
	t.Parallel()
	got := calledVerbs(friendFixtureDiff, friendVerbs)
	require.Len(t, got, 1, "calledVerbs = %+v, want one finding", got)
	assert.Equal(t, "fleet/seat-friend.yml", got[0].File, "finding = %s:%d", got[0].File, got[0].Line)
	assert.Equal(t, 2, got[0].Line, "finding = %s:%d", got[0].File, got[0].Line)
	for _, want := range []string{"acceptance", "ping", "pong", "sync"} {
		assert.Contains(t, got[0].Why, want, "finding names not the verb and the tool's known verbs: %s", got[0].Why)
	}
}

func TestCalledVerbsKnownNoPassesARegisteredVerb(t *testing.T) {
	t.Parallel()
	registered := func(tool string) []string {
		if tool == "friend" {
			return []string{"acceptance", "ping", "pong", "sync"}
		}
		return nil
	}
	assert.Empty(t, calledVerbs(friendFixtureDiff, registered), "calledVerbs = %+v, want none", calledVerbs(friendFixtureDiff, registered))
}

// TestCalledVerbsIgnoresWhatDoesNotRunATool pins the gate: a line that only names a tool
// — a Go comment, an error string, a shell built-in, a Markdown prose line whose first
// token is a tool — does not run it, so a word after the name is not read as a verb. Each
// table refuses anything but "where", so the test fails if the prose line is read at all.
func TestCalledVerbsIgnoresWhatDoesNotRunATool(t *testing.T) {
	t.Parallel()
	only := func(tool string) []string { return []string{"where"} }
	cases := []struct {
		name  string
		diff  string
		verbs func(string) []string
		why   string
	}{
		{
			name:  "another tool and a shell built-in",
			verbs: friendVerbs,
			diff: "diff --git a/fleet/seat-friend.yml b/fleet/seat-friend.yml\n" +
				"new file mode 100644\n" +
				"index 0000000..0000000\n" +
				"--- /dev/null\n" +
				"+++ b/fleet/seat-friend.yml\n" +
				"@@ -0,0 +1,3 @@\n" +
				"+plays:\n" +
				"+  - nova-config apply\n" +
				"+  - ls -la\n",
			why: "nova-config is another tool, ls a shell built-in",
		},
		{
			name:  "a prose line that names a tool",
			verbs: only,
			diff: "diff --git a/pkg/config/loop.go b/pkg/config/loop.go\n" +
				"--- a/pkg/config/loop.go\n" +
				"+++ b/pkg/config/loop.go\n" +
				"@@ -1,2 +1,4 @@\n" +
				"+// a loop row ran `nova-sprint table` for days after nova-sprint lost it\n" +
				"+\treturn fmt.Errorf(\"nova-sprint must be run from the repo root\")\n" +
				"+func f() {\n",
			why: "both lines name a tool, neither runs it",
		},
		{
			name:  "a prose line that begins with a tool",
			verbs: only,
			diff: "diff --git a/docs/GETTING-STARTED.md b/docs/GETTING-STARTED.md\n" +
				"--- a/docs/GETTING-STARTED.md\n" +
				"+++ b/docs/GETTING-STARTED.md\n" +
				"@@ -1,2 +1,3 @@\n" +
				"+nova-sprint is free and open source; it runs a card's checks.\n",
			why: "the prose line names a tool, it does not run it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, calledVerbs(tc.diff, tc.verbs), "calledVerbs, want none (%s)", tc.why)
		})
	}
}

// TestCalledVerbsReadsADocCommandBlock pins the gate's positive side: a Markdown fenced
// code block is a doc command block, so a line inside it that runs a tool with a verb is
// checked against the tool's table.
func TestCalledVerbsReadsADocCommandBlock(t *testing.T) {
	t.Parallel()
	diff := "diff --git a/docs/CLI.md b/docs/CLI.md\n" +
		"--- a/docs/CLI.md\n" +
		"+++ b/docs/CLI.md\n" +
		"@@ -1,2 +1,4 @@\n" +
		"+```sh\n" +
		"+nova-sprint table\n" +
		"+```\n"
	only := func(tool string) []string { return []string{"where"} }
	got := calledVerbs(diff, only)
	require.Len(t, got, 1, "calledVerbs = %+v, want one finding (the fenced block runs nova-sprint table)", got)
	assert.Equal(t, "docs/CLI.md", got[0].File, "finding = %s:%d", got[0].File, got[0].Line)
	assert.Equal(t, 2, got[0].Line, "finding = %s:%d", got[0].File, got[0].Line)
}

// TestCalledVerbsRefusesTheMotivatingFriendCall exercises the lander's own table
// (registeredVerbs, what land.go wires in): nova-friend registers no acceptance at the
// recorded head, so the motivating line is refused through the real wiring, not a
// synthetic callback.
func TestCalledVerbsRefusesTheMotivatingFriendCall(t *testing.T) {
	t.Parallel()
	got := calledVerbs(friendFixtureDiff, registeredVerbs)
	require.Len(t, got, 1, "calledVerbs = %+v, want one finding through registeredVerbs", got)
	assert.Contains(t, got[0].Why, "acceptance", "finding names not acceptance and nova-friend's known verbs: %s", got[0].Why)
	assert.Contains(t, got[0].Why, "wait-pong", "finding names not acceptance and nova-friend's known verbs: %s", got[0].Why)
}
