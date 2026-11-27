package forge

import "testing"

// TestSkippedIsNoVerdict: a skipped check ran nothing, so a commit whose
// checks were all skipped, or that reports none under a SUCCESS rollup, has
// no verdict (absent), never green; skipped beside a check that ran green
// stays green. On 11-16 and 11-17, 41 sprint/ PRs merged into schema main
// with every check skipped. Pure classification: no gh, no clock.
func TestSkippedIsNoVerdict(t *testing.T) {
	t.Parallel()
	run := func(name, conclusion string) gqlNode {
		return gqlNode{Typename: "CheckRun", Name: name, Status: "COMPLETED", Conclusion: conclusion}
	}
	for _, c := range []struct {
		name, rollup string
		nodes        []gqlNode
		want         CheckState
	}{
		{"all skipped", "SUCCESS", []gqlNode{run("unit", "SKIPPED"), run("lint", "SKIPPED")}, CheckAbsent},
		{"zero checks under a SUCCESS rollup", "SUCCESS", nil, CheckAbsent},
		{"neutral and skipped, nothing ran green", "SUCCESS", []gqlNode{run("unit", "NEUTRAL"), run("lint", "SKIPPED")}, CheckAbsent},
		{"green beside skipped", "SUCCESS", []gqlNode{run("unit", "SUCCESS"), run("lint", "SKIPPED")}, CheckGreen},
		{"a status context green beside skipped", "SUCCESS", []gqlNode{{Typename: "StatusContext", Context: "ci/x", State: "SUCCESS"}, run("lint", "SKIPPED")}, CheckGreen},
	} {
		r := classifyRollup_internal(breakSHA, c.rollup, c.nodes)
		if r.State != c.want {
			t.Errorf("%s: %s (%q), want %s", c.name, r.State, r.Reason, c.want)
		}
		if c.want == CheckAbsent && r.Reason == "" {
			t.Errorf("%s: no verdict with no reason", c.name)
		}
	}
}
