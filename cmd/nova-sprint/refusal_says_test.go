package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A refusal says why, in the default text output (docs/CLI.md: "refusals with
// reasons (REFUSED, on stderr)"; docs/STANDARD.md section 3, point 1: the line
// is `<TOKEN> REFUSED: <why>; run: <remedy>`), and it says what to run instead.
// The dogfood of 2026-10-10 ran brief and return on a working card and read
// only `BRIEF FAILED moved=0 refused=1` and `RETURN FAILED moved=0 refused=1`:
// the reason and the remedy were in --json alone. brief on a working card must
// print the card id, the reason (a working card keeps its brief, docs/
// SPEC-SPRINT.md section 3: nothing moves it but its finish) and the remedy
// (what changes its work instead); return on a working card the id, the reason
// (not merging) and the remedy (the moves the card still has).
func TestBriefAndReturnOnAWorkingCardSayWhyAndTheRemedy(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream a --count 1 --one --brief-file " + writeBrief(t, "the work"))
	ta.deal(1)
	ta.ok("start")

	code, _, errs := ta.do("brief a-1 --brief-file " + writeBrief(t, "the new work"))
	require.Equal(t, 1, code)
	assert.Contains(t, errs, "REFUSED a-1: a-1 is working: a card working, merging or landed keeps its brief", errs)
	assert.Contains(t, errs, "; run: nova-sprint drop a-1 --reason '<why>'", "the remedy is in the text, not only in --json: %s", errs)
	assert.Contains(t, errs, "BRIEF FAILED moved=0 refused=1")

	code, _, errs = ta.do("return a-1 --reason 'suspect of the red batch'")
	require.Equal(t, 1, code)
	assert.Contains(t, errs, "REFUSED a-1: not merging (it is working)", errs)
	assert.Contains(t, errs, "; run: nova-sprint card a-1 (its attempt is running: once it finishes (review), accept, rework or drop it), or now: nova-sprint drop a-1 --reason '<why>'",
		"the remedy is in the text, not only in --json: %s", errs)
	assert.Contains(t, errs, "RETURN FAILED moved=0 refused=1")
	ta.clean()
}

// refusalWhy is a printed refusal that says why: `REFUSED <key>: <why>`, the
// reason non-empty.
var refusalWhy = regexp.MustCompile(`REFUSED \S+: \S`)

// theStates sets a card in each state of the lifecycle in one sprint (docs/
// SPEC-SPRINT.md section 3): a-1 dealt and taken (working), a-2 waiting behind
// it, then a-1 through review and merging to landed, and a-2 ready after the
// tick its landing frees.
func theStates(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream a --count 1 --one --brief-file " + writeBrief(t, "the work"))
	ta.ok("add --stream a a-2 --one --needs a-1 --brief-file " + writeBrief(t, "the second"))
	ta.deal(1)
	ta.ok("start")
	ta.ok("take --as m1 a-1.w1@1")
	return ta
}

// Every verb that can refuse prints each refusal with a reason, and a refusal
// of a card names the remedy too, in the default text output: the summary line
// alone (`RETURN FAILED moved=0 refused=1`) is not an answer. The walk runs
// every verb with a flag none of them takes (the usage refusal, before the
// store is read), then each row of the table, a verb driven against a card
// whose state does not take it, and fails on any REFUSED line printed without
// a reason, or a card's refusal printed without a remedy.
func TestEveryVerbThatCanRefusePrintsAReasonAndARemedy(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	// every verb's usage refusal: reason and remedy, nothing read
	for _, v := range verbs {
		code, _, errs := ta.do(v.name + " --no-such-flag-walk")
		require.NotEqual(t, 0, code, "%s: %s", v.name, errs)
		for _, l := range printedRefusals(errs) {
			assert.Regexp(t, refusalWhy, l, "%s", v.name)
			assert.Contains(t, l, "; run: ", "%s", v.name)
		}
	}
	ta = theStates(t)

	// a card in each state, refused by the verb that wants another: the reason
	// names the state, and the remedy names what changes it
	// (docs/SPEC-SPRINT.md section 3, the lifecycle's moves)
	for _, round := range []struct {
		at   string
		set  func(t *testing.T, ta *testApp)
		rows []string
	}{
		{
			at: "working a-1, waiting a-2",
			rows: []string{
				"return a-1 --reason 'suspect'",
				"accept a-1 --read-ok",
				"release a-1 --reason x",
				"resolve a-1",
				"return a-2 --reason 'suspect'",
			},
		},
		{
			at: "a-1 in review",
			set: func(t *testing.T, ta *testApp) {
				ta.ok("finish --as m1 a-1.w1@1 --head abc1234 --report 'tests green'")
			},
			rows: []string{
				"return a-1 --reason 'suspect'",
				"resolve a-1",
			},
		},
		{
			at: "a-1 merging",
			set: func(t *testing.T, ta *testApp) {
				ta.ok("ask")
				ta.ok("read --as reader-a --ok a-1.r1.reader-a")
				ta.ok("accept a-1 --read-ok")
			},
			rows: []string{
				"rework a-1 --fix x",
				"brief a-1 --brief-file " + writeBrief(t, "the new work"),
			},
		},
		{
			at: "a-1 landed, a-2 dealt",
			set: func(t *testing.T, ta *testApp) {
				ta.ok("merge --stream a")
				ta.ok("tick")
			},
			rows: []string{
				"return a-1 --reason 'suspect'",
				"accept a-1 --read-ok",
				"resolve a-1",
				"return a-2 --reason 'suspect'",
				"resolve a-2",
			},
		},
	} {
		if round.set != nil {
			round.set(t, ta)
		}
		for _, line := range round.rows {
			code, _, errs := ta.do(line)
			require.NotEqual(t, 0, code, "%s, %s: %s", round.at, line, errs)
			lines := printedRefusals(errs)
			require.NotEmpty(t, lines, "%s, %s: %s", round.at, line, errs)
			for _, l := range lines {
				assert.Regexp(t, refusalWhy, l, "%s, %s", round.at, line)
				assert.Contains(t, l, "; run: ", "%s, %s: a card's refusal names its remedy", round.at, line)
			}
		}
	}
	ta.clean()
}

// printedRefusals is the REFUSED lines of a run's stderr.
func printedRefusals(errs string) []string {
	var out []string
	for _, l := range strings.Split(errs, "\n") {
		if strings.HasPrefix(l, "REFUSED ") {
			out = append(out, l)
		}
	}
	return out
}
