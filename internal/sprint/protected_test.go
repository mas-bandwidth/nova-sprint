package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lander never lands on a protected branch, dev or main, whatever the stream's mark
// (docs/SPEC-SPRINT.md section 7, the protected branches): land_protected is no landing
// permission, so a stream carrying it from an old store is held to the same rule, and the
// one command that fixes a card meant for such a branch re-points the stream's base.
func TestLanderNeverLandsOnAProtectedBranch(t *testing.T) {
	t.Parallel()
	const repo = "mas-bandwidth/nova-tools"
	w := streamsWorld(t, "s1")

	for _, base := range ProtectedBranches {
		why := ProtectedLandWhy(w.s, "s1", repo, base, "c1")
		assert.Contains(t, why, "card c1 lands on "+base+", a protected branch of "+repo, base)
		assert.Contains(t, why, LandingFlow, base)
		assert.Contains(t, why, "nova-sprint stream set s1 --base sprint/s1", base)
	}
	assert.Empty(t, ProtectedLandWhy(w.s, "s1", repo, "sprint/mechanical-2026-10-02", "c1"), "a sprint branch is not protected")
	assert.Contains(t, ProtectedLandWhy(w.s, "s1", "", DevBranch, "c1"), "of its repository (it names no REPO: line)")
	assert.Contains(t, ProtectedLandWhy(w.s, "s1", "", DevBranch, "c1"), "nova-sprint stream set s1 --base sprint/s1")

	m := streamsWorld(t, "s1")
	m.s.StreamCtl("s1").Fields[FieldLandProtected] = LandProtectedAny // an old store's mark
	require.True(t, IsPromotionStream(m.s, "s1"))
	for _, base := range ProtectedBranches {
		assert.NotEmpty(t, ProtectedLandWhy(m.s, "s1", repo, base, "c1"), "a marked stream no longer lands on "+base)
	}

	d := streamsWorld(t, "s1")
	d.s.StreamCtl("s1").Fields[FieldLandProtected] = repo
	d.must(Set(d.s, SetReq{Streams: []string{"s1"}, LandProtected: ReadTierDefault, Who: "coordinator"}))
	assert.False(t, d.s.StreamCtl("s1").Has(FieldLandProtected), "default takes an old mark off")

	p := Set(d.s, SetReq{Streams: []string{"s1"}, LandProtected: repo, Who: "coordinator"})
	require.NotEmpty(t, p.Refused, "a non-default mark is refused")
	assert.Contains(t, p.Refused[0].Why, LandingFlow)
	assert.Empty(t, p.Units)
}
