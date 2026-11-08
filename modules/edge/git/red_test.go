package git

import (
	"context"
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// redPass_internal is one rung-1 pass with the gated head's rollup r and,
// when base is set, the base tip's rollup.
func redPass_internal(t *testing.T, r forge.CheckRollup, base *forge.CheckRollup) LandResult {
	t.Helper()
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, r)
	if base != nil {
		ff.SetCheck("o/r", root, *base)
	}
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || gf.OriginRef("v2") != root {
		t.Fatalf("pass %+v err=%v: red never lands", res, err)
	}
	return res
}

// A red head whose base is red at its tip too is ci_red, one judgement at
// once, with the base's failing checks beside the head's so the
// coordinator weighs main's red and the card's together (LANDING.md "A red
// base"); a green base leaves BaseFailing empty.
func TestLandRedOnARedBaseNamesBothSides(t *testing.T) {
	t.Parallel()
	head := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "unit"}, URL: "https://github.com/o/r/actions/runs/1/job/2"}
	res := redPass_internal(t, head, &forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}})
	if res.Outcome != OutcomeRed || !slices.Equal(res.BaseFailing, []string{"corpus"}) || !slices.Equal(res.FailingCI, head.Failing) || len(res.FailingLines) == 0 {
		t.Fatalf("%+v, want ci_red with the base failing corpus and the head's lines", res)
	}
	res = redPass_internal(t, head, &forge.CheckRollup{State: forge.CheckGreen})
	if res.Outcome != OutcomeRed || len(res.BaseFailing) != 0 {
		t.Fatalf("%+v, want ci_red with no base failing", res)
	}
}
