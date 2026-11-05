package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// A friend's sprint card (docs/SPEC-SPRINT.md section 1; the owner, 2026-10-03: "Could we
// try expressing the work left for nova-tools-1.1.0 into cards, and doing it via the
// sprint, but doing parts on friends where we would normally do friend work."): a card
// whose brief says WHO: friend <name> is dealt by the tick to her row, and she takes and
// finishes it herself. Origin's tip of every branch is landHead (an injected tip: no
// socket), unless a test says otherwise.

const landHead = "0123456789abcdef0123456789abcdef01234567"

// friendRepo is the repository the card's REPO: line names, as the tip is asked of it.
var friendRepo = swarm.CardRepoURL("mas-bandwidth/nova-tools")

// tipIs is a tip that is at for every branch of friendRepo.
func tipIs(t *testing.T, at string) tipFn {
	return func(_ context.Context, repo, _ string) (string, error) {
		assert.Equal(t, friendRepo, repo, "the tip is read of the card's repository")
		return at, nil
	}
}

// friendCardApp is a running sprint with the friends given, each beating, a card s1-1
// whose brief says WHO: <who>, and the root their working directories are under.
func friendCardApp(t *testing.T, who string, friends ...string) (*testApp, string) {
	t.Helper()
	ta, _ := friendApp(t, friends...)
	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	ta.ok("friend sync")
	for _, f := range friends {
		ta.beatUp(f)
	}
	brief := filepath.Join(t.TempDir(), "s1-1.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: "+who)), 0o644))
	ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	ta.ok("start")
	return ta, root
}

// outboxReport writes the friend's REPORT.md of the job.
func outboxReport(t *testing.T, root, friend, job, report string) {
	t.Helper()
	dir := filepath.Join(root, friend+"-working", "outbox", job)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "REPORT.md"), []byte(report), 0o644))
}

// A twin's verbs beat its machines, never a friend's row: a twin holding a friend's card
// opens as before.
func TestATwinBeatsNoFriendRow(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "friend amy", "amy")
	ta.ok("tick")
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	require.NoError(t, ta.a.beatTwin(context.Background(), st))
}

// The stored view sprint draws no friend's row, as where does not: her row is hidden in
// the fleet table, and every machine's row is drawn.
func TestTheStoredViewHidesAFriendsRow(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "friend amy", "amy")
	ta.ok("tick")
	shapes, err := ta.m.Shapes(context.Background(), []string{sprint.Names{}.Table(sprint.Fleet)})
	require.NoError(t, err)
	hidden := map[string]bool{}
	for _, r := range shapes[0].Rows {
		hidden[r.Key] = r.Hidden
	}
	assert.Equal(t, map[string]bool{"m1": false, "m2": false, sprint.FriendRow("amy"): true}, hidden)
}

func TestAddHoldsTheWhoLineToTheFriendsTable(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync")
	add := func(who string) (int, string) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "c1.md"), []byte(passingBrief("c1: a card\nWHO: "+who)), 0o644))
		code, _, errs := ta.do("add --stream s1 --brief-dir " + dir)
		return code, errs
	}
	code, errs := add("friend nobody")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "WHO: friend nobody, and nobody is no row of the friends table (friends: amy)")
	code, errs = add("only friend nobody")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "nobody is no row")
	code, errs = add("machine")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "WHO: machine is not `friend` or `friend <name>`")
	code, _ = add("friend amy")
	assert.Equal(t, 0, code)
}

