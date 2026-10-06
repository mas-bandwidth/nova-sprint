package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// newArgs is one whole `nova-card new` call: every part a brief is written from.
func newArgs(taskFile string) []string {
	return []string{"new", "skeleton-a-w1",
		"--repo", "example/repo", "--base", "main", "--task-file", taskFile,
		"--paths", "cmd/nova-x/**,internal/x/**,docs/CLI.md", "--shared", "docs/CLI.md",
		"--test", "./cmd/nova-x TestXDoesY", "--gate", "./cmd/nova-x/,./internal/x/,./internal/ci/",
		"--tier", "heavy", "--needs", "skeleton-b-w1"}
}

// A brief from new passes the card lint with no edit: the add's model lines, the
// default child rules, the held rules file by reference, the typed header and the card
// checks; it carries every part it was given; and a missing part is refused naming it
// (docs/CLI.md, nova-card new).
func TestNewWritesALintCleanBriefFromItsParts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	taskFile := filepath.Join(dir, "task.md")
	require.NoError(t, os.WriteFile(taskFile, []byte("The x tool drops a y. Make it keep the y.\n"), 0o644))

	exit, brief, stderr := runCard(newArgs(taskFile)...)
	require.Equal(t, 0, exit, "stderr: %s", stderr)
	assert.Empty(t, card.NewLint("skeleton-a-w1", brief, card.Options{}, nil), "the brief:\n%s", brief)
	m, why := cardhdr.ReadModel(brief)
	assert.Empty(t, why)
	assert.Equal(t, "heavy", m.Tier)
	assert.Empty(t, swarm.LintCardChildWith([]byte(brief), swarm.DefaultChildRules))
	rules, err := swarm.HeldRules(swarm.DefaultRulesName)
	require.NoError(t, err)
	assert.Empty(t, swarm.LintCardChildByReference([]byte(brief), rules))
	for _, want := range []string{
		"\nREPO: example/repo\n", "\nBASE: main\n", "\nDEPENDS-ON: skeleton-b-w1\n",
		"\nPATHS: cmd/nova-x/**, internal/x/**, docs/CLI.md\n", "\nSHARED: docs/CLI.md\n",
		"\nTEST: ./cmd/nova-x TestXDoesY\n", "\nTHE TASK. The x tool drops a y. Make it keep the y.\n",
		"go vet and go test -count=1 -timeout 600s on ./cmd/nova-x/ ./internal/x/ ./internal/ci/,",
		"\nSTEP 1. ", "\nSTEP 6. ", swarm.ChildRulesParagraph(), card.AttributionSentence,
	} {
		assert.Contains(t, brief, want)
	}
	v, _ := swarm.CardHeaderValue([]byte(brief), "SHARED")
	assert.Equal(t, "docs/CLI.md", v, "SHARED: is read off the typed header, as the add reads it")

	// the same brief through --out, then held by nova-card lint
	out := filepath.Join(dir, "skeleton-a-w1.md")
	exit, stdout, stderr := runCard(append(newArgs(taskFile), "--out", out)...)
	require.Equal(t, 0, exit, stderr)
	assert.Equal(t, "CARD OK file="+out+"\n", stdout)
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, brief, string(raw))
	exit, stdout, _ = runCard("lint", "--card", out)
	assert.Equal(t, 0, exit, stdout)

	// --rules quotes a rules file's sentences after the default rules: the brief passes
	// the add under that file carried, and under the default rules
	fleet := filepath.Join("..", "..", "internal", "fleetrules", "child-rules.txt")
	rules, err = swarm.ReadChildRules(fleet)
	require.NoError(t, err)
	exit, ruled, stderr := runCard(append(newArgs(taskFile), "--rules", fleet)...)
	require.Equal(t, 0, exit, "stdout: %s\nstderr: %s", ruled, stderr)
	assert.Empty(t, swarm.LintCardChildWith([]byte(ruled), rules))
	assert.Empty(t, swarm.LintCardChildWith([]byte(ruled), swarm.DefaultChildRules))
	assert.Empty(t, card.NewLint("skeleton-a-w1", ruled, card.Options{}, rules))
	assert.Contains(t, ruled, "\nNever rebase.\n")

	// a missing part is refused naming it, and nothing is printed
	for flag, name := range map[string]string{"--repo": "--repo", "--task-file": "--task-file", "--test": "--test", "--gate": "--gate", "--tier": "--tier", "--paths": "--paths", "--base": "--base"} {
		args := newArgs(taskFile)
		for i, a := range args {
			if a == flag {
				args = append(args[:i:i], args[i+2:]...)
				break
			}
		}
		exit, stdout, stderr := runCard(args...)
		assert.Equal(t, 2, exit, "without %s", flag)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "nova-card new REFUSED: missing "+name, "without %s", flag)
	}
	exit, _, stderr = runCard(append([]string{"new"}, newArgs(taskFile)[2:]...)...)
	assert.Equal(t, 2, exit)
	assert.Contains(t, stderr, "missing <id>")

	// an invalid part is refused naming what is wrong
	exit, _, stderr = runCard(append(newArgs(taskFile), "--tier", "turbo")...)
	assert.Equal(t, 2, exit)
	assert.Contains(t, stderr, `tier "turbo"`)
	dotted := newArgs(taskFile)
	dotted[1] = "skeleton-a.w1"
	exit, _, stderr = runCard(dotted...)
	assert.Equal(t, 2, exit, "nova-sprint add refuses a brief file whose name is no card id")
	assert.Contains(t, stderr, `id "skeleton-a.w1" is not a card id`)

	// a brief the lint would refuse is a LINT DRIFT line and is not written: a TEST
	// whose package PATHS does not name
	red := filepath.Join(dir, "red.md")
	exit, stdout, _ = runCard(append(newArgs(taskFile), "--test", "./cmd/other TestZ", "--out", red)...)
	assert.Equal(t, 1, exit)
	assert.Contains(t, stdout, "check=test-outside-paths")
	assert.NoFileExists(t, red)
}

