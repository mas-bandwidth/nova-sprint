package store

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

func TestLandingPushCountsFinishedFriendReadsAfterAccept(t *testing.T) {
	t.Parallel()
	h := inReview(t, 1)
	require.Equal(t, tables(sprint.Merge, sprint.Work, sprint.Readers, sprint.Fleet),
		MergeStep(sprint.MergeReq{Stream: "s1"}).Load, "a successful merge reads active consumer state")
	h.must(SetStep(sprint.SetReq{ReadCards: sprint.ReadCardsOnWord, Who: h.st.Actor}))
	for _, rd := range []string{"reader-a", "reader-b", "reader-c"} {
		require.NoError(t, h.st.SetReaderAway(h.ctx, rd, true, "coordinator"))
	}
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{
		{Name: "stella", Width: 16, Class: "flash,frontier,heavy,pro"},
		{Name: "johnny", Width: 16, Class: "flash,pro"},
	})
	require.NoError(t, err)
	h.up("stella")
	h.up("johnny")
	_, refused := askOf(h.machine())
	require.Empty(t, refused)
	readers := friendReadsOf(h.snap(), "s1-1")
	require.Len(t, readers, 2)
	for _, reader := range readers {
		h.friendRead(reader, "s1-1", "Verdict: LAND\n")
	}
	history := h.snap().Work.Card("s1-1").F("landing_read_history")
	require.NotEmpty(t, history, "the finished verdict history survives the read card's retirement")
	require.Contains(t, history, `"verdict":"ok"`, "the retained record names the verdict")
	h.must(AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1}))
	notes, _, err := h.m.NotesSince(h.ctx, "", 10000)
	require.NoError(t, err)
	var landing []sprint.Note
	for _, n := range notes {
		if n.Type == sprint.NLanding {
			landing = append(landing, n)
		}
	}
	require.Len(t, landing, 1)
	require.True(t, strings.Contains(landing[0].What, "reads 2"), "landing push includes two completed friend reads: %s", landing[0].What)
}
