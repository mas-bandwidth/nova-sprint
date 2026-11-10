package usage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A real fleet run (the live store, 2026-11-09, yj-start-oom-false.w2 on
// amd-1): recorded unpriced=no-price under a policy whose prices() was {}.
// Table prices it: 101488 x 0.30 + 7906944 x 0.006 + 0 x 0.30 +
// (17506 + 31275) x 1.20 per million = 0.0304464 + 0.047441664 + 0.0585372
// = $0.136425264, half up to the micro-dollar.
const fleetRun = "input=101488 cache_read=7906944 cache_write=0 output=17506 reasoning=31275 model=deepseek/deepseek-flash"

func TestTablePricesTheFleetModels(t *testing.T) {
	t.Parallel()
	p, err := Price(fleetRun, BillingAPI, "", nil)
	require.NoError(t, err)
	assert.True(t, p.Priced, "a bench run is priced from Table when the policy's sheet names no price")
	assert.Equal(t, int64(136_425), p.Cost)
	assert.Equal(t, fleetRun+" billing=api charged=-", p.Record.String())
	for _, m := range []string{"deepseek/deepseek-flash", "deepseek/deepseek-v4-pro", "inception/mercury-3"} {
		_, ok := Table.Lookup(m)
		assert.True(t, ok, m)
	}
	p, err = Price("input=10 output=5 model=openai/sol", BillingAPI, "", nil)
	require.NoError(t, err)
	assert.False(t, p.Priced, "a model in neither sheet stays unpriced, never $0")
	assert.Equal(t, WhyNoPrice, p.Record.Unpriced)
}

func TestPolicySheetOverridesTable(t *testing.T) {
	t.Parallel()
	p, err := Price("input=1000000 model=deepseek/deepseek-flash", BillingAPI, "", Sheet{"deepseek/deepseek-flash": {In: 150_000}})
	require.NoError(t, err)
	assert.Equal(t, int64(150_000), p.Cost, "the policy's price first; Table only for what it leaves out")
}

func TestRepricedCountsStoredNoPriceRuns(t *testing.T) {
	t.Parallel()
	cost, ok := Repriced(fleetRun+" billing=api charged=- unpriced=no-price", 0, false)
	assert.True(t, ok)
	assert.Equal(t, int64(136_425), cost)
	cost, ok = Repriced("input=5 model=x billing=api charged=- unpriced=no-price", 7, true)
	assert.True(t, ok)
	assert.Equal(t, int64(7), cost, "the engine's stored price stands")
	_, ok = Repriced("input=5 model=openai/sol billing=api charged=- unpriced=no-price", 0, false)
	assert.False(t, ok, "a model Table does not price stays unpriced")
	_, ok = Repriced("billing=api charged=- unpriced=no-tokens", 0, false)
	assert.False(t, ok, "no tokens is no price")
	_, ok = Repriced("input=5 model=deepseek/deepseek-flash billing=subscription charged=-", 0, false)
	assert.False(t, ok, "a seat run is free")
}
