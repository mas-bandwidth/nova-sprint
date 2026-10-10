package sprint

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The machine gate on the world harness (docs/SPEC-SPRINT.md section 6, the machine
// gate): the gate is a fake here, so the test pins the tick's rule and not a bench. A red
// gate reworks the finished attempt with its failing lines as the fix before any reader is
// asked; a bench that does not answer holds the attempt; a green one records its lines on
// the attempt and the read packet carries them.

// TestARedGateIsReworkedBeforeAnyReaderIsAsked pins the red path: the machine runs the
// finished attempt's gate once, a red gate reworks the attempt with the gate's failing
// lines as its fix, counted toward the card's bound, and the ask asks no reader of it.
func TestARedGateIsReworkedBeforeAnyReaderIsAsked(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	finished(w, "s1-1", false)
	require.Equal(t, Review, w.s.Work.Card("s1-1").Col)

	runs := 0
	red := func(s *Snapshot, c *Card) GateRun {
		runs++
		return GateRun{Host: "bench1", Failed: []GateFinding{
			{File: "internal/sprint/steps_review.go", Line: 42, What: "go test ./internal/sprint: TestTheThing failed"},
		}}
	}
	p, ok := TickGate(w.s, TickReq{Who: "machine", Gate: red})
	require.True(t, ok, "a red gate moves the attempt")
	w.must(p)
	require.Equal(t, 1, runs, "the gate runs once for the attempt")

	pr := w.s.Work.Card("s1-1")
	require.NotEqual(t, Review, pr.Col, "the red gate reworks the attempt out of review")
	assert.Equal(t, 2, pr.Int("attempt"), "the red gate sends the next attempt")
	assert.Contains(t, pr.F("fix"), "internal/sprint/steps_review.go:42", "the failing line is the fix")
	assert.Contains(t, pr.F("fix"), "machine gate")
	assert.Contains(t, pr.F("finding"), "machine gate")
	assert.Equal(t, "1", pr.F(FieldGateReworks), "the gate rework is counted")
	assert.Equal(t, "1", pr.F("broken_reads"), "a gate rework counts toward the bound as a broken read")
	assert.Equal(t, GateRed, pr.F(FieldGate))
	assert.Empty(t, readsAt(w.s, pr, 1), "no reader is asked of the attempt the gate refused")

	// the next attempt's card is told the gate's finding
	if wc := w.s.Fleet.Card(WorkCardID("s1-1", 2)); assert.NotNil(t, wc) {
		assert.Contains(t, wc.F("finding"), "machine gate")
	}

	// the ask of the same pass asks no reader of a card the gate refused
	ask, _ := TickAsk(w.s, TickReq{Who: "machine", Gate: red})
	assert.Empty(t, ask.Units, "the ask asks no reader while the gate holds the attempt")

	// a friend reader is asked no attempt the gate refused: the gate's hold wraps the
	// friend ask too (gaterun.go, gateHeldPart; the finding of attempt 7). The frontier
	// card is in review with a red gate state, the state the ask sees before the gate's
	// rework reaches the table.
	t.Run("a friend reader is never asked of a red attempt", func(t *testing.T) {
		w := newWorld(t, "reader-a", "reader-b")
		putReview(w, "s1-1", "s1-1: work (s1) tier: frontier\n", 1, 1, "primary-head")
		frontier := w.s.Work.Card("s1-1")
		frontier.Fields[FieldGate] = GateRed
		frontier.Fields[FieldGateAttempt] = "1"
		dir := t.TempDir()
		askReaders(t, w, []FriendSeat{frontierSeat("amy", 2, Up, dir)})
		require.Nil(t, w.s.Fleet.Card(ReadCardID("s1-1", 1, "amy")),
			"a friend reader is never asked of an attempt the gate refused")
		require.NoFileExists(t, filepath.Join(dir, "inbox"),
			"the friend's inbox brief is not written for an attempt the gate refused")
	})
}

// TestABenchThatDoesNotAnswerHoldsTheAttempt pins the waiting path: a bench that does not
// answer is not a pass, the attempt stays in review with one judgment, and no reader is
// asked of it.
func TestABenchThatDoesNotAnswerHoldsTheAttempt(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	finished(w, "s1-1", false)

	waiting := func(s *Snapshot, c *Card) GateRun {
		return GateRun{Waiting: "no bench answered: ssh exit 255"}
	}
	p, ok := TickGate(w.s, TickReq{Who: "machine", Gate: waiting})
	require.True(t, ok, "a bench that did not answer is a move")
	w.must(p)

	pr := w.s.Work.Card("s1-1")
	require.Equal(t, Review, pr.Col, "the attempt waits in review")
	assert.Equal(t, GateWaiting, pr.F(FieldGate))
	require.Len(t, w.notesOf(NBenchDown), 1, "one judgment says the bench did not answer")
	require.Len(t, w.openOn("s1-1"), 1, "the judgment is open on the card")

	ask, _ := TickAsk(w.s, TickReq{Who: "machine", Gate: waiting})
	assert.Empty(t, ask.Units, "no reader is asked while the bench has not answered")
}

// TestAGreenGateRecordsItsLinesForTheReadPacket pins the green path: the gate's lines are
// recorded on the attempt, the ask goes on, and the read packet carries the lines so the
// reader judges the change and runs no Go.
func TestAGreenGateRecordsItsLinesForTheReadPacket(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	finished(w, "s1-1", false)

	green := func(s *Snapshot, c *Card) GateRun {
		return GateRun{Host: "bench1", Lines: []string{
			"go vet ./internal/sprint: ok",
			"go test -count=1 -timeout 600s ./internal/sprint: ok",
		}}
	}
	p, ok := TickGate(w.s, TickReq{Who: "machine", Gate: green})
	require.True(t, ok, "a green gate records its lines")
	w.must(p)

	pr := w.s.Work.Card("s1-1")
	require.Equal(t, Review, pr.Col, "a green gate leaves the attempt in review")
	assert.Equal(t, GateGreen, pr.F(FieldGate))
	assert.Equal(t, "bench1", pr.F(FieldGateBench))
	assert.Contains(t, pr.F(FieldGateLines), "go test -count=1 -timeout 600s ./internal/sprint: ok")

	ask, _ := TickAsk(w.s, TickReq{Who: "machine", Gate: green})
	require.NotEmpty(t, ask.Units, "a green gate lets the ask go on")
	w.must(ask)
	reads := readsAt(w.s, pr, 1)
	require.NotEmpty(t, reads, "a reader is asked")

	work := w.s.Fleet.Card(WorkCardID("s1-1", 1))
	pk := PacketOf("t-", 1, reads[0], pr, nil, work)
	assert.Equal(t, GateLines(pr.F(FieldGateLines)), pk.Gate, "the read packet carries the gate's lines")
	assert.Contains(t, pk.Gate, "go vet ./internal/sprint: ok")
}

// TestTheBenchGateRunsTheTestAndTheTouchedPackages pins the gate's commands over a fake
// bench-run seam: the touched packages' go vet and go test, then the TEST line's test
// alone, in that order, each through the verb, and a command that passed gives its line.
func TestTheBenchGateRunsTheTestAndTheTouchedPackages(t *testing.T) {
	t.Parallel()
	var ran [][]string
	diff := "diff --git a/internal/sprint/a.go b/internal/sprint/a.go\n" +
		"--- a/internal/sprint/a.go\n+++ b/internal/sprint/a.go\n@@ -1 +1 @@\n-x\n+y\n"
	g := NewBenchGate(BenchGateGit{
		Clone: func(string) (string, error) { return "/clone", nil },
		Hosts: []string{"bench1", "bench2"},
		Bench: func(ctx context.Context, hosts []string, clone, head string, argv []string) (BenchResult, error) {
			ran = append(ran, argv)
			return BenchResult{Host: hosts[0], Code: 0, Out: "ok\n"}, nil
		},
		View: func(ctx context.Context, dir, base, head string, env []string) (WorkView, error) {
			return WorkView{Head: head, Pushed: true, Diff: diff}, nil
		},
	})
	brief := "REPO: mas-bandwidth/nova-sprint\nBASE: main\nPATHS: internal/sprint/a.go\nTEST: ./internal/sprint TestNamed\n"
	pr := &Card{ID: "c1", Row: "s1", Col: Working,
		Fields: map[string]string{"brief": brief, "head": "0123456789abcdef0123456789abcdef01234567"}}
	run := g(nil, pr)
	require.Empty(t, run.Failed, "the fake bench is green: %v", run)
	require.Len(t, ran, 3, "the gate runs the vet, the touched test and the named test")
	assert.Equal(t, []string{"go", "vet", "./internal/sprint"}, ran[0])
	assert.Equal(t, []string{"go", "test", "-count=1", "-timeout", "600s", "./internal/sprint"}, ran[1])
	assert.Equal(t, []string{"go", "test", "-count=1", "-timeout", "600s", "-run", "^TestNamed$", "./internal/sprint"}, ran[2])
	assert.Equal(t, "bench1", run.Host)
	require.Len(t, run.Lines, 3)
	assert.Contains(t, run.Lines[1], "go test -count=1 -timeout 600s ./internal/sprint: ok")
}

// TestABenchThatDoesNotAnswerLeavesTheGateWaiting pins the seam's error path: a run that
// never reached the command is Waiting, never a red gate with findings.
func TestABenchThatDoesNotAnswerLeavesTheGateWaiting(t *testing.T) {
	t.Parallel()
	g := NewBenchGate(BenchGateGit{
		Clone: func(string) (string, error) { return "/clone", nil },
		Hosts: []string{"bench1"},
		Bench: func(ctx context.Context, hosts []string, clone, head string, argv []string) (BenchResult, error) {
			return BenchResult{}, errNoBench
		},
		View: func(ctx context.Context, dir, base, head string, env []string) (WorkView, error) {
			return WorkView{Head: head, Pushed: true, Diff: "diff --git a/internal/sprint/a.go b/internal/sprint/a.go\n"}, nil
		},
	})
	brief := "REPO: mas-bandwidth/nova-sprint\nBASE: main\nTEST: ./internal/sprint TestNamed\n"
	pr := &Card{ID: "c1", Row: "s1", Col: Working,
		Fields: map[string]string{"brief": brief, "head": "0123456789abcdef0123456789abcdef01234567"}}
	run := g(nil, pr)
	require.Empty(t, run.Failed)
	assert.Contains(t, run.Waiting, "no bench")
}

// TestBenchTestRunnerTreatsAnAbsentTestAsNotPassed pins the pin probe's absence rule: a
// package that names no such test is not a pass. go test exits zero both when the package
// has test files and none match ("[no tests to run]") and when the package has no test
// files at all ("[no test files]"); a change that adds the TEST line's test to a package
// that had no test files at the merge-base must not be mistaken for a test that passes at
// the merge-base (gate-pin-absent), because the card permits a test that does not exist
// there.
func TestBenchTestRunnerTreatsAnAbsentTestAsNotPassed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		res  BenchResult
		want bool
	}{
		{
			name: "a named test that ran and passed is a pass",
			res:  BenchResult{Code: 0, Out: "ok  \tgithub.com/mas-bandwidth/nova-sprint/internal/cardhdr\t0.002s\n"},
			want: true,
		},
		{
			name: "a package with no test files is not a pass",
			res:  BenchResult{Code: 0, Out: "?   \tgithub.com/mas-bandwidth/nova-sprint/internal/cardhdr\t[no test files]\n"},
			want: false,
		},
		{
			name: "a package whose test files name no such test is not a pass",
			res:  BenchResult{Code: 0, Out: "ok  \tgithub.com/mas-bandwidth/nova-sprint/internal/sprint\t0.006s [no tests to run]\n"},
			want: false,
		},
		{
			name: "a failing test is not a pass",
			res:  BenchResult{Code: 1, Out: "--- FAIL: TestNamed (0.00s)\nFAIL\n"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := benchTestRunner(func(context.Context, []string, string, string, []string) (BenchResult, error) {
				return tc.res, nil
			}, []string{"bench1"}, "/clone", nil, nil)
			got, err := runner("head", "./internal/cardhdr", "TestNamed")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// errNoBench is the seam's no-answer error.
var errNoBench = errors.New("no bench answered")
