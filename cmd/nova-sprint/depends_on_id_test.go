package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// A brief's DEPENDS-ON or Needs item that is not a card id is refused by name (the
// owner's rule of 2026-10-17: writers strict; reads forgiving only if nearly free): the
// card lint reports a depends-on finding naming the item on the header's line. A valid
// list, "none (first card)", "-" and an owner/repo#n reference are still read as today.
func TestADependsOnItemThatIsNotACardIDIsRefusedByName(t *testing.T) {
	t.Parallel()

	brief := "RESULT: s1-2 sha=<sha12> tier: pro\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"KIND: fix\n" +
		"DEPENDS-ON: an earlier card (since split)\n" +
		"PATHS: README.md\n"
	_, findings := lintBriefReads(brief, ruleSet{rules: swarm.DefaultChildRules})
	var dep []swarm.CardHeaderFinding
	for _, f := range findings {
		if f.Check == "depends-on" {
			dep = append(dep, f)
		}
	}
	require.Len(t, dep, 1)
	assert.Equal(t, 4, dep[0].Line)
	assert.Contains(t, dep[0].Excerpt, `"an earlier card"`)

	for _, line := range []string{
		"DEPENDS-ON: s1-1, s1-0 (since split)",
		"DEPENDS-ON: none (first card)",
		"DEPENDS-ON: -",
		"DEPENDS-ON: mas-bandwidth/nova-sprint#12",
	} {
		brief := "RESULT: s1-2 sha=<sha12> tier: pro\n" +
			"REPO: mas-bandwidth/nova-sprint\n" +
			"KIND: fix\n" +
			line + "\n" +
			"PATHS: README.md\n"
		_, findings := lintBriefReads(brief, ruleSet{rules: swarm.DefaultChildRules})
		for _, f := range findings {
			assert.NotEqual(t, "depends-on", f.Check, line)
		}
	}

	brief = "RESULT: s1-2 sha=<sha12> tier: pro\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"KIND: fix\n" +
		"Needs: two words\n" +
		"PATHS: README.md\n"
	_, findings = lintBriefReads(brief, ruleSet{rules: swarm.DefaultChildRules})
	dep = nil
	for _, f := range findings {
		if f.Check == "depends-on" && strings.Contains(f.Excerpt, `"two words"`) {
			dep = append(dep, f)
		}
	}
	require.Len(t, dep, 1)
	assert.Equal(t, 4, dep[0].Line)
}
