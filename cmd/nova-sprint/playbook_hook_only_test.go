package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/docs"
)

// playbook_hook_only_test.go holds docs/COORDINATOR-PLAYBOOK.md to the
// hook-only friend model: the nova-friend daemons were retired on 2026-10-17
// and a friend is reached by nova-hook alone, so the page names no line that
// installs or runs one (section 2, "Running a friend row, the coordinator's
// own included").

// retiredFriendDaemonRe names a verb that installs or runs a nova-friend daemon.
// `nova-friend help run` reads a verb's help and is not refused.
var retiredFriendDaemonRe = regexp.MustCompile(`nova-friend\s+(install|run)\b`)

// retiredFriendDaemonLines is every place in text naming a retired daemon
// verb, each with the line it starts on. The whole text is scanned, so a
// wrap between "nova-friend" and its verb is still found.
func retiredFriendDaemonLines(text string) []string {
	var found []string
	for _, m := range retiredFriendDaemonRe.FindAllStringIndex(text, -1) {
		line := strings.Count(text[:m[0]], "\n") + 1
		found = append(found, strconv.Itoa(line)+": "+strings.Join(strings.Fields(text[m[0]:m[1]]), " "))
	}
	return found
}

// The playbook names no `nova-friend install` or `nova-friend run` line. The
// witnesses run the same check on lines that must be refused and on lines
// that must pass, so a check that refuses nothing, or everything, fails here.
func TestPlaybookNamesNoRetiredFriendDaemon(t *testing.T) {
	t.Parallel()

	t.Run("the page", func(t *testing.T) {
		for _, l := range retiredFriendDaemonLines(docs.CoordinatorPlaybook) {
			t.Errorf("%s:%s names a retired nova-friend daemon verb (retired 2026-10-17; friends are reached by nova-hook alone)", playbookPath, l)
		}
		assert.Contains(t, docs.CoordinatorPlaybook, "## 2. Running a friend row, the coordinator's own included", "section 2 keeps its heading")
		assert.Contains(t, docs.CoordinatorPlaybook, "--role friend", "section 2 hooks the friend in with nova-hook")
	})

	t.Run("the witnesses", func(t *testing.T) {
		for _, bad := range []string{
			"3. Install the daemon. usage: `nova-friend install --as <me> --harness <h> --dir <d>`.",
			"4. Let launchd run the daemon. usage: `nova-friend run --as <me> --harness <h> --dir <d>`.",
			"the directory a lane runs with, and nova-friend run refuses a friend without it",
			"`nova-friend  install --dry-run`",
			"and nova-friend\n   run refuses a friend without it",
			"the daemon (nova-friend\ninstall writes it)",
		} {
			assert.NotEmpty(t, retiredFriendDaemonLines("a\n"+bad+"\nb"), "the line is not refused: %s", bad)
		}
		assert.Equal(t, []string{"2: nova-friend run"}, retiredFriendDaemonLines("a\n`nova-friend run --as f1`\nb"), "the refusal names its line")
		assert.Equal(t, []string{"2: nova-friend install"}, retiredFriendDaemonLines("a\nb nova-friend\n  install c"), "a wrapped refusal names the line it starts on")
		for _, good := range []string{
			"usage: `nova-hook hook --as <me> --role <role> [--once] [--harness <name>] [--redis <addr>] [--dry-run]`",
			"(`nova-friend help run`, `nova-friend help resume`)",
			"`nova-friend check`, `nova-friend reach --as <coordinator> --to <friend>`",
			"`nova-friend runner` and `nova-friend installed` are other words",
		} {
			assert.Empty(t, retiredFriendDaemonLines(good), "the line is refused: %s", good)
		}
	})
}
