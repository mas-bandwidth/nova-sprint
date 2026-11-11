package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// GitHub rate-limiting the forge's identity is no fact about the landing:
// a PR that could not be read or opened, and checks that could not be
// read, for that reason alone are forge_rested, which the run asks again
// (LAYER-4-EDGES "Rate limits": a fault that spends nothing), never
// pr_unopenable or ci_fault, which are one land judgement per card. Any
// other failure of the same calls is the judgement it was.
func TestLandRateLimitedForgeIsAskedAgainNotJudged(t *testing.T) {
	t.Parallel()
	limited := fmt.Errorf("forge: %w", &forge.FaultError{Kind: forge.FaultRateLimit, Identity: forge.HostLogin, RetryAfter: 1060, Reason: "HTTP 403: API rate limit exceeded"})
	gate := &forge.FaultError{Kind: forge.FaultGate, Identity: forge.HostLogin, Reason: "the gate did not run"}
	for _, c := range []struct {
		name string
		set  func(ff *fakeForge_internal)
		want LandOutcome
	}{
		{"PR rate-limited", func(ff *fakeForge_internal) { ff.OpenErr = limited }, OutcomeRested},
		{"checks rate-limited", func(ff *fakeForge_internal) { ff.ChecksErr = limited }, OutcomeRested},
		{"PR failed otherwise", func(ff *fakeForge_internal) { ff.OpenErr = errors.New("HTTP 403") }, OutcomeUnopenable},
		{"checks failed otherwise", func(ff *fakeForge_internal) { ff.ChecksErr = gate }, OutcomeCIFault},
	} {
		gf, ff, root, c1, _ := landWorld_internal()
		c.set(ff)
		res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
		if err != nil || res.Outcome != c.want || gf.OriginRef("v2") != root {
			t.Errorf("%s: %+v err=%v, want %s and nothing merged", c.name, res, err, c.want)
		}
		if c.want == OutcomeRested && !strings.Contains(res.Reason, "rate limit") {
			t.Errorf("%s: the reason %q does not say the rate limit", c.name, res.Reason)
		}
	}
}
