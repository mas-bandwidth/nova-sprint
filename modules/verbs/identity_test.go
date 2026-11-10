package verbs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestIdentityVerb: the coordinator sets the sprint's commit identity with a
// reason; the same identity again changes nothing; no flags prints it; a
// value a shell or git would misread, an email that is not one address, a
// missing reason or a missing identity is refused naming the fix; a worker
// may not set it.
func TestIdentityVerb(t *testing.T) {
	t.Parallel()
	set := func(name, email string) []string {
		return []string{"identity", "--name", name, "--email", email, "--reason", "the allowlisted account"}
	}
	runCases(t, []tcase{
		{name: "set", as: "carol", args: set("Other Bot", "2+other@users.noreply.example.test"), exit: 0,
			want: []string{"IDENTITY Other Bot <2+other@users.noreply.example.test> applied at seq 11; every take from now carries it",
				`git config user.name "Other Bot"`, `git config user.email "2+other@users.noreply.example.test"`, "next: nova-sprint check"}},
		{name: "the same", as: "carol", args: set(testIdentity.Name, testIdentity.Email), exit: 0, want: []string{"IDENTITY already Sprint Bot <1+sprint-bot@users.noreply.example.test>; nothing changed"}},
		{name: "show", as: "carol", args: []string{"identity"}, exit: 0, want: []string{"IDENTITY Sprint Bot <1+sprint-bot@users.noreply.example.test>", "finish refuses a head with a commit by any other author or committer email"}},
		{name: "show with none", as: "carol", args: []string{"identity"}, exit: 1, snap: func(s *store.Snapshot) { s.Identity = store.Identity{} },
			want: []string{"the sprint has no commit identity", `nova-sprint identity --name "<git user.name>"`}},
		{name: "a quote", as: "carol", args: set(`Bot "x"`, "a@b"), exit: 1, want: []string{"a name or email holds a quote"}},
		{name: "two at signs", as: "carol", args: set("Bot", "a@b@c"), exit: 1, want: []string{"the email a@b@c is not one address"}},
		{name: "no email", as: "carol", args: []string{"identity", "--name", "Bot", "--reason", "x"}, exit: 1, want: []string{"an identity needs a name and an email"}},
		{name: "no reason", as: "carol", args: []string{"identity", "--name", "Bot", "--email", "a@b"}, exit: 1, want: []string{"an identity change needs its reason"}},
		{name: "a worker", as: "alice", args: set("Bot", "a@b"), exit: 1, want: []string{"coordinator"}},
	})
	w := newWorld(t)
	w.ok(t, "carol", set("Other Bot", "2+other@users.noreply.example.test")...)
	if got := w.snap(t).Identity; got != (store.Identity{Name: "Other Bot", Email: "2+other@users.noreply.example.test"}) {
		t.Fatalf("identity %+v", got)
	}
}

// TestTakeCarriesTheIdentity: the receipt carries the sprint's commit
// identity for the worker, and take's text gives a friend the two git config
// lines and the By:/Co-Authored-By: rule; with no identity take refuses.
func TestTakeCarriesTheIdentity(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	var got takeReceipt
	if err := json.Unmarshal([]byte(w.ok(t, "alice", "take", "p1.w1@1", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.CommitName != testIdentity.Name || got.CommitEmail != testIdentity.Email {
		t.Fatalf("receipt identity %q <%q>", got.CommitName, got.CommitEmail)
	}
	want(t, w.do("alice", "take", "p1.w1@1"), 0, "Every commit is authored and committed as Sprint Bot <1+sprint-bot@users.noreply.example.test>",
		`git config user.email "1+sprint-bot@users.noreply.example.test"`, `"By: <your name>"`)
	s := sprint()
	s.Identity = store.Identity{}
	want(t, worldFrom(t, s).do("alice", "take", "p1.w1@1"), 1, "SPRINT REFUSED take: the sprint has no commit identity")
}

// TestFinishRefusesAnotherIdentity: finish reads the head's commits since the
// base and refuses one authored or committed by another email, naming each
// commit and both emails, with the two git config lines and the rewrite as
// the fix; the identity's email in another case passes; a head with no
// commit beyond the base and the stream's base is nothing done; a log that
// cannot be read is the edge's failure.
func TestFinishRefusesAnotherIdentity(t *testing.T) {
	t.Parallel()
	mine := git.Commit{SHA: strings.Repeat("1", 40), AuthorEmail: testIdentity.Email, CommitterEmail: strings.ToUpper(testIdentity.Email)}
	theirs := git.Commit{SHA: strings.Repeat("2", 40), AuthorEmail: "aurora@example.test", CommitterEmail: testIdentity.Email}
	committed := git.Commit{SHA: strings.Repeat("3", 40), AuthorEmail: testIdentity.Email, CommitterEmail: "alice@nova-sprint.local"}
	finish := []string{"finish", "p1.w1@1", "--head", head}
	commits := func(cs ...git.Commit) func(*testing.T, *world) {
		return func(t *testing.T, w *world) { takeW1(t, w); w.origin.commits = cs }
	}
	runCases(t, []tcase{
		{name: "all the identity's", as: "alice", args: finish, exit: 0, prep: commits(mine), want: []string{"FINISHED p1.w1"}},
		{name: "another author and committer", as: "alice", args: finish, exit: 1, prep: commits(mine, theirs, committed),
			want: []string{"2 of the 3 commits on sprint/s/p1 since the base are not made as Sprint Bot <1+sprint-bot@users.noreply.example.test>",
				"222222222222 (author aurora@example.test, committer 1+sprint-bot@users.noreply.example.test)",
				"333333333333 (author 1+sprint-bot@users.noreply.example.test, committer alice@nova-sprint.local)",
				`in the clone run git config user.name "Sprint Bot" and git config user.email "1+sprint-bot@users.noreply.example.test"`,
				"git rebase --rebase-merges " + base + " --exec 'git commit --amend --no-edit --reset-author'", "git push --force-with-lease origin sprint/s/p1"}},
		{name: "no commit beyond the base", as: "alice", args: finish, exit: 1, prep: commits([]git.Commit{}...),
			want: []string{"head " + head + " has no commit beyond " + base + " and origin/main; nothing was done", "commit the work on sprint/s/p1, push it, and finish with its head"}},
		{name: "the log unreadable", as: "alice", args: finish, exit: 2,
			prep: func(t *testing.T, w *world) { takeW1(t, w); w.origin.commitsE = errBroken }, want: []string{"the commits of " + head + " could not be read in a clone of o/r"}},
	})
}
