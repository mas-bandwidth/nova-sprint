package sprint

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestABaseForwardMergeIsRecordedAndRechecksItsCards(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.s.Now = time.Unix(1000, 0).UTC()
	const repo = "mas-bandwidth/nova-sprint"
	const otherRepo = "example/other"
	const base = "sprint/schema-luau"
	brief := func(repo, base string) string {
		return "RESULT: c tier: pro\nREPO: " + repo + "\nBASE: " + base + "\n\nTHE TASK. Do the work.\n"
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2, Brief: brief(repo, base)}))
	w.must(Add(w.s, AddReq{Stream: "s2", Count: 1, Brief: brief(otherRepo, base)}))
	w.must(Add(w.s, AddReq{Stream: "s3", Count: 1, Brief: brief(repo, "sprint/other")}))
	require.Equal(t, Ready, w.state("s1-1"))
	require.Equal(t, Ready, w.state("s1-2"))

	const merge = "60a42041"
	w.must(RecordForwardMerge(w.s, ForwardMergeReq{Repo: repo, Base: base, From: "sprint/schema-go", Merge: merge, Who: "coordinator", Reason: "merged by hand"}))

	for _, id := range []string{"s1-1", "s1-2"} {
		require.Equal(t, merge, w.s.Work.Card(id).F(FieldForwardMerge), "%s is marked pending a recheck", id)
		require.Empty(t, w.s.Work.Card(id).F(FieldForwardMergeChecked), "%s is not falsely recorded as checked", id)
	}
	require.Empty(t, w.s.Work.Card("s2-1").F(FieldForwardMerge), "the same branch name in another repository is untouched")
	require.Empty(t, w.s.Work.Card("s3-1").F(FieldForwardMerge), "another base in the same repository is untouched")

	ctl := w.s.StreamCtl("s1")
	require.NotNil(t, ctl)
	require.Equal(t, repo, ctl.F(FieldBaseLandedRepo), "durable base tip is repository-scoped on the stream control row")
	require.Equal(t, base, ctl.F(FieldBaseLandedBase))
	require.Equal(t, merge, ctl.F(FieldBaseLandedTip), "base tip moves even when the legacy Work property was absent")
	_, hadLegacyTip := w.s.Work.Prop(BaseLandedKey(repo, base))
	require.False(t, hadLegacyTip, "a new durable base tip does not require a legacy property")

	var log []string
	for _, n := range w.notes {
		if n.Type == NBaseForwardMerged {
			log = append(log, n.What)
		}
	}
	require.Len(t, log, 1, "the merge is logged: %v", log)
	require.Contains(t, log[0], merge)
	require.Contains(t, log[0], "sprint/schema-go")
	require.Contains(t, log[0], repo)
	require.Contains(t, log[0], base)
}

func TestABaseForwardMergeBlocksDealUntilTheRecheckIsRecorded(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.s.Now = time.Unix(1000, 0).UTC()
	const repo, base, merge = "mas-bandwidth/nova-sprint", "sprint/schema-luau", "60a42041"
	brief := "RESULT: c tier: pro\nREPO: " + repo + "\nBASE: " + base + "\n\nTHE TASK. Do the work.\n"
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 1, Brief: brief}))
	w.must(RecordForwardMerge(w.s, ForwardMergeReq{Repo: repo, Base: base, From: "sprint/schema-go", Merge: merge, Who: "coordinator", Reason: "merged by hand"}))

	blocked := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, blocked.Units, "a stale ready card is never dealt")
	require.Len(t, blocked.Refused, 1)
	require.Contains(t, blocked.Refused[0].Why, "not rechecked")

	card := w.s.Work.Card("s1-1")
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(card.F("brief"))))
	bad := RecheckForwardMerge(w.s, ForwardMergeRecheckReq{Repo: repo, Base: base, Merge: merge, Who: "coordinator", Behind: 2,
		Checks: []ForwardMergeCardCheck{{ID: card.ID, Stream: card.Row, BriefSHA256: "wrong"}}})
	require.Empty(t, bad.Units, "a stale brief witness cannot clear the pending mark")
	require.Len(t, bad.Refused, 1)
	require.Equal(t, merge, card.F(FieldForwardMerge))
	require.Empty(t, card.F(FieldForwardMergeChecked))
	w.must(RecheckForwardMerge(w.s, ForwardMergeRecheckReq{
		Repo: repo, Base: base, Merge: merge, Who: "coordinator", Behind: 2,
		Checks: []ForwardMergeCardCheck{{ID: card.ID, Stream: card.Row, BriefSHA256: digest}},
	}))
	card = w.s.Work.Card("s1-1")
	require.Equal(t, merge, card.F(FieldForwardMergeChecked))
	require.Equal(t, "2", card.F(FieldForwardMergeBehind), "the fresh behind count is recorded")
	dealt := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, dealt.Refused, "the exact rechecked card is dealable")
	require.Len(t, dealt.Units, 1)
}
