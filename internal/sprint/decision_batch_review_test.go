package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// These fixtures prove the predicate state before checking the notice. A
// negative-only fixture can pass even when the whole Review branch is deleted.
func decisionReviewFixture(t *testing.T) (*Snapshot, *Card) {
	t.Helper()
	w := newWorld(t, "reader-m1")
	w.readersTable()
	putReview(w, "review-card", "RESULT: review-card tier: flash\n", 1, 1, "head")
	w.s.Work.SetProp(PropReadsNeeded, "1")
	w.s.ReaderStates = map[string]string{"reader-m1": ReaderUp}
	w.s.Routes = []Route{{Name: "flash-route", Tier: "flash", Enabled: false}}
	return w.s, w.s.Work.Card("review-card")
}

func decisionReviewNotice(t *testing.T, s *Snapshot, pr *Card, count int) {
	t.Helper()
	n := Note{ID: "tier-judgment", Kind: Judgment, Type: NNoRoute, StreamLevel: true, Stream: TierSubject("flash"), Primaries: []string{pr.ID}, At: s.Now.Add(-DeadlineJudgment), What: "1 primaries of tier flash wait: no route serves it"}
	for _, lane := range []string{"coordinator", "owner"} {
		set, body, _ := DecisionBatch(s.Now, []Open{{Key: OpenKey(n.ID, StreamSubject(n.Stream)), Note: n}}, nil, lane, nil, s)
		require.Equal(t, n.ID, set)
		if count == 1 {
			require.Contains(t, body, "cards="+pr.ID+"\n")
			require.Contains(t, body, "1 primaries of tier flash wait:")
		} else {
			require.NotContains(t, body, "cards=")
			require.NotContains(t, body, pr.ID)
			require.Contains(t, body, "0 primaries of tier flash wait:")
		}
	}
}

func TestDecisionBatchReviewMissingRouteNamesHeldCard(t *testing.T) {
	s, pr := decisionReviewFixture(t)
	require.NotEqual(t, "failed", pr.F("result"))
	require.Equal(t, 1, ReadsWanted(s, pr))
	require.True(t, readsWithoutRoute(s, pr))
	tier, why := s.readRouteMissing(pr)
	require.Equal(t, "flash", tier)
	require.NotEmpty(t, why)
	require.False(t, s.refusedNoRoute(pr), "this exercises missing route alone")
	decisionReviewNotice(t, s, pr, 1)
}

func TestDecisionBatchReviewRefusedRouteNamesHeldCard(t *testing.T) {
	s, pr := decisionReviewFixture(t)
	// Routes are available again, but both allowed identities of the only
	// reader have already refused this attempt and neither can be re-asked.
	s.Routes = nil
	for _, id := range ReadCardIDs(pr.ID, pr.Int("attempt"), "reader-m1") {
		s.Readers.Put(&Card{ID: id, Fields: map[string]string{"primary": pr.ID, "retired_by": RetiredByRefused, FieldRefused: "no route serves this read"}})
	}
	tier, why := s.readRouteMissing(pr)
	require.Equal(t, "flash", tier)
	require.Empty(t, why, "this must depend on refusedNoRoute, not missing route")
	require.True(t, readsWithoutRoute(s, pr))
	require.True(t, s.refusedNoRoute(pr))
	decisionReviewNotice(t, s, pr, 1)
}

func TestDecisionBatchReviewFailedCardIsNotHeldByMissingRoute(t *testing.T) {
	s, pr := decisionReviewFixture(t)
	pr.Fields["result"] = "failed"
	require.True(t, readsWithoutRoute(s, pr), "only the failed-result guard excludes it")
	_, why := s.readRouteMissing(pr)
	require.NotEmpty(t, why)
	decisionReviewNotice(t, s, pr, 0)
}

func TestDecisionBatchReviewExistingReadsAreNotHeldByMissingRoute(t *testing.T) {
	for _, col := range []string{Asked, Reading, OK} {
		t.Run(col, func(t *testing.T) {
			s, pr := decisionReviewFixture(t)
			// The route became unavailable after this read was launched. Its
			// already-routed read still stands, so no replacement is wanted.
			s.Readers.Put(&Card{ID: ReadCardID(pr.ID, pr.Int("attempt"), "reader-m1"), Row: "reader-m1", Col: col, Fields: map[string]string{"primary": pr.ID, "head": "head", FieldRoute: "flash-route"}})
			require.NotEqual(t, "failed", pr.F("result"))
			require.Zero(t, ReadsWanted(s, pr))
			require.False(t, readsWithoutRoute(s, pr), "only this guard excludes it")
			_, why := s.readRouteMissing(pr)
			require.NotEmpty(t, why, "otherwise deleting readsWithoutRoute stays green")
			decisionReviewNotice(t, s, pr, 0)
		})
	}
}
