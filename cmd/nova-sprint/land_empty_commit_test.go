package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestLandRefusesAnEmptyCommitThatClaimsChanges tests that checkEmptyCommit refuses
// an empty commit whose RESULT.md claims changes (docs/SPEC-SPRINT.md section 7, the lander's
// checks): given an empty diff and a claiming RESULT.md it refuses with the finding
// "empty commit: the result claims changes the diff does not show"; given an empty diff
// and a missing result (no work card, blank result text) it refuses with the finding
// "empty commit: the head changes nothing from the attempt's start, and the lander holds
// no result to say why", so a card the lander holds no result for is not accused of
// claiming changes; given an empty diff and a nothing-to-do result it passes, and given a
// non-empty diff it passes.
func TestLandRefusesAnEmptyCommitThatClaimsChanges(t *testing.T) {
	t.Parallel()

	const wantFinding = "empty commit: the result claims changes the diff does not show"
	const wantNoResultFinding = "empty commit: the head changes nothing from the attempt's start, and the lander holds no result to say why"

	t.Run("empty diff with claiming result refuses", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name   string
			result string
			want   string
		}{
			{
				name:   "verdict ok",
				result: "head: 2d720d219ff5\nbranch: sprint/c1.w1\nverdict: ok\nreport: done\n",
			},
			{
				name:   "step line with commit sha",
				result: "STEP 1. Commit 2d720d219ff5\n",
			},
			{
				name:   "non-empty diff stat files changed",
				result: "diff stat: 1 file changed, 5 insertions(+)\n",
			},
			{
				name:   "nothing-to-do in prose beside verdict ok",
				result: "verdict: ok\nhead: 2d720d219ff5\nI found nothing to do about the lint, then changed land.go\n",
			},
			{
				name:   "nothing-to-do in prose beside a step sha",
				result: "STEP 1. Commit 2d720d219ff5\nthere was nothing to do after that\n",
			},
			{
				name:   "no result text at all fails closed",
				result: "",
				want:   wantNoResultFinding,
			},
			{
				name:   "non-empty diff stat table row",
				result: "cmd/nova-sprint/land.go | 10 ++++++++++\n",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := checkEmptyCommit(true, tc.result)
				want := tc.want
				if want == "" {
					want = wantFinding
				}
				assert.Equal(t, want, got)
				if tc.want == wantNoResultFinding {
					assert.NotContains(t, got, "the result claims changes")
				}
			})
		}
	})

	t.Run("empty diff with nothing-to-do result passes", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name   string
			result string
		}{
			{
				name:   "nothing-to-do report line",
				result: "verdict: nothing\nhead: 2d720d219ff5\nnothing to do: already clean\n",
			},
			{
				name:   "nothing-to-do report field",
				result: "head: 2d720d219ff5\nreport: nothing-to-do at this head\n",
			},
			{
				name:   "every step dash",
				result: "STEP 1. -\nSTEP 2. -\n",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := checkEmptyCommit(true, tc.result)
				assert.Empty(t, got)
			})
		}
	})

	t.Run("an all-letter word is not a commit sha", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, checkEmptyCommit(true, "STEP 1. decade defaced -\n"))
		assert.Empty(t, checkEmptyCommit(true, "STEP 1. facade -\nSTEP 2. -\n"))
	})

	t.Run("claimText carries the recorded finish", func(t *testing.T) {
		t.Parallel()
		ok := &sprint.Card{Fields: map[string]string{"ok": "yes", "head": "2d720d219ff5", "report": "verdict: ok\n1 file changed, 10 insertions(+)"}}
		assert.Equal(t, wantFinding, checkEmptyCommit(true, claimText(ok)))
		generic := &sprint.Card{Fields: map[string]string{"ok": "yes", "head": "2d720d219ff5", "report": "done"}}
		assert.Empty(t, checkEmptyCommit(true, claimText(generic)), "generic report does not claim changes")
		none := &sprint.Card{Fields: map[string]string{"ok": "yes", "head": "2d720d219ff5", "report": "nothing to do: the card was right"}}
		assert.Empty(t, checkEmptyCommit(true, claimText(none)))
		got := checkEmptyCommit(true, claimText(nil))
		assert.Equal(t, wantNoResultFinding, got, "no work card fails closed")
		assert.NotContains(t, got, "the result claims changes")
	})

	t.Run("both empty-commit findings return the card to review", func(t *testing.T) {
		t.Parallel()
		assert.True(t, isEmptyCommitFinding(emptyCommitFinding), "a claiming result is an empty-commit finding")
		assert.True(t, isEmptyCommitFinding(emptyCommitNoResultFinding), "a missing result is an empty-commit finding")
		assert.NotEqual(t, emptyCommitFinding, emptyCommitNoResultFinding, "the two findings read differently")
		assert.False(t, isEmptyCommitFinding("some other refusal"), "a refusal that is not an empty commit is not one")
	})

	t.Run("non-empty diff passes", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name   string
			result string
		}{
			{
				name:   "claiming verdict ok with non-empty diff",
				result: "verdict: ok\n1 file changed, 10 insertions(+)\n",
			},
			{
				name:   "step sha with non-empty diff",
				result: "STEP 1. Commit 2d720d219ff5\n",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := checkEmptyCommit(false, tc.result)
				assert.Empty(t, got)
			})
		}
	})
}
