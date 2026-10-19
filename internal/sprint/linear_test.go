package sprint

import (
	"errors"
	"strings"
	"testing"
)

type linearFake struct {
	last, mergeBase, tip string
	commits              []LinearSourceCommit
	seen                 map[string]bool
	trees                map[string]string
	conflict             string
	merges               []string
	actions, messages    []string
	pushes, aborts       int
}

func (f *linearFake) LatestTrailer(string, string) (string, error) { return f.last, nil }
func (f *linearFake) MergeBase(string, string) (string, error)     { return f.mergeBase, nil }
func (f *linearFake) FirstParentRange(from, _ string) ([]LinearSourceCommit, error) {
	for i, c := range f.commits {
		if c.SHA == from {
			return f.commits[i+1:], nil
		}
	}
	return f.commits, nil
}
func (f *linearFake) CardOnBase(_ string, card string) (bool, error) { return f.seen[card], nil }
func (f *linearFake) CherryPick(sha string, mainline bool) error {
	if sha == f.conflict {
		return errors.New("conflict in b.txt")
	}
	f.actions = append(f.actions, sha)
	return nil
}
func (f *linearFake) Squash(head string) error {
	f.actions = append(f.actions, "squash "+head)
	return nil
}
func (f *linearFake) Tree(ref string) (string, error) { return f.trees[ref], nil }
func (f *linearFake) Commit(message string) error {
	f.messages = append(f.messages, message)
	return nil
}
func (f *linearFake) Abort() error                                  { f.aborts++; return nil }
func (f *linearFake) Tip() (string, error)                          { return f.tip, nil }
func (f *linearFake) MergeCommits(string, string) ([]string, error) { return f.merges, nil }
func (f *linearFake) Push(string, string) error                     { f.pushes++; return nil }

func threeLinear() []LinearSourceCommit {
	return []LinearSourceCommit{{SHA: "a", Card: "one", Subject: "one"}, {SHA: "b", Card: "two", Subject: "two", Merge: true}, {SHA: "c", Card: "three", Subject: "three"}}
}

func TestLinearReplayThreeCardsThreeCommitsInOrder(t *testing.T) {
	t.Parallel()
	f := &linearFake{mergeBase: "base", tip: "tip", commits: threeLinear(), seen: map[string]bool{}, trees: map[string]string{"HEAD": "same", "forward": "same"}}
	if err := LandLinear(f, "Owner/Repo", "main", "c", []LinearCard{{ID: "forward", Head: "forward", Source: "c", Forward: true}}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.actions, ",") != "a,b,c" || len(f.messages) != 3 || f.pushes != 1 {
		t.Fatalf("actions=%v messages=%d pushes=%d", f.actions, len(f.messages), f.pushes)
	}
	for i, c := range threeLinear() {
		if !strings.Contains(f.messages[i], "Nova-Card: "+c.Card) || !strings.Contains(f.messages[i], "Nova-Linear-Source: "+c.SHA) {
			t.Fatalf("message %d: %q", i, f.messages[i])
		}
	}
}

func TestLinearConflictRefusesNothingPushed(t *testing.T) {
	t.Parallel()
	f := &linearFake{mergeBase: "base", commits: threeLinear(), seen: map[string]bool{}, conflict: "b", trees: map[string]string{}}
	err := LandLinear(f, "owner/repo", "main", "c", []LinearCard{{ID: "forward", Head: "forward", Source: "c", Forward: true}})
	if err == nil || !strings.Contains(err.Error(), "card two") || !strings.Contains(err.Error(), "commit b") || f.pushes != 0 || f.aborts != 1 {
		t.Fatalf("err=%v pushes=%d aborts=%d", err, f.pushes, f.aborts)
	}
}

func TestLinearResumeAfterPartialLand(t *testing.T) {
	t.Parallel()
	f := &linearFake{last: "a", tip: "tip", commits: threeLinear(), seen: map[string]bool{"one": true}, trees: map[string]string{"HEAD": "same", "forward": "same"}}
	if err := LandLinear(f, "owner/repo", "main", "c", []LinearCard{{ID: "forward", Head: "forward", Source: "c", Forward: true}}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.actions, ",") != "b,c" {
		t.Fatalf("actions=%v", f.actions)
	}
}

func TestLinearNeverPushesAMergeCommit(t *testing.T) {
	t.Parallel()
	f := &linearFake{tip: "tip", seen: map[string]bool{}, trees: map[string]string{}, merges: []string{"deadbeef"}}
	err := LandLinear(f, "owner/repo", "main", "", nil)
	if err == nil || !strings.Contains(err.Error(), "owner/repo:main") || !strings.Contains(err.Error(), "deadbeef") || f.pushes != 0 {
		t.Fatalf("err=%v pushes=%d", err, f.pushes)
	}
}

func TestLinearBasesSetting(t *testing.T) {
	t.Parallel()
	s := settingsSnapshot(nil)
	bad := Set(s, SetReq{LinearBases: "owner/repo", Who: "coord"})
	if len(bad.Refused) != 1 || len(bad.Props) != 0 || !strings.Contains(bad.Refused[0].Why, "owner/name:branch") {
		t.Fatalf("bad plan: %+v", bad)
	}
	good := Set(s, SetReq{LinearBases: "RobotGameDevelopers/VirtualGo:main", Who: "coord"})
	if len(good.Refused) != 0 || len(good.Props) == 0 {
		t.Fatalf("good plan: %+v", good)
	}
	s.Work.SetProp(PropLinearBases, "RobotGameDevelopers/VirtualGo:main")
	if !IsLinearBase(s, "https://github.com/robotgamedevelopers/virtualgo.git", "main") || IsLinearBase(s, "robotgamedevelopers/virtualgo", "dev") {
		t.Fatal("linear base matching")
	}
}
