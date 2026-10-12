package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checksOf returns the check tokens of findings, so a test can look for one token without
// pinning the order or the count of every other finding.
func checksOf(fs []CardHeaderFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Check)
	}
	return out
}

// A card that quotes the default RULES paragraph verbatim carries every rule: no
// rule- finding is drawn. The paragraph is built from DefaultChildRules, not pasted, so
// it tracks the set.
func TestLintCardChildQuotedRulesParagraphCarriesEveryRule(t *testing.T) {
	t.Parallel()
	card := "RESULT: c1 sha=000000000000 tier: pro\nPATHS: pkg/x.go\n\n" +
		ChildRulesParagraph() + "\nTHE TASK.\n"
	got := LintCardChildWith([]byte(card), DefaultChildRules)
	for _, c := range checksOf(got) {
		assert.False(t, strings.HasPrefix(c, "rule-"), "a quoted rule drew %s: %+v", c, got)
	}
}

// A dropped rule is one rule-<name> finding naming the sentence the card no longer quotes.
func TestLintCardChildNamesADroppedRule(t *testing.T) {
	t.Parallel()
	card := "RESULT: c1 sha=000000000000 tier: pro\nPATHS: pkg/x.go\n\n" +
		ChildRulesParagraph() + "\nTHE TASK.\n"
	card = strings.Replace(card, "Never start a server on this machine.\n", "", 1)
	got := LintCardChildWith([]byte(card), DefaultChildRules)
	assert.Contains(t, checksOf(got), "rule-no-server")
}

// A reworded rule no longer matches the sentence, so it is a missing rule too.
func TestLintCardChildNamesARewordedRule(t *testing.T) {
	t.Parallel()
	card := "RESULT: c1 sha=000000000000 tier: pro\nPATHS: pkg/x.go\n\n" +
		ChildRulesParagraph() + "\nTHE TASK.\n"
	card = strings.Replace(card, "Never start a server on this machine.", "Try not to run a server here.", 1)
	got := LintCardChildWith([]byte(card), DefaultChildRules)
	assert.Contains(t, checksOf(got), "rule-no-server")
}

// A command the card forbids in prose is not a step- finding: the negation in the
// command's own clause is read as prose about the rule. The same command without the
// negation is a violation.
func TestLintCardChildReadsANegatedCommandAsProse(t *testing.T) {
	t.Parallel()
	head := "RESULT: c1 sha=000000000000 tier: pro\nPATHS: pkg/x.go\n\n" + ChildRulesParagraph() + "\n"
	negated := LintCardChildWith([]byte(head+"Never run redis-server here.\n"), DefaultChildRules)
	assert.NotContains(t, checksOf(negated), "step-redis-server", "a negated command drew a finding: %+v", negated)

	violation := LintCardChildWith([]byte(head+"Run redis-server here.\n"), DefaultChildRules)
	assert.Contains(t, checksOf(violation), "step-redis-server")
}

// childNegationInClause reads a negation within three words of the command, and not one
// farther back in the same clause (security#66 finding 3).
func TestChildNegationInClauseReadsOnlyANearNegation(t *testing.T) {
	t.Parallel()
	assert.True(t, childNegationInClause("Never start a "), "a near negation")
	assert.True(t, childNegationInClause("do not run "), "a near negation")
	assert.False(t, childNegationInClause("start a "), "no negation")
	assert.False(t, childNegationInClause("do not ever under any circumstances restart then bind "), "a negation too far back is prose about something else")
}

// An empty card is one finding, not every rule it does not quote.
func TestLintCardChildEmptyCardIsOneFinding(t *testing.T) {
	t.Parallel()
	got := LintCardChildWith([]byte("   \n\n"), DefaultChildRules)
	require.Len(t, got, 1)
	assert.Equal(t, EmptyCardCheck, got[0].Check)
}

// ParseChildRules reads one sentence per line, names a [name] line, and reports every
// problem of the file together.
func TestParseChildRulesReadsAWellFormedFile(t *testing.T) {
	t.Parallel()
	text := "# comment\n[no-force-push] Never force-push.\nA second rule.\n"
	rules, err := ParseChildRules(text, "src")
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, "no-force-push", rules[0].Name)
	assert.Equal(t, "Never force-push.", rules[0].Sentence)
	assert.Equal(t, "src:2", rules[0].Source)
	assert.Equal(t, "line3", rules[1].Name)
	assert.Equal(t, "A second rule.", rules[1].Sentence)
}

// A duplicate rule (same sentence, or same name) is refused, naming the earlier line.
func TestParseChildRulesRefusesADuplicate(t *testing.T) {
	t.Parallel()
	_, err := ParseChildRules("[a] Never do the thing.\n[b] Never do the thing.\n", "src")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeats")

	_, err = ParseChildRules("[a] First.\n[a] Second.\n", "src")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already line")
}

// An empty file (or one of only comments) holds no rule and is refused.
func TestParseChildRulesRefusesAnEmptyFile(t *testing.T) {
	t.Parallel()
	_, err := ParseChildRules("# only a comment\n\n", "src")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rule")
}

