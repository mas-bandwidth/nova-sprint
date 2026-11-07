package verbs

import (
	"context"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// fakeLanding is the forge for the hand landing: one PR per head branch.
type fakeLanding struct {
	prs      map[string]*forge.PRState
	behind   int
	mergeErr error
	ff, prs2 []string
	opened   int
}

func (f *fakeLanding) FindPR(_ context.Context, _, head, _ string) (forge.PRState, bool, error) {
	p, ok := f.prs[head]
	if !ok {
		return forge.PRState{}, false, nil
	}
	return *p, true, nil
}
func (f *fakeLanding) OpenPR(_ context.Context, _, head, _, _, _ string) (forge.PRInfo, error) {
	f.opened++
	f.prs[head] = &forge.PRState{Number: 9, URL: "https://github.com/o/r/pull/9", State: "OPEN", HeadSHA: head3}
	return forge.PRInfo{Number: 9}, nil
}
func (f *fakeLanding) Merge(_ context.Context, _ string, _ int, sha, _ string) error {
	if f.mergeErr != nil {
		return f.mergeErr
	}
	f.ff = append(f.ff, sha)
	return nil
}
func (f *fakeLanding) MergePR(_ context.Context, _ string, _ int, head string, _ []string) (string, error) {
	if f.mergeErr != nil {
		return "", f.mergeErr
	}
	f.prs2 = append(f.prs2, head)
	return "cccccccccccccccccccccccccccccccccccccccc", nil
}
func (f *fakeLanding) Behind(context.Context, string, string, string) (int, error) {
	return f.behind, nil
}

const head3 = "3333333333333333333333333333333333333333"

// merging3 is p3 merging at head3 on sprint/s/p3 (its work's branch), with
// an open land judgement about it.
func merging3(s *store.Snapshot) {
	s.Cards["p3"] = store.Card{ID: "p3", Kind: "producer", Stream: "s", State: "merging", Gen: 1, Attempt: 1, Review: 1, Tier: "pro", Priority: "normal", Head: head3, Author: "alice"}
	s.Cards["p3.w1"] = store.Card{ID: "p3.w1", Kind: "work", Of: "p3", Stream: "s", State: "done", Result: "ok", Gen: 1, Attempt: 1, Holder: "alice", Tier: "pro", Branch: "sprint/s/p3", Head: head3, BaseSHA: base, Priority: "normal"}
	withJudgement("land.p3.1", "land", "land", "p3", "land | fix | retry")(s)
}

func landWorld(t *testing.T) (*world, *fakeLanding) {
	s := sprint()
	merging3(&s)
	w := worldFrom(t, s)
	w.origin.tips["o/r sprint/s/p3"] = head3
	f := &fakeLanding{prs: map[string]*forge.PRState{}}
	w.e.Landing = f
	return w, f
}

// land <card>: the PR is opened, fast-forwarded (the head contains the
// base), the card landed and the open land judgement answered land.
func TestLandCardByHand(t *testing.T) {
	t.Parallel()
	w, f := landWorld(t)
	want(t, w.do("carol", "land", "p3", "--reason", "looked: the red was the runner"), 0, "fast-forwarded main to "+head3, "p3->landed")
	if f.opened != 1 || len(f.ff) != 1 {
		t.Fatalf("opened %d ff %v", f.opened, f.ff)
	}
	s := w.snap(t)
	if s.Cards["p3"].State != "landed" || s.Judgements["land.p3.1"].State != "answered" {
		t.Fatalf("p3 %s, judgement %s", s.Cards["p3"].State, s.Judgements["land.p3.1"].State)
	}
}

// judge <land judgement> land is the same hand landing; behind the base it
// merges by the repository's method.
func TestJudgeLandIsTheHandLanding(t *testing.T) {
	t.Parallel()
	w, f := landWorld(t)
	f.behind = 1
	want(t, w.do("carol", "judge", "land.p3.1", "land", "--reason", "looked"), 0, "merged as cccc", "p3->landed")
	if len(f.prs2) != 1 {
		t.Fatalf("merges %v", f.prs2)
	}
}

// judge <land judgement> fix sends the card back with the finding.
func TestJudgeLandFixSendsBack(t *testing.T) {
	t.Parallel()
	w, _ := landWorld(t)
	want(t, w.do("carol", "judge", "land.p3.1", "fix", "--reason", "bus is over its line budget"), 0, "p3->ready")
	c := w.snap(t).Cards["p3"]
	if c.Finding == "" || c.Bounces != 1 {
		t.Fatalf("p3 %+v", c)
	}
}

// The hand landing refuses a head not on origin and says a refused merge
// verbatim; nothing moves.
func TestLandRefusals(t *testing.T) {
	t.Parallel()
	w, _ := landWorld(t)
	delete(w.origin.tips, "o/r sprint/s/p3")
	want(t, w.do("carol", "land", "p3", "--reason", "x"), 1, "is not the tip of sprint/s/p3 at origin", "git push origin sprint/s/p3")
	w, f := landWorld(t)
	f.mergeErr = errors.New("Update is not a fast forward")
	want(t, w.do("carol", "land", "p3", "--reason", "x"), 1, "Update is not a fast forward")
	w, _ = landWorld(t)
	want(t, w.do("alice", "land", "p3", "--reason", "x"), 1)
	if w.snap(t).Cards["p3"].State != "merging" {
		t.Fatal("a refused landing moved the card")
	}
}
