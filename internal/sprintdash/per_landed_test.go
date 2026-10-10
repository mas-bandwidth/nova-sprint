package sprintdash

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cost readout (the owner, dogfood 2026-10-10: $7.74 per landed at 6 of 82; the
// stream table $0.03 against the sprint total $46.39): per landed is the landed cards'
// spend per landed card, every attempt, read and rework of each, failed ones included,
// never all spend over the landed count; the spend on cards not yet landed shows beside
// it as its own in-flight figure, so the total is still the whole spend and nothing is
// hidden, and the two figures agree once every card has landed. The stream table's
// total column is the stream's whole spend, so the tiers and the total columns agree
// with the sprint total.

// landedCostDriver draws a where --json snapshot with app.js on the scroll test's DOM
// shim and reads the cost tile and the stream table's row.
const landedCostDriver = `
context.render(input.data);
const txt = id => doc.getElementById(id).textContent;
const kids = id => { const b = doc.getElementById(id); return b.children.length > 1 ? b.children[1].children.map(c => c.textContent) : []; };
process.stdout.write(JSON.stringify({ cost: txt('cost'), per: txt('cost-per'), landed: txt('landed'),
  head: doc.getElementById('top-streams').children[0] ? doc.getElementById('top-streams').children[0].children.map(c => c.textContent) : [],
  row: kids('top-streams') }));
`

// perLandedCopy is the card's readout: two landed cards costing $1 each (one with a
// failed attempt, the two attempts' $0.30 and $0.70), one in-flight card costing $10,
// the stream's landed spend $2 and whole spend $12.
const perLandedCopy = `{"landed":2,"all":3,
"summary":"2/3 66.7% -> ETA 1h",
"streams":[{"Stream":"s1","Release":""}],
"tables":{"work":{"s1":{"cost":"$2.00","landed":"2","waiting":"0","ready":"0","working":"0","review":"0","merging":"1","per_landed":"$1.00"}}},
"stream_costs":{"s1":{"total_cost":"$12.00","landed_cost":"$2.00","work_cost":"$12.00","cost_by_tier":{"flash":"$2.00","pro":"$10.00"}}}}`

// drawLandedCopy renders one copy of the page and reads the cost tile and the stream
// table's row.
func drawLandedCopy(t *testing.T, copy string) map[string]any {
	t.Helper()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NOVA_CI") == "1" {
			require.NoError(t, err, "node is required for the dashboard JS behavioural test")
		}
		t.Skip("node is not installed on this machine; the dashboard JS behavioural test needs it")
	}
	shim, _, ok := strings.Cut(scrollShim, "// the viewer:")
	require.True(t, ok, "the scroll test's shim has its viewer")
	in, err := json.Marshal(map[string]any{"appJS": string(file("app.js")), "data": json.RawMessage(copy)})
	require.NoError(t, err)
	cmd := exec.Command(nodePath, "-e", shim+landedCostDriver)
	cmd.Stdin = bytes.NewReader(in)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	require.NoError(t, cmd.Run(), "node runner failed: %s", errBuf.String())
	require.Empty(t, errBuf.String(), "app.js threw while drawing")
	var res map[string]any
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &res), outBuf.String())
	return res
}

// The cost tile: the total is the whole spend, per card is the landed spend over the
// landed count, and the in-flight spend shows beside it as its own figure.
func TestTheCostTileCountsPerLandedFromTheLandedSpend(t *testing.T) {
	t.Parallel()
	res := drawLandedCopy(t, perLandedCopy)
	assert.Equal(t, "2", res["landed"], "the headline is the landed count")
	assert.Equal(t, "$12.00", res["cost"], "the total is the whole spend, the landed and the in-flight together")
	assert.Equal(t, "$1.00 per card · $10.00 in flight", res["per"],
		"per card is the landed cards' spend ($2) over the landed count (2), never $6.00; the in-flight $10 shows beside it")
}

// The stream table's total column is the stream's whole spend, so the tiers and the
// total columns sum to the sprint total the cost tile reads.
func TestTheStreamTableTotalsAgreeWithTheSprintTotal(t *testing.T) {
	t.Parallel()
	res := drawLandedCopy(t, perLandedCopy)
	assert.Equal(t, []any{"stream", "pro", "flash", "total"}, res["head"])
	assert.Equal(t, []any{"s1", "$10", "$2", "$12"}, res["row"],
		"the tiers are the stream's spend by tier and the total column is its whole spend: $12, the sprint total")
}
