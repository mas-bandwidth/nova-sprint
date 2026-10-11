package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// needsSetWorld is stream a with a-1 admitted first and ready, ready to be
// named as a need.
func needsSetWorld(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream a a-1 --one --brief-file " + writeBrief(t, "one"))
	return ta
}

// landedNeedWorld is stream a with a-0 landed, ready to be named as a need.
func landedNeedWorld(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream a a-1 --one --brief-file " + writeBrief(t, "one"))
	ta.ok("start")
	ta.landStream("a", []string{""}, []string{""}, []string{""})
	require.Equal(t, sprint.Landed, ta.primary("a-1").Col, "a-1 landed")
	return ta
}

// (i) needs set <id> --needs a,b on a ready card whose a is not landed records
// the needs and moves the card ready -> waiting in one unit, printing the move.
func TestNeedsSetMovesAReadyCardToWaiting(t *testing.T) {
	t.Parallel()
	ta := needsSetWorld(t)
	ta.ok("add --stream a a-2 --one --brief-file " + writeBrief(t, "two"))
	require.Equal(t, sprint.Ready, ta.primary("a-2").Col, "a-2 starts ready")

	out := ta.ok("needs set a-2 --needs a-1")
	assert.Contains(t, out, "a-2", "the move names the card")
	assert.Contains(t, out, "waiting", "the move is printed")

	c := ta.primary("a-2")
	assert.Equal(t, sprint.Waiting, c.Col, "a ready card that gains a need not landed waits")
	assert.Equal(t, "a-1", c.F("needs"), "the verb recorded the needs")
	assert.Equal(t, sprint.Ready, ta.primary("a-1").Col, "a-1 has not landed")
	ta.clean()
}

// (ii) needs set on a waiting card whose new needs are all landed moves it to
// ready.
func TestNeedsSetMovesAWaitingCardToReady(t *testing.T) {
	t.Parallel()
	ta := landedNeedWorld(t)
	ta.ok("add --stream a a-9 --one --brief-file " + writeBrief(t, "nine"))
	ta.ok("add --stream a a-2 --one --brief-file " + writeBrief(t, "two") + " --needs a-9")
	require.Equal(t, sprint.Waiting, ta.primary("a-2").Col, "a-2 waits on a-9")

	ta.ok("needs set a-2 --needs a-1")
	c := ta.primary("a-2")
	assert.Equal(t, sprint.Ready, c.Col, "a waiting card whose needs have landed is ready")
	assert.Equal(t, "a-1", c.F("needs"), "the landed need is recorded")
	ta.clean()
}

// (iii) needs set --clear empties the needs, and a waiting card moves to ready.
func TestNeedsSetClearEmptiesAndMovesToReady(t *testing.T) {
	t.Parallel()
	ta := needsSetWorld(t)
	ta.ok("add --stream a a-2 --one --brief-file " + writeBrief(t, "two") + " --needs a-1")
	require.Equal(t, sprint.Waiting, ta.primary("a-2").Col, "a-2 waits on a-1")
	require.Equal(t, "a-1", ta.primary("a-2").F("needs"))

	out := ta.ok("needs set a-2 --clear")
	assert.Contains(t, out, "a-2")
	c := ta.primary("a-2")
	assert.Equal(t, "", c.F("needs"), "--clear emptied the needs")
	assert.Equal(t, sprint.Ready, c.Col, "with no needs left, a waiting card is ready")
	ta.clean()
}

