package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A friend's beat carries her session's last pong (friend beat --pong), the server takes it
// as a beat's flag, and the tick's coordinator's pass judges her session deaf from it: the
// judgment is in the coordinator's inbox (docs/SPEC-SPRINT.md section 8, "The coordinator's
// pass").
func TestFriendBeatCarriesTheSessionPongAndTheTickJudgesADeafSession(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "friend amy", "amy")
	pong := ta.a.now().Add(-sprint.FriendDeafAfter - time.Minute).UTC().Format(time.RFC3339)
	out := ta.ok("friend beat amy --pong " + pong)
	assert.Contains(t, out, " pong="+pong)
	ta.ok("tick")
	in := ta.ok("inbox")
	assert.Contains(t, in, sprint.NFriendDeaf)
	assert.Contains(t, in, "friend amy")

	code, _, errs := ta.do("friend beat amy --pong yesterday")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--pong wants an RFC3339 time")

	assert.Empty(t, friendBeatReport([]string{"--pong", pong, "--active", pong}), "the server takes both as a beat's flags")
	assert.NotEmpty(t, friendBeatReport([]string{"--pong", "yesterday"}))
}

// set --friend-finish is the friend-finish window the pass judges an idle friend by.
func TestSetFriendFinishIsTheIdleWindow(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "friend amy", "amy")
	out := ta.ok("set --friend-finish 45m")
	assert.Contains(t, out, "friend-finish 45m")
	code, _, errs := ta.do("set --friend-finish soon")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "--friend-finish wants a duration")
	assert.Contains(t, ta.ok("set --friend-finish default"), "friend-finish default (15m0s)")
}

// Verified working in the view (card sn-verified-working-b-ns-bcb.w1; docs/SPEC-SPRINT.md
// section 1, "Verified working"): the friends table draws dealt beside working, and working
// counts only a card a push proves or her beat names running (sprint.FriendWorkingOf), the
// read friend take makes of her (reads.go, a.friendStarted). The reader caught the earlier
// attempt setting the working cell from the fleet working count and never calling
// FriendWorkingOf; this test reads the cell the view draws.
func TestTheFriendsTableCountsDealtBesideVerifiedWorking(t *testing.T) {
	t.Parallel()
	ta, _ := takeApp(t, 1, nil, "amy")
	ta.ok("tick")
	cells := func() map[string]string {
		var w whereView
		ta.json("where", &w)
		c := w.Tables[sprint.Friends]["amy"]
		return map[string]string{"dealt": cellText(c["dealt"]), "working": cellText(c["working"])}
	}

	// a push on the card's branch proves it: dealt and working agree
	ta.a.tip = tipIs(t, landHead)
	assert.Equal(t, map[string]string{"dealt": "1", "working": "1"}, cells(), "a push on its branch proves the card")

	// no push and her beat names nothing: dealt and working differ
	ta.a.tip = func(_ context.Context, _, _ string) (string, error) { return "", nil }
	assert.Equal(t, map[string]string{"dealt": "1", "working": "0"}, cells(), "no push proves it and her beat does not name it")

	// her beat names it running: working counts it with no push
	ta.ok("friend beat amy --running s1-1")
	assert.Equal(t, map[string]string{"dealt": "1", "working": "1"}, cells(), "her beat names it running")
}

// Verified working counts only positive evidence (card sn-verified-working-b-ns-bcc.w1;
// docs/SPEC-SPRINT.md section 1, "Verified working"; the reader: "pass only positive
// evidence (a readable branch push or --running beat) into the working count"). A push
// whose read fails is not proof: the card is dealt to her row and does not count working,
// but friend take keeps the conservative read and leaves the card with her. The earlier
// attempt counted the unreadable push: friendStarted marked the card started and the cell
// took FriendWorkingOf's count of it.
func TestTheWorkingCountTakesOnlyPositiveEvidenceAndTakeKeepsTheCard(t *testing.T) {
	t.Parallel()
	ta, _ := takeApp(t, 1, nil, "amy")
	ta.ok("tick")
	ta.a.tip = func(_ context.Context, _, _ string) (string, error) { return "", errors.New("ls-remote timed out") }

	var w whereView
	ta.json("where", &w)
	c := w.Tables[sprint.Friends]["amy"]
	assert.Equal(t, "1", cellText(c["dealt"]), "the card is dealt to her row in working")
	assert.Equal(t, "0", cellText(c["working"]), "a push that cannot be read is not the positive evidence working counts")

	// the conservative read stays for friend take: the card it cannot read stays with her
	code, _, errs := ta.do("friend take amy s1-1")
	assert.Equal(t, 1, code, "the take refuses the card whose push it cannot read")
	assert.Contains(t, errs, "REFUSED s1-1: s1-1.w1 has started: a push on sprint/s1-1.w1.g1.e0 cannot be read (ls-remote timed out)")
	ta.clean()
}
