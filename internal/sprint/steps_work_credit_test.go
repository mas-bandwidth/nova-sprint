package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnOutOfCreditTakeSpendsNoRedeal: a take the provider refused for want of credit
// (class out-of-credit, a 402) on a LIMITED route rests the provider exactly as it always
// has, but its card is returned untouched: withdrawn WITHOUT FieldTakeEnded, so no redeal
// and no attempt is spent, the primary back in ready, and no failed-work judgment. The
// refusal is still recorded (ProviderTake), which is what the provider's rest is read from
// (routeEnds, providerRestsDue); only FieldTakeEnded and the redeal are left off. The one
// no-spend path is the same one a rate limit uses (rateLimitedReturned); an auth refusal
// (a 401) and an unlimited route keep takeEnded's path.
func TestAnOutOfCreditTakeSpendsNoRedeal(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proMercury(w)
	// a limited route: its model carries an rpm, so the card is dealt a rate budget
	w.s.Fleet.SetProps(map[string]string{PropModelRPM("inception/mercury-3"): "10"})
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Work.Card("s1-1").F("work")
	member := w.s.Fleet.Card(wc).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	attempt := w.s.Work.Card("s1-1").Int("attempt")
	require.NotZero(t, attempt, "the take cut an attempt")

	p := Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "provider failure: provider: class=out-of-credit status=402 msg=Insufficient Balance"})
	require.Len(t, p.Units, 1, "%+v", p)
	assert.Contains(t, p.Units[0].Moved, "returned untouched")
	w.must(p)

	c := w.s.Fleet.Card(wc)
	assert.Equal(t, ProviderFailures, c.Col)
	assert.Empty(t, c.F(FieldTakeEnded), "no take ended: no redeal spent")
	assert.Zero(t, c.Int("redeals"), "the redeal is unspent")
	takes, _ := ProviderTakes(c)
	require.Len(t, takes, 1, "the refusal is recorded so the provider rests as before")
	assert.Equal(t, Ready, w.state("s1-1"), "the primary goes back to ready")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"), "the same attempt, unspent")
	assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")

	rests := RestsDue(w.s)
	require.Len(t, rests, 1, "the provider rests on the refusal: %+v", rests)
	assert.Equal(t, "", rests[0].Route, "the rest is the provider's, never a route's")
	assert.Equal(t, "inception", rests[0].Provider)
	assert.Equal(t, RestCredit, rests[0].Cause)
}