// (iv) a need naming a card not on the table is refused, exit 1, nothing
// written, naming it. And the verb is the coordinator's alone.
func TestNeedsSetRefusesANeedNotOnTheTable(t *testing.T) {
	t.Parallel()
	ta := needsSetWorld(t)
	ta.ok("add --stream a a-2 --one --brief-file " + writeBrief(t, "two"))
	before := ta.applies()

	code, out, errs := ta.do("needs set a-2 --needs nosuch")
	assert.Equal(t, 1, code, "a need not on the table is refused: %q", errs)
	assert.NotContains(t, out, "MOVED", "nothing moved")
	assert.Contains(t, errs, "nosuch", "the refusal names the need")
	assert.Equal(t, before, ta.applies(), "a refused needs set wrote")
	assert.Equal(t, "", ta.primary("a-2").F("needs"), "nothing was written")

	code, _, errs = ta.do("needs set a-2 --needs a-1 --actor intruder")
	assert.Equal(t, 1, code, "needs set by another actor: %q", errs)
	assert.Contains(t, errs, "the coordinator's alone", "needs set is the coordinator's: %q", errs)
	assert.Equal(t, before, ta.applies(), "an intruder's needs set wrote")
	ta.clean()
}

// (v) a card dealt or working takes the new needs for its next attempt and
// keeps its attempt.
func TestNeedsSetOnADealtCardKeepsItsAttempt(t *testing.T) {
	t.Parallel()
	ta := landedNeedWorld(t)
	ta.ok("add --stream a a-2 --one --brief-file " + writeBrief(t, "two"))
	ta.deal(1)
	before := ta.primary("a-2")
	require.Equal(t, sprint.Working, before.Col, "a-2 was dealt")
	require.Equal(t, "1", before.F("attempt"))

	ta.ok("needs set a-2 --needs a-1")
	c := ta.primary("a-2")
	assert.Equal(t, sprint.Working, c.Col, "a working card stays where it is")
	assert.Equal(t, "1", c.F("attempt"), "it keeps its attempt")
	assert.Equal(t, "a-1", c.F("needs"), "the new needs are recorded for its next attempt")
	ta.clean()
}

// (vi) a landed or dropped card is refused.
func TestNeedsSetRefusesALandedOrDroppedCard(t *testing.T) {
	t.Parallel()
	ta := landedNeedWorld(t)
	before := ta.applies()
	code, _, errs := ta.do("needs set a-1 --needs a-9")
	assert.Equal(t, 1, code, "a landed card is refused: %q", errs)
	assert.Contains(t, errs, "a-1", "the refusal names the card")
	assert.Equal(t, before, ta.applies(), "a refused landed set wrote")

	ta.ok("add --stream a a-9 --one --brief-file " + writeBrief(t, "nine"))
	ta.ok("drop a-9 --reason 'gone'")
	before = ta.applies()
	code, _, errs = ta.do("needs set a-9 --needs a-1")
	assert.Equal(t, 1, code, "a dropped card is refused: %q", errs)
	assert.Contains(t, errs, "a-9", "the refusal names the card")
	assert.Equal(t, before, ta.applies(), "a refused dropped set wrote")
	ta.clean()
}

// (vii) the brief's DEPENDS-ON: line is rewritten to match, so card <id> shows
// the truth, and no other line changes.
func TestNeedsSetRewritesTheBriefsDependsOnLine(t *testing.T) {
	t.Parallel()
	ta := needsSetWorld(t)
	ta.ok("add --stream a a-0 --one --brief-file " + writeBrief(t, "zero"))
	base := passingBrief("two")
	dir := t.TempDir()
	path := filepath.Join(dir, "a-2.md")
	require.NoError(t, os.WriteFile(path, []byte("DEPENDS-ON: a-0\n"+base), 0o600))
	ta.ok("add --stream a a-2 --one --brief-file " + path)

	before := ta.primary("a-2").F("brief")
	ta.ok("needs set a-2 --needs a-1")
	after := ta.primary("a-2").F("brief")

	assert.Contains(t, after, "DEPENDS-ON: a-1", "the brief's DEPENDS-ON line matches the card")
	assert.NotContains(t, after, "a-0", "the old need is gone from the brief")
	assert.Equal(t, stripDepends(before), stripDepends(after), "no other line changed")
	ta.clean()
}

// stripDepends is a brief's lines but its DEPENDS-ON: line, for a test that one
// line alone changed.
func stripDepends(brief string) []string {
	var out []string
	for _, l := range strings.Split(brief, "\n") {
		if strings.HasPrefix(l, "DEPENDS-ON:") {
			continue
		}
		out = append(out, l)
	}
	return out
}
