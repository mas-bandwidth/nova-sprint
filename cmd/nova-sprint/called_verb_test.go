package main

import (
	"strings"
	"testing"
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
	got := calledVerbs(friendFixtureDiff, friendVerbs)
	if len(got) != 1 {
		t.Fatalf("calledVerbs = %+v, want one finding", got)
	}
	f := got[0]
	if f.File != "fleet/seat-friend.yml" || f.Line != 2 {
		t.Fatalf("finding = %s:%d, want fleet/seat-friend.yml:2", f.File, f.Line)
	}
	for _, want := range []string{"acceptance", "ping", "pong", "sync"} {
		if !strings.Contains(f.Why, want) {
			t.Fatalf("finding names not the verb and the tool's known verbs: %s", f.Why)
		}
	}
}

func TestCalledVerbsKnownNoPassesARegisteredVerb(t *testing.T) {
	registered := func(tool string) []string {
		if tool == "friend" {
			return []string{"acceptance", "ping", "pong", "sync"}
		}
		return nil
	}
	if got := calledVerbs(friendFixtureDiff, registered); len(got) != 0 {
		t.Fatalf("calledVerbs = %+v, want none", got)
	}
}

func TestCalledVerbsIgnoresAnotherToolAndAShellBuiltIn(t *testing.T) {
	diff := "diff --git a/fleet/seat-friend.yml b/fleet/seat-friend.yml\n" +
		"new file mode 100644\n" +
		"index 0000000..0000000\n" +
		"--- /dev/null\n" +
		"+++ b/fleet/seat-friend.yml\n" +
		"@@ -0,0 +1,3 @@\n" +
		"+plays:\n" +
		"+  - nova-config apply\n" +
		"+  - ls -la\n"
	if got := calledVerbs(diff, friendVerbs); len(got) != 0 {
		t.Fatalf("calledVerbs = %+v, want none (nova-config is another tool, ls a shell built-in)", got)
	}
}

// TestCalledVerbsIgnoresAProseLineThatNamesATool pins the gate: a line that only names a
// tool — a Go comment or an error string, a loop's note — does not run it, so a word after
// the name is not read as a verb. The table here refuses anything but "where", so the test
// fails if the prose line is read at all.
func TestCalledVerbsIgnoresAProseLineThatNamesATool(t *testing.T) {
	diff := "diff --git a/pkg/config/loop.go b/pkg/config/loop.go\n" +
		"--- a/pkg/config/loop.go\n" +
		"+++ b/pkg/config/loop.go\n" +
		"@@ -1,2 +1,4 @@\n" +
		"+// a loop row ran `nova-sprint table` for days after nova-sprint lost it\n" +
		"+\treturn fmt.Errorf(\"nova-sprint must be run from the repo root\")\n" +
		"+func f() {\n"
	only := func(tool string) []string { return []string{"where"} }
	if got := calledVerbs(diff, only); len(got) != 0 {
		t.Fatalf("calledVerbs = %+v, want none (both lines name a tool, neither runs it)", got)
	}
}

// TestCalledVerbsRefusesTheMotivatingFriendCall exercises the lander's own table
// (registeredVerbs, what land.go wires in): nova-friend registers no acceptance at the
// recorded head, so the motivating line is refused through the real wiring, not a
// synthetic callback.
func TestCalledVerbsRefusesTheMotivatingFriendCall(t *testing.T) {
	got := calledVerbs(friendFixtureDiff, registeredVerbs)
	if len(got) != 1 {
		t.Fatalf("calledVerbs = %+v, want one finding through registeredVerbs", got)
	}
	if !strings.Contains(got[0].Why, "acceptance") || !strings.Contains(got[0].Why, "wait-pong") {
		t.Fatalf("finding names not acceptance and nova-friend's known verbs: %s", got[0].Why)
	}
}
