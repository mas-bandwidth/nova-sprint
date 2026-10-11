package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// needs set is the coordinator's direct verb for a card's needs
// (docs/SPEC-SPRINT.md, needs): before it the only way to re-point a card's
// needs after admission was to drop it and add it again, a twin.

// landOne lands s1-1, leaving s1-2 ready in the same stream so the stream is
// not archived and the landed card stays on the work table.
func (ta *testApp) landOne() string {
	ta.t.Helper()
	ta.ok("add --one --stream s1 s1-1")
	ta.ok("add --one --stream s1 s1-2")
	ta.deal(1)
	var q struct{ Cards []queueCard }
	ta.json("queue --as m1", &q)
	card := ""
	for _, c := range q.Cards {
		if strings.HasPrefix(c.ID, "s1-") {
			card = c.ID
			break
		}
	}
	require.NotEmpty(ta.t, card, "an s1 card is dealt")
	id := strings.TrimSuffix(card, ".w1")
	ta.ok("take --as m1 " + card + "@1")
	ta.ok("finish --as m1 " + card + "@1")
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 5")
	ta.ok("read --as reader-b --ok --limit 5")
	ta.ok("accept --read-ok")
	ta.ok("merge --stream s1 --batch 1")
	ta.ok("tick")
	require.Equal(ta.t, sprint.Landed, ta.primary(id).Col)
	return id
}

// (i) needs set on a ready card whose need is not landed records the needs and
// moves the card ready -> waiting in one unit, printing the move.
func TestNeedsSetMovesAReadyCardToWaitingOnAnUnlandedNeed(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream a a-1")
	ta.ok("add --one --stream a a-3")
	ta.ok("add --one --stream a a-2")
	require.Equal(t, sprint.Ready, ta.primary("a-2").Col)

	out := ta.ok("needs set a-2 --needs a-1,a-3")
	assert.Contains(t, out, "MOVED a-2")
	assert.Contains(t, out, "-> waiting")
	c := ta.primary("a-2")
	assert.Equal(t, sprint.Waiting, c.Col, "a ready card that gains a need not landed waits")
	assert.Equal(t, "a-1,a-3", c.F("needs"), "the needs are recorded")
	ta.clean()
}

// (ii) needs set on a waiting card whose new needs have all landed moves it to
// ready in one unit.
func TestNeedsSetMovesAWaitingCardToReadyWhenItsNeedsAreLanded(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	landed := ta.landOne()
	ta.ok("add --one --stream a a-3")
	ta.ok("add --one --stream a a-2 --needs a-3")
	require.Equal(t, sprint.Waiting, ta.primary("a-2").Col, "a-2 waits on a-3")

	out := ta.ok("needs set a-2 --needs " + landed)
	assert.Contains(t, out, "MOVED a-2")
	assert.Contains(t, out, "-> ready")
	c := ta.primary("a-2")
	assert.Equal(t, sprint.Ready, c.Col, "a waiting card whose new needs have all landed is ready")
	assert.Equal(t, landed, c.F("needs"))
	ta.clean()
}

// (iii) needs set --clear empties the needs and moves a waiting card to ready.
func TestNeedsSetClearEmptiesTheNeedsAndReadiesAWaitingCard(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream a a-1")
	ta.ok("add --one --stream a a-2 --needs a-1")
	require.Equal(t, sprint.Waiting, ta.primary("a-2").Col)

	out := ta.ok("needs set a-2 --clear")
	assert.Contains(t, out, "MOVED a-2")
	c := ta.primary("a-2")
	assert.Equal(t, "", c.F("needs"), "--clear empties the needs")
	assert.Equal(t, sprint.Ready, c.Col, "a waiting card with nothing to wait on is ready")
	ta.clean()
}

// (iv) a need that is no primary on the table is refused, exit 1, nothing
// written, naming it.
func TestNeedsSetRefusesANeedNotOnTheTable(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream a a-2")
	before := *ta.primary("a-2")

	code, out, errs := ta.do("needs set a-2 --needs zz-9")
	assert.Equal(t, 1, code)
	assert.NotContains(t, out, "MOVED")
	assert.Contains(t, errs, "zz-9", "the refusal names the need")
	assert.Contains(t, errs, "nothing was changed")
	c := ta.primary("a-2")
	assert.Equal(t, before.F("needs"), c.F("needs"), "nothing was written")
	assert.Equal(t, before.Col, c.Col, "nothing moved")
	ta.clean()
}

// (v) a card dealt or working takes the new needs for its next attempt and
// keeps its attempt.
func TestNeedsSetOnAWorkingCardKeepsItsAttempt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream a a-1")
	ta.deal(1)
	require.Equal(t, sprint.Working, ta.primary("a-1").Col, "a-1 is working")
	landed := ta.landOne()

	out := ta.ok("needs set a-1 --needs " + landed)
	assert.Contains(t, out, "MOVED a-1")
	c := ta.primary("a-1")
	assert.Equal(t, sprint.Working, c.Col, "a working card keeps working")
	assert.Equal(t, "1", c.F("attempt"), "its attempt is kept")
	assert.Equal(t, landed, c.F("needs"), "the new needs apply to its next attempt")
	ta.clean()
}

// (vi) a dropped card is refused.
func TestNeedsSetRefusesADroppedCard(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream a a-2")
	ta.ok("drop a-2 --reason 'obsolete'")

	code, out, errs := ta.do("needs set a-2 --clear")
	assert.Equal(t, 1, code)
	assert.NotContains(t, out, "MOVED")
	assert.Contains(t, errs, "a-2")
	assert.Contains(t, errs, "nothing was changed")
	ta.clean()
}

// (vi) a landed card is refused.
func TestNeedsSetRefusesALandedCard(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	landed := ta.landOne()

	code, out, errs := ta.do("needs set " + landed + " --clear")
	assert.Equal(t, 1, code)
	assert.NotContains(t, out, "MOVED")
	assert.Contains(t, errs, "landed")
	assert.Contains(t, errs, "nothing was changed")
}

// (vii) each card's brief DEPENDS-ON: line is rewritten to match the new needs,
// without changing any other line.
func TestNeedsSetRewritesTheBriefsDependsOnLineOnly(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --one --stream a a-1 --brief-file " + writeHeaderBrief(t, dir, "a-1", "-", "internal/a1.go"))
	ta.ok("add --one --stream a a-3 --brief-file " + writeHeaderBrief(t, dir, "a-3", "-", "internal/a3.go"))
	ta.ok("add --one --stream a a-2 --brief-file " + writeHeaderBrief(t, dir, "a-2", "a-1", "internal/a2.go"))
	before := ta.primary("a-2").F("brief")
	require.Contains(t, before, "DEPENDS-ON: a-1\n")

	ta.ok("needs set a-2 --needs a-3")
	c := ta.primary("a-2")
	assert.Equal(t, "a-3", c.F("needs"))
	assert.Equal(t, strings.Replace(before, "DEPENDS-ON: a-1", "DEPENDS-ON: a-3", 1), c.F("brief"),
		"only the DEPENDS-ON: line changed")
	ta.clean()
}
