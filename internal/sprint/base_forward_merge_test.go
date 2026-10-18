package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A base branch taking a forward-merge of another branch by hand moves the
// base's known tip outside the lander. The store records it: every waiting,
// ready or review card whose BASE is that base is marked to be rechecked
// against the new tip (FieldForwardMerge), and the log names the merge. A card
// on another base is untouched.
func TestABaseForwardMergeIsRecordedAndRechecksItsCards(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	const base, other = "sprint/schema-luau", "sprint/other"
	brief := func(b string) string {
		return "RESULT: c tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: " + b + "\n\nTHE TASK. Do the work.\n"
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2, Brief: brief(base)}))
	w.must(Add(w.s, AddReq{Stream: "s2", Count: 1, Brief: brief(other)}))
	require.Equal(t, Ready, w.state("s1-1"))
	require.Equal(t, Ready, w.state("s1-2"))
	require.Equal(t, Ready, w.state("s2-1"))

	const merge = "60a42041"
	w.must(RecordForwardMerge(w.s, ForwardMergeReq{Base: base, From: "sprint/schema-go", Merge: merge, Who: "coordinator", Reason: "merged by hand"}))

	require.Equal(t, merge, w.s.Work.Card("s1-1").F(FieldForwardMerge), "a waiting card on the base is marked for recheck")
	require.Equal(t, merge, w.s.Work.Card("s1-2").F(FieldForwardMerge), "a ready card on the base is marked for recheck")
	require.Empty(t, w.s.Work.Card("s2-1").F(FieldForwardMerge), "a card on another base is untouched")

	var log []string
	for _, n := range w.notes {
		if n.Type == NBaseForwardMerged {
			log = append(log, n.What)
		}
	}
	require.Len(t, log, 1, "the merge is logged: %v", log)
	require.Contains(t, log[0], merge, "the log names the merge: %s", log[0])
	require.Contains(t, log[0], "sprint/schema-go", "the log names the merged branch: %s", log[0])
	require.Contains(t, log[0], base, "the log names the base: %s", log[0])
	require.NotContains(t, strings.Join(log, "\n"), other)
}
