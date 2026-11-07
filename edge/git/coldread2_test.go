package git

import (
	"context"
	"testing"
)

// Cold read 2, 2026-11-06, at ef4df30c (Rowan's cold reader). One failing
// probe per confirmed defect.

// Round 2 ruling (landing): "No landing without a forge: a stream whose
// settings carry no forge ... never auto-lands; 'no forge' is a refusal naming
// stream set --mode hand, never a green gate." The refusal at land.go:54 fires
// only when Repo is set; with no forge and no repo, gateMergedGroup_internal
// returns green (land.go:146-148) and base moves to a merge no CI ran.
func TestColdread2DefectLandWithoutForgeOrRepoIsUngated(t *testing.T) {
	t.Parallel()
	gf, _, h1, h2 := crFakeWorld_internal(t)
	res, err := LandPass(context.Background(), gf, nil, LandRequest{Stream: "s1", Base: "v2", Heads: []string{h1, h2}})
	if err == nil && res.Outcome == OutcomeLanded {
		t.Errorf("landed %s on v2 with no forge and no repository: no CI ran at that sha", res.GatedSHA)
	}
}
