package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A read handed back on a provider failure is the provider's, not the reader's
// (tla/ReadProviderReturn.tla): it spends the reader only at ProviderReadCap of them at the
// attempt, read from the cards themselves, so returns recorded before the rule spend
// nothing either.

const ultramarine429Return = `no verdict (ran=false verdict=""): provider failure: provider: class=rate-limited status=429 msg=The request exceeds your organization's requests per minute. Retry in 1 s.`

func providerReturned(id, reason string) *Card {
	return &Card{ID: id, Row: "m1", Fields: map[string]string{"retired_by": RetiredByReturned, "reason": reason}}
}

func TestAProviderFailureReturnSpendsNoReader(t *testing.T) {
	t.Parallel()
	s := cEnvSnap([]string{"s1"}, []string{"m1"}, nil)
	s.Work.Put(&Card{ID: "p1", Row: "s1", Col: Review, Fields: map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", "head": "abc"}})
	rid := ReadCardID("p1", 1, "m1")
	s.Fleet.Put(&Card{ID: rid, Row: "m1", Col: Working, Fields: map[string]string{"kind": "read", "primary": "p1", "stream": "s1", "reader": "m1",
		"attempt": "1", "head": "abc", "gen": "1", "asked": stamp(s.Now.Add(-time.Minute)), FieldReadCard: "1"}})
	p := Read(s, ReadReq{Sel: Sel{IDs: []string{rid}}, As: "m1", Return: true, Reason: ultramarine429Return, Who: "m1"})
	require.Empty(t, p.Refused)
	require.Len(t, p.Units, 1)
	rc := envExitEntryOf(p.Units[0], rid)
	assert.Equal(t, RetiredByReturned, rc.Set["retired_by"], "still a return, with its reason")
	assert.Contains(t, p.Units[0].Moved, "a provider failure, its reader not spent")
	assert.False(t, readSpent([]*Card{{ID: rid, Row: "m1", Fields: rc.Set}}), "the reader may be dealt the read again")
}

func TestProviderFailureReturnsSpendOnlyAtTheCap(t *testing.T) {
	t.Parallel()
	base := ReadCardID("p1", 1, "m1")
	var cards []*Card
	for i := 1; i < ProviderReadCap; i++ {
		cards = append(cards, providerReturned(base+".g"+itoa(i), ultramarine429Return))
		assert.False(t, readSpent(cards), "%d provider returns spend nothing", i)
	}
	cards = append(cards, providerReturned(base+".g"+itoa(ProviderReadCap), ultramarine429Return))
	require.True(t, readSpent(cards), "at the cap the reader is spent")
	assert.Equal(t, base+".g"+itoa(ProviderReadCap), readSpender(cards).ID, "the last provider return is the spender")

	mixed := []*Card{providerReturned(base, ultramarine429Return), providerReturned(base+".g1", "no verdict: nothing to read")}
	assert.True(t, readSpent(mixed), "a return of the reader's own still spends it")
	verdict := []*Card{providerReturned(base, ultramarine429Return), {ID: base + ".g1", Row: "m1", Fields: map[string]string{"verdict": "ok", "retired_by": "read"}}}
	assert.True(t, readSpent(verdict), "a verdict spends it")
}

func TestProviderFailureReturnClasses(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]bool{
		ultramarine429Return: true,
		`no verdict (ran=false verdict=""): provider failure: provider: class=provider-5xx status=503 msg=unavailable`:                                   true,
		`no verdict (ran=false verdict=""): provider failure: provider: class=timeout status=- msg=no headers`:                                           true,
		`no verdict (ran=false verdict=""): provider failure: provider: class=other status=502 msg=bad gateway`:                                          true,
		`no verdict (ran=false verdict=""): provider failure: provider: class=out-of-credit status=402 msg=This request requires more credits`:           false,
		`no verdict (ran=false verdict=""): provider failure: provider: class=auth status=401 msg=Missing API key`:                                       false,
		`no verdict (ran=false verdict=""): provider failure: provider: class=other status=429 msg=You exceeded your current quota (insufficient_quota)`: false,
		`no verdict (ran=false verdict=""): the child ended without a result`:                                                                            false,
		`no verdict (ran=true verdict="nothing"): rate limit mentioned in a finding`:                                                                     false,
	} {
		assert.Equal(t, want, ProviderFailureReturn(reason), reason)
	}
}
