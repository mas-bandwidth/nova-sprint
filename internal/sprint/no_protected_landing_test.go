package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No stream lands a card on a protected branch, dev or main, and the mark is no landing
// permission (docs/SPEC-SPRINT.md section 7, the protected branches; sprint.LandingFlow):
// `stream set --promotion`, `stream set --land-protected <anything but default>`,
// `stream set --base dev|main`, `add --land-protected`, `card base <id> dev|main` and a
// land pass whose base is dev or main are each refused, exit 2, nothing written, naming the
// stream, quoting the flow and the one command that fixes it, `nova-sprint stream set <s>
// --base sprint/<s>`; `--promotion=false` and `--land-protected default` take an old mark off.

func TestStreamSetPromotionIsRefusedNamingTheFlow(t *testing.T) {
	t.Parallel()
	w := streamsWorld(t, "s")
	p := Set(w.s, SetReq{Streams: []string{"s"}, LandProtected: LandProtectedAny, Who: "coordinator"})
	require.NotEmpty(t, p.Refused, "--promotion is refused")
	assert.Contains(t, p.Refused[0].Why, LandingFlow)
	assert.Contains(t, p.Refused[0].Why, "nova-sprint stream set s --base sprint/s")
	assert.Empty(t, p.Units, "nothing written")
	assert.False(t, w.s.StreamCtl("s").Has(FieldLandProtected), "nothing written")
}

func TestStreamSetBaseDevOrMainIsRefused(t *testing.T) {
	t.Parallel()
	for _, base := range ProtectedBranches {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			w := streamsWorld(t, "s")
			p := Set(w.s, SetReq{Streams: []string{"s"}, Base: base, Who: "coordinator"})
			require.NotEmpty(t, p.Refused, base)
			assert.Contains(t, p.Refused[0].Why, LandingFlow, base)
			assert.Contains(t, p.Refused[0].Why, "nova-sprint stream set s --base sprint/s", base)
			assert.Empty(t, p.Units, base)
		})
	}
}

func TestALandProtectedMarkNoLongerAdmitsALandingOnDev(t *testing.T) {
	t.Parallel()
	const repo = "mas-bandwidth/nova-tools"
	w := streamsWorld(t, "s1")
	ctl := w.s.StreamCtl("s1")
	if ctl.Fields == nil {
		ctl.Fields = map[string]string{}
	}
	ctl.Fields[FieldLandProtected] = LandProtectedAny // an old store's mark
	require.True(t, IsPromotionStream(w.s, "s1"), "the old mark is still read as the promotion stream")
	why := ProtectedLandingWhy(w.s, "s1", repo, DevBranch)
	assert.NotEmpty(t, why, "a marked stream no longer lands on dev")
	assert.Contains(t, why, LandingFlow)
	assert.Contains(t, why, "nova-sprint stream set s1 --base sprint/s1")
}

func TestCardBaseOntoMainIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(Add(w.s, AddReq{Brief: "tier: pro\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\n\nThe task.\n", Stream: "s1", Count: 1}))
	accepted(w, "s1-1")
	p := CardBase(w.s, CardBaseReq{ID: "s1-1", Base: "main", OnOrigin: true, Who: "coordinator"})
	require.NotEmpty(t, p.Refused, "card base onto main is refused")
	assert.Contains(t, p.Refused[0].Why, LandingFlow)
	assert.Contains(t, p.Refused[0].Why, "nova-sprint stream set s1 --base sprint/s1")
	assert.Empty(t, p.Units, "nothing written")
}

func TestTakingTheOldMarkOffIsAccepted(t *testing.T) {
	t.Parallel()
	w := streamsWorld(t, "s")
	ctl := w.s.StreamCtl("s")
	if ctl.Fields == nil {
		ctl.Fields = map[string]string{}
	}
	ctl.Fields[FieldLandProtected] = LandProtectedAny // an old store's mark
	require.True(t, IsPromotionStream(w.s, "s"))
	w.must(Set(w.s, SetReq{Streams: []string{"s"}, LandProtected: ReadTierDefault, Who: "coordinator"}))
	assert.False(t, w.s.StreamCtl("s").Has(FieldLandProtected), "--promotion=false / --land-protected default takes the mark off")
}

func TestNoRefusalNamesTheMarkAsARemedy(t *testing.T) {
	t.Parallel()
	w := streamsWorld(t, "s1")
	refusals := []string{
		ProtectedLandingWhy(w.s, "s1", "owner/repo", DevBranch),
		ProtectedLandingWhy(w.s, "s1", "owner/repo", "main"),
		ProtectedLandWhy(w.s, "s1", "owner/repo", DevBranch, "c1"),
		PromotionBaseWhy(w.s, "s1", DevBranch, "c1"),
		PromotionBaseWhy(w.s, "s1", "main", "c1"),
		SprintBranchWhy(w.s, "s1", DevBranch, "c1"),
		SprintBranchWhy(w.s, "s1", "main", "c1"),
	}
	if p := Set(w.s, SetReq{Streams: []string{"s1"}, LandProtected: LandProtectedAny, Who: "coordinator"}); len(p.Refused) > 0 {
		for _, r := range p.Refused {
			refusals = append(refusals, r.Why)
		}
	}
	for _, why := range refusals {
		assert.NotContains(t, why, "--promotion", why)
		assert.NotContains(t, why, "--land-protected <", why)
	}
}
