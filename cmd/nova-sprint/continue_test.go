package main

import (
	"bytes"
	"encoding/json"
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

// continueBoundCard drives s1-1 to its bound in review with two pushed heads,
// the state TestContinueAnswersABoundFromTheCarriedHead continues from.
func continueBoundCard(t *testing.T, ta *testApp) {
	t.Helper()
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.m.SetMaxAttempts("2")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + writeNeedsBrief(t, t.TempDir(), "s1-1", "the work", ""))
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1 --head " + continueHead1 + " --failed --report 'partial work'")
	ta.ok("rework s1-1 --fix 'continue the carried work'")
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w2@1")
	ta.ok("finish --as m1 s1-1.w2@1 --head " + continueHead2 + " --failed --report 'partial work'")
	require.Equal(t, sprint.Review, ta.primary("s1-1").Col, "at the bound in review")
	_ = ta.group(sprint.NBound, "s1")
}

// dryContinue runs continueDryRun itself on the test store, as cmdContinue
// does for --dry-run. (The verb's --dry-run flag is not registered today —
// stepDryRun has no "continue" — so the CLI refuses it; that gap is listed in
// the PR, and these tests hold the dry-run body itself.)
func dryContinue(t *testing.T, ta *testApp, req sprint.ContinueReq) (int, string, string) {
	t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	var out, errs bytes.Buffer
	code := ta.a.continueDryRun(common{}, st, req, &out, &errs)
	return code, out.String(), errs.String()
}

// continueDryRun plans the step on one read: it prints the new brief's header
// and the judgments it would answer, and writes nothing (the card stays at
// its bound, the bound judgment stays open, and a real continue still lands).
func TestContinueDryRunPrintsBriefHeaderAndJudgmentsWithoutWriting(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	continueBoundCard(t, ta)
	briefBefore := ta.primary("s1-1").F("brief")
	snapBefore, err := ta.m.Snapshot()
	require.NoError(t, err)

	code, out, _ := dryContinue(t, ta, sprint.ContinueReq{ID: "s1-1", Note: "x", Who: "coordinator"})
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "DRY-RUN", out)
	require.Contains(t, out, "nothing was written", out)
	require.Contains(t, out, "BRIEF HEADER:", out)
	require.Contains(t, out, "CARRY: s1-1 attempt 2 head="+continueHead2, out)
	require.Contains(t, out, "WOULD ANSWER", out)
	require.Contains(t, out, "WOULD s1-1 continued", out)

	snapAfter, err := ta.m.Snapshot()
	require.NoError(t, err)
	require.JSONEq(t, string(snapBefore), string(snapAfter), "a dry-run wrote to the store")
	pr := ta.primary("s1-1")
	require.Equal(t, sprint.Review, pr.Col, "a dry-run moves nothing")
	require.Equal(t, briefBefore, pr.F("brief"), "a dry-run edits no brief")
	_ = ta.group(sprint.NBound, "s1") // the bound judgment is still open

	out = ta.ok("continue s1-1 --note x")
	require.Contains(t, out, "CONTINUE OK")
	require.Equal(t, sprint.Ready, ta.primary("s1-1").Col, "the real continue still lands")
	ta.clean()
}

// A dry run the step refuses exits 1, says REFUSED with the step's reason,
// and writes nothing.
func TestContinueDryRunRefusedExits1AndWritesNothing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + writeNeedsBrief(t, t.TempDir(), "s1-1", "the work", ""))
	briefBefore := ta.primary("s1-1").F("brief")
	applies := ta.applies()

	code, out, _ := dryContinue(t, ta, sprint.ContinueReq{ID: "s1-1", Note: "x", Who: "coordinator"})
	require.Equal(t, 1, code, out)
	require.Contains(t, out, "DRY-RUN", out)
	require.Contains(t, out, "REFUSED", out)
	require.Contains(t, out, "no attempt pushed a head", out)
	require.Equal(t, applies, ta.applies(), "a refused dry-run wrote")
	require.Equal(t, briefBefore, ta.primary("s1-1").F("brief"), "a refused dry-run edited the brief")
	ta.clean()
}

// A dry run with --json (the common the verb hands it) reports the run as a
// dry run, with the step's change count and the brief header among its lines.
func TestContinueDryRunJSONReportsTheDryRun(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + writeNeedsBrief(t, t.TempDir(), "s1-1", "the work", ""))

	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	var out, errs bytes.Buffer
	code := ta.a.continueDryRun(common{json: true}, st, sprint.ContinueReq{ID: "s1-1", Note: "y", Carry: continueHead1, Who: "coordinator"}, &out, &errs)
	require.Equal(t, 0, code, "%s%s", out.String(), errs.String())
	var got struct {
		Status  string   `json:"status"`
		DryRun  bool     `json:"dry_run"`
		Changes int      `json:"changes"`
		Lines   []string `json:"lines"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), out.String())
	require.Equal(t, "ok", got.Status)
	require.True(t, got.DryRun, "the JSON run is marked a dry run")
	require.Equal(t, 1, got.Changes)
	require.Contains(t, strings.Join(got.Lines, "\n"), "CARRY: s1-1 attempt 1 head="+continueHead1)
	require.NotContains(t, ta.primary("s1-1").F("brief"), "CARRY:", "a dry-run edited the brief")
	ta.clean()
}

// A --carry that is no 40-hex sha is a branch name: with no REPO: line on the
// card and no --repo-dir the verb refuses before any write, whatever the
// branch is spelled like (short hex, non-hex, a path, upper-case hex).
func TestContinueCarryNonShaIsRefusedAsABranchWithoutWriting(t *testing.T) {
	t.Parallel()
	for _, carry := range []string{"abc1234", strings.Repeat("z", 40), "feature/work", strings.Repeat("A", 40)} {
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		ta.ok("add --stream s1 s1-1 --one --brief-file " + writeNeedsBrief(t, t.TempDir(), "s1-1", "the work", ""))
		applies := ta.applies()
		code, _, errs := ta.do("continue s1-1 --carry " + carry + " --note y")
		require.Equal(t, 2, code, "carry %q: %s", carry, errs)
		require.Contains(t, errs, "names no REPO: line and no --repo-dir was given", "carry %q", carry)
		require.Equal(t, applies, ta.applies(), "carry %q wrote", carry)
		require.NotContains(t, ta.primary("s1-1").F("brief"), "CARRY:", "carry %q edited the brief", carry)
		ta.clean()
	}
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
