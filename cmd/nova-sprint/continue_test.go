package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

const (
	continueHead1 = "1111111111111111111111111111111111111111"
	continueHead2 = "2222222222222222222222222222222222222222"
)

// A card whose attempts pushed heads and that reached its brief's bound is
// continued from the carried head: the brief's CARRY: line at the newest pushed
// head, the continue note last, the bound reset, the bound judgment answered,
// and the card back in ready.
func TestContinueAnswersABoundFromTheCarriedHead(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	// the cap is two (nova-config's max_attempts on the twin); the seat's rework at attempt 1
	// is a decision (brief_bound.go, FieldDecidedAttempt), so it is the second identical
	// failure (rule 2, the bound judgment) that stops the card at attempt 2
	ta.m.SetMaxAttempts("2")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + writeNeedsBrief(t, t.TempDir(), "s1-1", "the work", ""))
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1 --head " + continueHead1 + " --failed --report 'partial work'")
	ta.ok("rework s1-1 --fix 'continue the carried work'")
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w2@1")
	ta.ok("finish --as m1 s1-1.w2@1 --head " + continueHead2 + " --failed --report 'partial work'")

	pr := ta.primary("s1-1")
	require.Equal(t, sprint.Review, pr.Col, "at the bound in review")
	_ = ta.group(sprint.NBound, "s1") // the bound judgment is open

	out := ta.ok("continue s1-1 --note x")
	require.Contains(t, out, "CONTINUE OK")

	pr = ta.primary("s1-1")
	require.Equal(t, sprint.Ready, pr.Col, "back in ready")
	brief := pr.F("brief")
	require.Equal(t, 1, strings.Count(brief, "CARRY:"), "exactly one CARRY line:\n%s", brief)
	require.Contains(t, brief, "CARRY: s1-1 attempt 2 head="+continueHead2, "the carried head is the second attempt's:\n%s", brief)
	require.Equal(t, "2", pr.F(sprint.FieldBriefAttempt), "the bound counts from the continue")
	lines := strings.Split(strings.TrimSpace(brief), "\n")
	require.Contains(t, lines[len(lines)-1], "x", "the note is last:\n%s", brief)
	require.Empty(t, briefWrong(ta), "the bound judgment is answered")
	ta.clean()
}

// A continue that is given --carry <sha> carries that sha even when the card
// has no attempt that pushed a head.
func TestContinueCarriesAnExplicitSha(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + writeNeedsBrief(t, t.TempDir(), "s1-1", "the work", ""))
	ta.ok("continue s1-1 --carry " + continueHead1 + " --note y")
	pr := ta.primary("s1-1")
	require.Contains(t, pr.F("brief"), "CARRY: s1-1 attempt 1 head="+continueHead1)
	ta.clean()
}

// A continue refuses, nothing written, when no attempt pushed a head and no
// --carry is given.
func TestContinueRefusesWithoutACarriedHead(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + writeNeedsBrief(t, t.TempDir(), "s1-1", "the work", ""))
	applies := ta.applies()
	code, _, errs := ta.do("continue s1-1 --note x")
	require.Equal(t, 1, code, errs)
	require.Contains(t, errs, "no attempt pushed a head", errs)
	require.Equal(t, applies, ta.applies(), "nothing written")
}
