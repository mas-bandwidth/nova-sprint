package git

import (
	"context"
	"testing"
)

// Cold read 2, 2026-11-06, at ef4df30c (Rowan's cold reader). One failing
// probe per confirmed defect.

// Round 2 ruling (landing): a stream with no forge never auto-lands; "no
// forge" is a refusal naming the hand landing, never a green gate.
func TestColdread2DefectLandWithoutForgeOrRepoIsUngated(t *testing.T) {
	t.Parallel()
	gf := NewFake(nil)
	root := gf.AddCommit("", map[string]string{"README": "v2\n"}, "refs/heads/v2")
	h1 := gf.AddCommit(root, map[string]string{"a.txt": "a\n"}, "refs/heads/sprint/s1/c1")
	for _, req := range []LandRequest{
		{Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{h1}, Branches: []string{"sprint/s1/c1"}},
		{Stream: "s1", Base: "v2", Heads: []string{h1}, Branches: []string{"sprint/s1/c1"}},
	} {
		res, err := LandPass(context.Background(), gf, nil, req)
		if err == nil || res.Outcome == OutcomeLanded {
			t.Errorf("landed %+v with no forge (or no repository): no CI ran", res)
		}
	}
}
