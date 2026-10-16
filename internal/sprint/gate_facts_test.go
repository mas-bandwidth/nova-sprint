package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAMembersBeatFactsReachItsControlCardAndTheDeal: the facts a member's beat carries
// (fleet beat --facts) are written by the tick's presence onto its control card (FieldFacts),
// and the deal then gives a GATE-NEEDS linux card to the linux member, never to the darwin
// one and never as the NGateNeeds judgment with every member's facts empty (the v1.4.2 fleet,
// 2026-10-16: "no member up satisfies GATE-NEEDS linux (amd-2 (facts: ); hetzner1 (facts: ); …)").
// A second presence with the same beats writes nothing more.
func TestAMembersBeatFactsReachItsControlCardAndTheDeal(t *testing.T) {
	w := fleetWorld(t, 1, 1, "m-darwin", "m-linux")
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	beats := TickReq{Beats: map[string]Beat{
		"m-darwin": {At: w.s.Now, Facts: []string{"darwin", "arm64", "tla"}},
		"m-linux":  {At: w.s.Now, Facts: []string{"linux", "amd64", "landlock", "tla", "go"}},
	}}
	w.part(TickPresence, beats)
	assert.Equal(t, "darwin,arm64,tla", w.s.MemberCtl("m-darwin").F(FieldFacts), "the darwin member's facts are on its control card")
	assert.Equal(t, "linux,amd64,landlock,tla,go", w.s.MemberCtl("m-linux").F(FieldFacts), "the linux member's facts are on its control card")
	assert.Empty(t, factsPlan(w.s, beats).Units, "facts that stand are not written again")

	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	assert.Empty(t, w.notesOf(NGateNeeds), "a member up carries linux: no judgment")
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the GATE-NEEDS linux card is dealt")
	assert.Equal(t, "m-linux", wc.Row, "the GATE-NEEDS linux card goes to the linux member")
}

// TestABeatWithoutFactsLeavesTheControlCard: a beat that carries no facts (a member from
// before v1.4.3) and a stale beat write nothing, so the card keeps what it had.
func TestABeatWithoutFactsLeavesTheControlCard(t *testing.T) {
	w := fleetWorld(t, 0, 1, "m1")
	w.s.MemberCtl("m1").Fields[FieldFacts] = "linux"
	assert.Empty(t, factsPlan(w.s, TickReq{Beats: map[string]Beat{"m1": {At: w.s.Now}}}).Units)
	assert.Empty(t, factsPlan(w.s, TickReq{Beats: map[string]Beat{"m1": {At: w.s.Now.Add(-24 * BeatDeadline), Facts: []string{"darwin"}}}}).Units)
}

// TestFactWords: a beat's --facts is a comma list of fact words; an empty list and a word
// that is not a fact word are refused.
func TestFactWords(t *testing.T) {
	got, err := FactWords("linux, amd64,landlock")
	require.NoError(t, err)
	assert.Equal(t, []string{"linux", "amd64", "landlock"}, got)
	for _, bad := range []string{"", " , ", "linux,--as", "linux,a b"} {
		_, err := FactWords(bad)
		assert.Error(t, err, bad)
	}
}
