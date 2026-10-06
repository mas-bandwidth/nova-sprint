package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The store's lines of merge health (docs/SPEC-SPRINT.md section 8, "The coordinator's
// pass", merge health): dev behind (DevBehind) and a stream the base-gate rule stopped
// (NBaseRed) are lines with no drift facts recorded, and the promotion closes its line.
func TestMergeHealthCarriesDevBehindAndAStoppedStream(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	assert.Empty(t, MergeHealthLines(w.s), "nothing landed, nothing stopped")
	w.place(w.s.Work, "s1-1", "s1", Landed)
	w.s.Work.Card("s1-1").Fields["landed"] = stamp(t0.Add(-PromoteAge - time.Minute))
	w.s.Work.Card("s1-1").Fields["base"] = "sprint/b"
	w.note(Note{Kind: Judgment, Type: NBaseRed, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, What: "stopped", At: t0, Marked: true})

	w.part(TickCoordinatorPass, TickReq{})
	open := w.openOn(StreamSubject(""))
	require.Len(t, open, 1, "one merge-health judgment: %v", w.s.Open)
	h := open[0].Note
	assert.Equal(t, NMergeHealth, h.Type)
	assert.Contains(t, h.What, "stream s1 stopped: its base fails its tree gate")
	assert.Contains(t, h.What, "dev is behind: 1 cards landed on sprint/b since no promotion recorded")
	assert.Less(t, strings.Index(h.What, "stream s1 stopped"), strings.Index(h.What, "dev is behind"), "the base before dev")

	// the base-gate judgment answered, dev promoted: nothing holds, the pass closes it
	w.closeAll(w.openOn("s1-1"))
	w.must(Promoted(w.s, PromotedReq{Sha: "abc1234", Who: "coordinator"}))
	w.part(TickCoordinatorPass, TickReq{})
	assert.Empty(t, w.openOn(StreamSubject("")), "merge health well: closed")
}
