package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// A staging refusal for want of a bench mirror is a repository capability
// (mirrorRefusals), and the tick's mirror need is the set of members that lack
// the mirror for each repository, one judgment per repository, naming them.

// TestMirrorNeedFromAStagingRefusal: studio refused nova-sprint at staging for want of a
// bench mirror; the need names nova-sprint and studio, and the tick raises one judgment for
// it, naming studio.
func TestMirrorNeedFromAStagingRefusal(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, mirrorErr, t0.Add(-time.Minute))
	needs := mirrorNeeds(s)
	require.Len(t, needs, 1)
	assert.Equal(t, "mas-bandwidth/nova-sprint", needs[0].Repo)
	assert.Equal(t, []string{"studio"}, needs[0].Members)

	p, _ := TickDeal(s, TickReq{})
	found := false
	for _, n := range p.Notes {
		if n.Type == NMirrorNeed && n.Kind == Judgment {
			found = true
			assert.Equal(t, []string{"studio"}, n.Primaries, "the judgment names the members that lack the mirror")
			assert.Contains(t, n.What, "nova-sprint")
		}
	}
	assert.True(t, found, "the tick raises the mirror-need judgment")
}

// TestMirrorNeedEndsAfterAnOk: studio finishes a nova-sprint card ok; the refusal ends, and
// the need is gone.
func TestMirrorNeedEndsAfterAnOk(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, mirrorErr, t0.Add(-time.Minute))
	s.Fleet.Put(&Card{ID: "ns-x.w1", Row: "studio", Col: DoneOK, Fields: map[string]string{
		"ok": "yes", "member": "studio", "finished": t0.Format(time.RFC3339),
		FieldRepo: "https://github.com/mas-bandwidth/nova-sprint"}})
	assert.Empty(t, mirrorNeeds(s), "studio finished a nova-sprint card ok: the need is served")
}

// TestMirrorNeedEndsPastTheWindow: past StagingMirrorWindow the refusal lapses and the need
// is gone.
func TestMirrorNeedEndsPastTheWindow(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, mirrorErr, t0.Add(-2*StagingMirrorWindow))
	needs := mirrorNeeds(s)
	assert.Empty(t, needs, "the window passed: the need lapses")
}

// TestAddAndTheTickKeepABenchMirrorForEveryNamedRepo: add admits a card whose REPO: names a
// repository no member carries; its plan prints the MIRROR line (sprint.MirrorNotice) and
// the repository is recorded on the stream's control card (FieldRepo, by sprint.Add). A
// staging refusal for a second repository from one member is the tick's mirror need: one
// row for that repository, naming the member that lacks it, one judgment, and the refused
// card is never redealt to a member that lacks the mirror. docs/SPEC-SPRINT.md, the mirror
// need.
func TestAddAndTheTickKeepABenchMirrorForEveryNamedRepo(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.setWidth("studio", 4)
	w.up("studio")
	w.setWidth("b1", 4)
	w.up("b1")
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true}}

	// add a card for a repository no member carries: add prints the MIRROR line, and the
	// repository is recorded on the stream's control card.
	repo := "https://github.com/mas-bandwidth/nova-tools"
	w.must(Add(w.s, AddReq{Stream: "ns", Count: 1, Repo: repo, Brief: "tier: flash\n"}))
	require.Contains(t, w.s.StreamCtl("ns").F(FieldRepo), "mas-bandwidth/nova-tools",
		"add records the repository on the stream's control card")
	lines := MirrorNotice(w.s, []string{repo})
	require.Len(t, lines, 1)
	assert.Equal(t, "MIRROR mas-bandwidth/nova-tools needed on b1, studio", lines[0],
		"add prints the MIRROR line naming the members that lack it")

	// a staging refusal for a second repository from studio: the tick keeps the need,
	// names studio, raises one judgment, and never redeals the card to a member that
	// lacks the mirror.
	ref := "https://github.com/mas-bandwidth/nova-sprint"
	w.s.Fleet.Put(&Card{ID: "ns1.w1", Row: "b1", Col: Withdrawn, Fields: map[string]string{
		"gen":                  "1",
		FieldRepo:              ref,
		FieldStagingTake + "1": ProviderTake{Member: "studio", Finished: t0.Add(-time.Minute).Format(time.RFC3339), Error: mirrorErr}.String(),
	}})
	needs := mirrorNeeds(w.s)
	require.Len(t, needs, 1)
	assert.Equal(t, "mas-bandwidth/nova-sprint", needs[0].Repo)
	assert.Equal(t, []string{"studio"}, needs[0].Members,
		"the need names the member that lacks the mirror")

	p2, _ := TickDeal(w.s, TickReq{})
	judged := 0
	for _, n := range p2.Notes {
		if n.Type == NMirrorNeed && n.Kind == Judgment {
			judged++
			assert.Equal(t, []string{"studio"}, n.Primaries, "the judgment names the members that lack it")
			assert.Contains(t, n.What, "nova-sprint", "the judgment names the repository")
		}
	}
	assert.Equal(t, 1, judged, "one judgment is raised per repository")
	assert.Equal(t, []string{"studio"}, notLaunching(w.s, []string{"studio", "b1"}, &Card{
		ID: "ns-2.w2", Row: "studio", Col: Ready, Fields: map[string]string{FieldRepo: ref}}),
		"a card of the refused repository is not redealt to studio while b1 is free")
}
