package cardgen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checksOf are the distinct check names of findings.
func checksOf(fs []LintFinding) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		out[f.Check] = true
	}
	return out
}

// 2026-10-18: nova-card lint held a brief to swarm.LintCardHeader with required set, the
// typed-header lint of nova-swarm lint --card, and nova-sprint add never runs it (add runs
// lintBriefReads and card.Checks). So a card add admits came back from nova-card lint red:
// `KIND: fix` is no kind of pkg/hygiene's list (fix-red, ledger, ...), the template has no
// START line, and `TEST: none because the cases are C++ checks: <case>` is a TEST line the
// sprint's template accepts. nova-card lint answers as add answers, from the one function
// both call: a brief of the sprint's template form raises no kind-declared, start-named or
// test-named finding.
func TestALintAgreesWithAddOnTheSprintsTemplateForm(t *testing.T) {
	t.Parallel()
	base := Render(header, Card{ID: "a", File: "x/y.go", Paths: []string{"x/y.go"}, Test: "x TestA", Tier: "pro", Kind: "fix-red", Task: "Do it."})
	require.Empty(t, Lint("a", base), "the rendered brief is the clean base")

	var kept []string
	for _, l := range strings.Split(base, "\n") {
		if !strings.HasPrefix(l, "START:") {
			kept = append(kept, l)
		}
	}
	form := strings.Join(kept, "\n")
	form = strings.Replace(form, "KIND: fix-red", "KIND: fix", 1)
	form = strings.Replace(form, "TEST: x TestA", "TEST: none because the cases are C++ checks: ParseCase", 1)
	require.NotEqual(t, base, form)

	got := checksOf(Lint("a", form))
	for _, check := range []string{"kind-declared", "start-named", "test-named"} {
		assert.False(t, got[check], "%s is raised by nova-card lint and never by add: %v", check, got)
	}
}