// ReadChildRules reads a file and parses it; a missing file is an error.
func TestReadChildRulesReadsTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.txt")
	require.NoError(t, os.WriteFile(p, []byte("[no-kill] Never kill.\n"), 0o644))
	rules, err := ReadChildRules(p)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, "no-kill", rules[0].Name)
	assert.Equal(t, p+":1", rules[0].Source)

	_, err = ReadChildRules(filepath.Join(dir, "absent.txt"))
	require.Error(t, err)
}

// RulesParagraph is the RULES paragraph of a rule set: parsing its rule lines back yields
// the same sentences, so the paragraph round-trips through ParseChildRules.
func TestRulesParagraphRoundTripsParseChildRules(t *testing.T) {
	t.Parallel()
	rules := []ChildRule{
		{Name: "a", Sentence: "First rule."},
		{Name: "b", Sentence: "Second rule, folded over a line."},
	}
	para := RulesParagraph(rules)
	assert.True(t, strings.HasPrefix(para, "RULES.\n"))
	body := strings.TrimPrefix(para, "RULES.\n")
	parsed, err := ParseChildRules(body, "src")
	require.NoError(t, err)
	require.Len(t, parsed, len(rules))
	assert.Equal(t, RulesParagraph(rules), RulesParagraph(parsed))
}

// LintCardChild is LintCardChildWith under the default rules.
func TestLintCardChildUsesTheDefaultRules(t *testing.T) {
	t.Parallel()
	card := "RESULT: c1 sha=0\nPATHS: pkg/x.go\n\n" + ChildRulesParagraph() + "\nTHE TASK.\n"
	got := LintCardChild([]byte(card))
	for _, c := range checksOf(got) {
		assert.False(t, strings.HasPrefix(c, "rule-"), "a quoted rule drew %s: %+v", c, got)
	}
}

// childLibrariesFilled reads what follows a `Libraries considered:` colon: not empty and
// not still an angle-bracket placeholder.
func TestChildLibrariesFilledReadsTheLine(t *testing.T) {
	t.Parallel()
	assert.True(t, childLibrariesFilled("used the stdlib"))
	assert.False(t, childLibrariesFilled(""))
	assert.False(t, childLibrariesFilled("  <what was found>  "))
}

// childHasTimeout reports whether a `go test` command carries a positive -timeout before
// the command ends.
func TestChildHasTimeoutReadsTheTimeout(t *testing.T) {
	t.Parallel()
	assert.True(t, childHasTimeout("go test -timeout 600s ./pkg", 0))
	assert.True(t, childHasTimeout("go test -timeout=10m ./pkg", 0))
	assert.False(t, childHasTimeout("go test ./pkg", 0))
	assert.False(t, childHasTimeout("go test -timeout 0 ./pkg", 0))
}

// childRmInsideJob reports whether every target of a recursive rm is inside the job.
func TestChildRmInsideJobReadsTheTargets(t *testing.T) {
	t.Parallel()
	assert.True(t, childRmInsideJob("rm -rf ./build", 0))
	assert.True(t, childRmInsideJob("rm -rf $PWD/tmp", 0))
	assert.False(t, childRmInsideJob("rm -rf /", 0))
	assert.False(t, childRmInsideJob("rm -rf ../outside", 0))
	assert.False(t, childRmInsideJob("rm -rf", 0))
}

// childInsideJob reports whether a path is inside the job directory.
func TestChildInsideJobReadsThePath(t *testing.T) {
	t.Parallel()
	assert.True(t, childInsideJob("build"))
	assert.True(t, childInsideJob("$PWD/build"))
	assert.True(t, childInsideJob("<job>/build"))
	assert.False(t, childInsideJob("/abs"))
	assert.False(t, childInsideJob("~"))
	assert.False(t, childInsideJob("../up"))
	assert.False(t, childInsideJob("."))
	assert.False(t, childInsideJob("*"))
}

// ChildRemedy is what one child-rule check wants under a rule set: the scan's remedy for a
// step- token, the rule's sentence for a rule- token, and "" for a token that is neither.
func TestChildRemedyAnswersForTheToken(t *testing.T) {
	t.Parallel()
	rules := []ChildRule{{Name: "worktree", Sentence: "Work only in the job directory.", Source: "src"}}
	assert.Contains(t, ChildRemedy(rules, "rule-worktree"), "Work only in the job directory.")
	assert.Contains(t, ChildRemedy(rules, "step-redis-server"), "redis-server")
	assert.Equal(t, EmptyCardRemedy, ChildRemedy(rules, EmptyCardCheck))
	assert.Equal(t, LibrariesConsideredRemedy, ChildRemedy(rules, LibrariesConsideredRule))
	assert.Equal(t, "", ChildRemedy(rules, "no-such-token"))
}

// childClause is the text of a line before a command that belongs to the command's own
// clause: back to the last `;`, `,`, `&&`, `||` or `. `.
func TestChildClauseReadsBackToTheClauseEnd(t *testing.T) {
	t.Parallel()
	assert.Equal(t, " run ", childClause("do this; run "))
	assert.Equal(t, "run ", childClause("run "))
}
