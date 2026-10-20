package card

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A brief that tells the worker to HOLD on a defect in a file its PATHS names —
// or a directory that prefixes one — contradicts itself: it owns the file and
// says to stop on a defect in it, so one attempt holds while the next fixes it
// and attempts alternate. The brief lint says so before the card is added.
func TestHoldOnADefectInAPathsFileIsAFinding(t *testing.T) {
	t.Parallel()
	for _, brief := range []string{
		"RESULT: x tier: pro\nPATHS: internal/x/runtime/load.nut\n\nTHE TASK. HOLD on a defect in internal/x/runtime/load.nut.\n",
		"RESULT: x tier: pro\nPATHS: internal/x/runtime/load.nut\n\nTHE TASK. HOLD if the runtime under internal/x/runtime/ is wrong.\n",
	} {
		f := Checks("x", brief, Options{})
		require.Len(t, f, 1, brief)
		assert.Equal(t, "hold-on-own-paths", f[0].Check)
		assert.Equal(t, 4, f[0].Line)
		assert.Contains(t, f[0].Excerpt, "internal/x/runtime")
	}
}

// A HOLD on a defect in a file PATHS does not name is no self-contradiction: the
// brief does not own it, so holding on a defect there is fine.
func TestHoldOnAFileOutsidePathsIsNoFinding(t *testing.T) {
	t.Parallel()
	brief := "RESULT: x tier: pro\nPATHS: internal/x/runtime/load.nut\n\nTHE TASK. HOLD on a defect in internal/y/other.go.\n"
	assert.Empty(t, Checks("x", brief, Options{}))
}

// The card contract's HOLD lines — finish HOLD with --failed, Verdict: LAND|HOLD —
// are the RULES paragraph, not a task sentence, and raise nothing.
func TestHoldInTheReportContractIsNoFinding(t *testing.T) {
	t.Parallel()
	brief := "RESULT: x tier: pro\nPATHS: internal/x/runtime/load.nut\n\nRULES.\nfinish HOLD with --failed\nVerdict: LAND|HOLD\n"
	assert.Empty(t, Checks("x", brief, Options{}))
}
