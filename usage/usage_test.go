package usage

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStringRoundTrip(t *testing.T) {
	line := "input=19541 cache_read=36336 cache_write=0 output=692 reasoning=70 model=deepseek/deepseek-flash models=2 billing=api charged=0.042196 unpriced=no-price wall=49s"
	r, err := Parse(line)
	require.NoError(t, err)
	assert.Equal(t, Tokens{19541, 36336, 0, 692, 70}, r.Tokens)
	assert.Equal(t, "deepseek/deepseek-flash", r.Model)
	assert.Equal(t, 2, r.Models)
	assert.Equal(t, "0.042196", r.Charged)
	assert.Equal(t, []string{"wall=49s"}, r.Extra)
	assert.Equal(t, line, r.String())
	assert.Equal(t, int64(19541+36336+692+70), r.Tokens.Total())
}

func TestUnreportedIsNeverZero(t *testing.T) {
	r, err := Parse("output=5")
	require.NoError(t, err)
	assert.Equal(t, Unreported, r.Tokens.Input)
	assert.True(t, r.Tokens.Reported())
	assert.Equal(t, "output=5", r.String())
	e, err := Parse("")
	require.NoError(t, err)
	assert.False(t, e.Tokens.Reported())
	assert.Equal(t, "", e.String())
	assert.Equal(t, int64(0), e.Tokens.Total())
}

func TestParseRefusesMalformed(t *testing.T) {
	for _, bad := range []string{"input=-1", "input=x", "output", "charged=1e3", "charged=-1", "billing=free", "models=0", "=3"} {
		_, err := Parse(bad)
		assert.Error(t, err, bad)
		assert.Equal(t, []string{bad}, MustParse(bad).Extra, bad)
	}
	r, err := Parse("charged=-")
	require.NoError(t, err)
	assert.Equal(t, "", r.Charged)
}

func TestDecimalAndUSD(t *testing.T) {
	for in, want := range map[string]string{"0.0421960": "0.042196", "007": "7", "1.": "", "1.50": "1.5", "0": "0"} {
		got, err := Decimal(in)
		if want == "" {
			assert.Error(t, err, in)
			continue
		}
		assert.Equal(t, want, got, in)
	}
	assert.Equal(t, "0.042196", USD(42196))
	assert.Equal(t, "12", USD(12_000_000))
	assert.Equal(t, "0", USD(-3))
}

func TestTokensAdd(t *testing.T) {
	a := Tokens{1, Unreported, 3, Unreported, Unreported}
	b := Tokens{Unreported, Unreported, 4, 5, Unreported}
	assert.Equal(t, Tokens{1, Unreported, 7, 5, Unreported}, a.Add(b))
}

func TestBilling(t *testing.T) {
	assert.Equal(t, BillingAPI, Billing("nova-worker"))
	for _, h := range []string{"claude", "codex", "antigravity", "grok", ""} {
		assert.Equal(t, BillingSubscription, Billing(h), h)
	}
}

var sheet = Sheet{
	"deepseek-flash":      {In: 270_000, CacheRead: 70_000, CacheWrite: 270_000, Out: 1_100_000},
	"inception/mercury-3": {In: 250_000, CacheRead: 250_000, CacheWrite: 250_000, Out: 1_000_000},
}

func TestPriceAPIExact(t *testing.T) {
	// 19541*0.27 + 36336*0.07 + 0*0.27 + (692+70)*1.1 = 5276.07 + 2543.52 + 838.2 = 8657.79 micro: 8658 rounded half up
	p, err := Price("input=19541 cache_read=36336 cache_write=0 output=692 reasoning=70 model=deepseek/deepseek-flash", BillingAPI, "0.05", sheet)
	require.NoError(t, err)
	assert.True(t, p.Priced)
	assert.Equal(t, int64(8658), p.Cost)
	assert.Equal(t, "input=19541 cache_read=36336 cache_write=0 output=692 reasoning=70 model=deepseek/deepseek-flash billing=api charged=0.05", p.Record.String())
	// the whole provider/model name is looked up first
	p, err = Price("input=1000000 output=1000000 model=inception/mercury-3", BillingAPI, "", sheet)
	require.NoError(t, err)
	assert.Equal(t, int64(1_250_000), p.Cost)
	assert.Equal(t, "input=1000000 output=1000000 model=inception/mercury-3 billing=api charged=-", p.Record.String())
}

func TestPriceHalfUp(t *testing.T) {
	assert.Equal(t, int64(1), Cost(Tokens{Input: 2, CacheRead: -1, CacheWrite: -1, Output: -1, Reasoning: -1}, Rate{In: 250_000})) // 0.5 -> 1
	assert.Equal(t, int64(0), Cost(Tokens{Input: 1, CacheRead: -1, CacheWrite: -1, Output: -1, Reasoning: -1}, Rate{In: 499_999})) // 0.499999 -> 0
	assert.Equal(t, int64(math.MaxInt64), Cost(Tokens{Input: math.MaxInt64, CacheRead: -1, CacheWrite: -1, Output: -1, Reasoning: -1}, Rate{In: math.MaxInt64}))
}

func TestPriceUnpricedNeverZero(t *testing.T) {
	p, err := Price("", BillingAPI, "0.01", sheet)
	require.NoError(t, err)
	assert.False(t, p.Priced)
	assert.Equal(t, "billing=api charged=0.01 unpriced=no-tokens", p.Record.String())
	p, err = Price("input=10 output=5 model=openai/sol", BillingAPI, "", sheet)
	require.NoError(t, err)
	assert.False(t, p.Priced)
	assert.Equal(t, "input=10 output=5 model=openai/sol billing=api charged=- unpriced=no-price", p.Record.String())
	p, err = Price("input=10 output=5", BillingAPI, "", sheet)
	require.NoError(t, err)
	assert.Equal(t, WhyNoPrice, p.Record.Unpriced, "no model is no price")
}

func TestPriceSubscriptionIsFree(t *testing.T) {
	p, err := Price("input=500 output=20 model=anthropic/opus billing=api unpriced=x", BillingSubscription, "1.25", sheet)
	require.NoError(t, err)
	assert.True(t, p.Priced)
	assert.Equal(t, int64(0), p.Cost)
	assert.Equal(t, "input=500 output=20 model=anthropic/opus billing=subscription charged=1.25", p.Record.String(), "the row decides billing; the kin's dollars are evidence only")
	p, err = Price("", BillingSubscription, "", nil)
	require.NoError(t, err)
	assert.True(t, p.Priced)
	assert.Equal(t, "billing=subscription charged=-", p.Record.String(), "a seat need pass nothing")
}

func TestPriceRefusesBadWords(t *testing.T) {
	_, err := Price("input=lots", BillingAPI, "", sheet)
	assert.Error(t, err)
	_, err = Price("", BillingAPI, "1e3", sheet)
	assert.Error(t, err)
}

func TestCompact(t *testing.T) {
	for n, want := range map[int64]string{0: "0", -5: "0", 999: "999", 1000: "1K", 1049: "1K", 1050: "1.1K", 1_234_567: "1.2M", 2_000_000: "2M",
		48_149_999: "48.1M", 999_950: "1M", 999_949: "999.9K", 7_500_000_000: "7.5B", 3_000_000_000_000: "3T"} {
		assert.Equal(t, want, Compact(n), "%d", n)
	}
}
