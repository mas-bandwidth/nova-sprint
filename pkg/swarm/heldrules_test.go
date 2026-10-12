package swarm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HeldRules reads the rules file this build holds: the fleet file parses to a non-empty
// set whose rules name their source fleet/<name>.
func TestHeldRulesReadsTheHeldFleetFile(t *testing.T) {
	t.Parallel()
	rules, err := HeldRules(DefaultRulesName)
	require.NoError(t, err)
	require.NotEmpty(t, rules)
	for _, r := range rules {
		assert.NotEmpty(t, r.Name, "rule name")
		assert.NotEmpty(t, r.Sentence, "rule sentence")
		assert.True(t, strings.HasPrefix(r.Source, "fleet/"), "source %q", r.Source)
	}
}

// A name this build does not hold, a path that is not a base name, and an empty name are
// refused, never silently read as no rules.
func TestHeldRulesRefusesANameThisBuildDoesNotHold(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "no-such-rules.txt", "../child-rules.txt", "sub/child-rules.txt"} {
		_, err := HeldRules(name)
		require.Error(t, err, name)
	}
	require.NotEmpty(t, HeldRulesNames())
}

// StagedBrief is the brief a child is handed: the card text then the rule set's RULES
// paragraph after one blank line.
func TestStagedBriefAppendsTheRules(t *testing.T) {
	t.Parallel()
	rules := []ChildRule{{Name: "worktree", Sentence: "Work only in the job directory."}}
	got := StagedBrief("RESULT: c1\n\nTHE TASK.\n", rules)
	assert.True(t, strings.HasSuffix(got, "RULES.\n"+rules[0].Sentence), "%q", got)
	assert.Contains(t, got, "THE TASK.\n\nRULES.\n")
}

// A brief that already quotes every sentence is handed as it is: the rules are not
// repeated.
func TestStagedBriefDoesNotRepeatQuotedRules(t *testing.T) {
	t.Parallel()
	rules := []ChildRule{{Name: "worktree", Sentence: "Work only in the job directory."}}
	brief := "RESULT: c1\n\n" + RulesParagraph(rules)
	assert.Equal(t, brief, StagedBrief(brief, rules))
}

// An empty brief and an empty rule set are handed as they are: a card with no brief has no
// task to hold to rules.
func TestStagedBriefLeavesEmptyInputsAlone(t *testing.T) {
	t.Parallel()
	rules := []ChildRule{{Name: "worktree", Sentence: "Work only in the job directory."}}
	assert.Equal(t, "  \n", StagedBrief("  \n", rules))
	assert.Equal(t, "RESULT: c1\n", StagedBrief("RESULT: c1\n", nil))
}

// OwnRulesName is the rules file of the repository the brief names: this repository's
// default, a per-repo file when held, unnamed when the brief names no repo, and "" for a
// repo whose rules this build does not hold. A card with no repository of its own leans on
// the add's held set (unnamed).
func TestOwnRulesNameNamesTheBriefsRepositoryRulesFile(t *testing.T) {
	t.Parallel()
	home := "RESULT: c1 sha=000000000000 tier: pro\nREPO: mas-bandwidth/nova-tools\n\nTHE TASK.\n"
	other := "RESULT: c1 sha=000000000000 tier: pro\nREPO: someone/else\n\nTHE TASK.\n"
	none := "RESULT: c1 sha=000000000000 tier: pro\n\nTHE TASK.\n"
	assert.Equal(t, DefaultRulesName, OwnRulesName(home, "unnamed"))
	assert.Equal(t, "unnamed", OwnRulesName(none, "unnamed"))
	assert.Equal(t, "", OwnRulesName(other, "unnamed"))
}