// new --batch writes one brief per row into a directory for nova-sprint add --brief-dir,
// each lint-clean, and a row missing a part refuses the whole batch naming the row, with
// nothing written.
func TestNewBatchWritesOneBriefPerRow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("Do a.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("Do b.\n"), 0o644))
	tsv := filepath.Join(dir, "cards.tsv")
	rows := "id\trepo\tbase\ttask-file\tpaths\tshared\ttest\tgate\ttier\tneeds\n" +
		"card-a-w1\texample/repo\tmain\ta.md\tinternal/a/**\t-\tinternal/a TestA\tinternal/a\tflash\t-\n" +
		"card-b-w1\texample/repo\tmain\tb.md\tinternal/b/**,docs/CLI.md\tdocs/CLI.md\tinternal/b TestB\tinternal/b,internal/ci\tpro\tcard-a-w1\n"
	require.NoError(t, os.WriteFile(tsv, []byte(rows), 0o644))
	out := filepath.Join(dir, "cards")
	exit, stdout, stderr := runCard("new", "--batch", tsv, "--out", out)
	require.Equal(t, 0, exit, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Equal(t, "CARDS OK dir="+out+" cards=2\n", stdout)
	for _, id := range []string{"card-a-w1", "card-b-w1"} {
		raw, err := os.ReadFile(filepath.Join(out, id+".md"))
		require.NoError(t, err)
		assert.Empty(t, card.NewLint(id, string(raw), card.Options{}, nil), "%s:\n%s", id, raw)
	}
	raw, _ := os.ReadFile(filepath.Join(out, "card-b-w1.md"))
	assert.Contains(t, string(raw), "\nDEPENDS-ON: card-a-w1\n")
	assert.Contains(t, string(raw), "\nTHE TASK. Do b.\n")

	bad := filepath.Join(dir, "bad.tsv")
	require.NoError(t, os.WriteFile(bad, []byte(rows+"card-c-w1\texample/repo\tmain\ta.md\tinternal/c/**\t-\tinternal/c TestC\t-\tflash\t-\n"), 0o644))
	empty := filepath.Join(dir, "none")
	exit, _, stderr = runCard("new", "--batch", bad, "--out", empty)
	assert.Equal(t, 2, exit)
	assert.Contains(t, stderr, "row 4 (card-c-w1): missing --gate")
	assert.NoDirExists(t, empty)
	exit, _, stderr = runCard("new", "--batch", tsv, "--out", out)
	assert.Equal(t, 2, exit, "a directory that already holds briefs is refused")
	assert.Contains(t, stderr, "already holds")
}
