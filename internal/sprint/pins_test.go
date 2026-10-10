package sprint_test

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The machine lint's pin checks on the twin repository and a fake bench (docs/SPEC-SPRINT.md
// section 6, the work lint): a test that runs green at the attempt's merge-base passes
// without the change, so it pins nothing; the lint refuses the attempt with the finding,
// reworks it at once, and asks no reader of it. The repository is the twin, the bench a
// fake, so the test pins the check's rule and runs no Go.

// TestATestThatDoesNotPinTheChangeIsReworkedBeforeARead pins the red-first check: the TEST
// line's test passes at the merge-base (a fake bench says so), so the machine lint gives
// gate-pin-absent, the rework is at once, it counts as a broken read, and no reader, the
// friends' or the machine's, is asked of the attempt.
func TestATestThatDoesNotPinTheChangeIsReworkedBeforeARead(t *testing.T) {
	t.Parallel()
	r := newLintRig(t)
	repo := r.repo
	// the base commit is the merge-base of this branch: the test that passes there pins
	// nothing
	base := repo.git(repo.dev, "rev-parse", "HEAD")
	r.gate = func(_ context.Context, ref string, _ []string) (sprint.BenchResult, error) {
		if ref == base {
			return sprint.BenchResult{Code: 0, Out: "ok  \tgithub.com/mas-bandwidth/nova-sprint/internal/sprint\t0.001s\n"}, nil
		}
		return sprint.BenchResult{Code: 1, Out: "--- FAIL\n"}, nil
	}
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "pin", Brief: lintBrief("pin", repo.origin)}}}))
	head := repo.branch("work/pin", map[string]string{"internal/sprint/a.go": "package sprint\n\n// A is two.\nfunc A() int { return 2 }\n"}, "a change no test pins")
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.tick()
	wc := r.snap().Fleet.Card(sprint.WorkCardID("pin", 1))
	require.NotNil(t, wc)
	if wc.Col != sprint.Working {
		r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Who: "m1"}))
		wc = r.snap().Fleet.Card(wc.ID)
	}
	r.must(store.FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")},
		Head: head, Report: "done", Usage: lintUsage, Who: "m1"}))
	r.tick()
	r.tick()

	s := r.snap()
	pr := s.Work.Card("pin")
	require.NotNil(t, pr)
	assert.Equal(t, 2, pr.Int("attempt"), "the attempt is reworked at once")
	assert.Contains(t, pr.F("fix"), sprint.LintPinAbsent+":", "the finding is its fix")
	assert.Contains(t, pr.F("fix"), "remedy: ", "the finding carries its remedy")
	assert.Equal(t, "1", pr.F(sprint.FieldLintReworks), "the machine lint's rework is counted")
	assert.Equal(t, "1", pr.F("broken_reads"), "a lint rework counts toward the bound as a broken read")
	assert.Empty(t, s.Readers.Of("pin"), "no reader is asked of the attempt the machine lint refused")
	if wc := s.Fleet.Card(sprint.WorkCardID("pin", 2)); assert.NotNil(t, wc) {
		assert.Contains(t, wc.F("finding"), sprint.LintPinAbsent, "the next attempt is told the finding")
	}
}