// A friend's later attempt starts from the current tip of the card's base branch, as a member's
// rework is staged (nova-tools#5215): she carries the last pushed attempt's work onto it herself,
// and the Head she reports is on that tip; a first attempt's brief says nothing of it.
func TestAFriendsReworkStartsFromTheTipOfItsBase(t *testing.T) {
	t.Parallel()
	p := sprint.Packet{Card: "s1-1.w3", Epoch: 0, Attempt: 3, Branch: "sprint/s1-1.w3.g1.e0", BaseHead: landHead, BaseAttempt: 2,
		Brief: "s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy\n\nThe task.", Fix: "assert the bound"}
	text := friendBrief("amy", p)
	assert.Contains(t, text, "This attempt starts from the current tip of sprint/s1 on origin, never from an older base: fetch it and start your branch there. "+
		"Carry the work of attempt 2 onto it yourself: its head, "+landHead+", is the last pushed by any attempt before this one (`git diff origin/sprint/s1..."+landHead+"` shows that work); where it does not apply cleanly, redo it. "+
		"The Head you finish with must be origin's tip of your branch; the attempt is expected to start from the tip named above.\n")
	assert.Equal(t, "THE ONE THING LEFT: assert the bound", strings.Split(text, "\n")[1], "a reworked attempt's fix is its first line after STATUS")
	assert.Contains(t, text, "\nThe carried work: attempt 2's head "+landHead+", carried onto sprint/s1-1.w3.g1.e0 from the tip of its base;")
	assert.NotContains(t, text, "start from it.", "never the old head")

	p.BaseHead, p.BaseAttempt = "", 0
	p.Brief = "s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\n\nThe task."
	assert.Contains(t, friendBrief("amy", p), "the current tip of the repository's default branch on origin, never from an older base: fetch it and start your branch there. No attempt before this one pushed work to carry. The Head you finish with must be origin's tip of your branch; the attempt is expected to start from the tip named above.\n")

	p.Attempt = 1
	assert.NotContains(t, friendBrief("amy", p), "This attempt starts")
}

// The brief a friend receives for a later attempt says, in the present tense, that the Head
// she finishes with must be origin's tip of her branch and that the attempt is expected to
// start from the tip named above; it claims no descent check and never says descends
// (docs/SPEC-CARD-CONTRACT.md, where a rework starts; nova-tools#5215).
func TestTheFriendBriefSaysWhereTheAttemptStarts(t *testing.T) {
	t.Parallel()
	p := sprint.Packet{Card: "s1-1.w3", Epoch: 0, Attempt: 3, Branch: "sprint/s1-1.w3.g1.e0", BaseHead: landHead, BaseAttempt: 2,
		Brief: "s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy\n\nThe task."}
	text := friendBrief("amy", p)
	assert.Contains(t, text, "The Head you finish with must be origin's tip of your branch", "the brief names the tip-equality rule")
	assert.Contains(t, text, "the attempt is expected to start from the tip named above", "the brief says where the attempt starts")
	assert.NotContains(t, text, "descend", "the brief claims no descent check")
}

// A named friend's configured work restriction is held at admission (docs/SPEC-SPRINT.md
// section 1, a friend's card): a card whose stream or KIND is outside her streams and
// kinds is refused at add and at brief, with the restriction named, so it never sits
// undealable.
func TestAddAndBriefRefuseNamedFriendOutsideRestrictions(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	_, _, err := cfg.Update(context.Background(), config.KindFriend, "amy", map[string]string{"streams": " security* ", "kinds": " fix-red, review "}, "test")
	require.NoError(t, err)
	ta.ok("friend sync")
	brief := passingBrief("s1-1: restricted friend card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\nKIND: fix-red")
	briefPath := filepath.Join(t.TempDir(), "brief.md")
	require.NoError(t, os.WriteFile(briefPath, []byte(brief), 0o644))
	code, _, errs := ta.do("add --stream s1 --one --brief-file " + briefPath)
	assert.NotZero(t, code)
	assert.Contains(t, errs, "streams restriction")

	ta, cfg = friendApp(t, "amy")
	_, _, err = cfg.Update(context.Background(), config.KindFriend, "amy", map[string]string{"streams": "security*"}, "test")
	require.NoError(t, err)
	ta.ok("friend sync")
	ta.ok("add --stream s1 --count 1 --one")
	code, _, errs = ta.do("brief s1-1 --brief-file " + briefPath)
	assert.NotZero(t, code)
	assert.Contains(t, errs, "streams restriction")
}
