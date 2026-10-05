package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
)

func TestNewWritesALintCleanBriefFromItsParts(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskFile := filepath.Join(taskDir, "TASK.md")
	taskText := "Add nova-card new [id] writing a lint-clean brief from its parts."
	require.NoError(t, os.WriteFile(taskFile, []byte(taskText), 0o644))

	// 1. Valid invocation writing to stdout
	args := []string{
		"new", "card-test-1",
		"--repo", "mas-bandwidth/nova-sprint",
		"--base", "main",
		"--task-file", taskFile,
		"--paths", "cmd/nova-card/**,internal/card/**,docs/SPEC-CARD-CONTRACT.md,docs/CLI.md",
		"--shared", "docs/CLI.md",
		"--test", "./cmd/nova-card TestNewWritesALintCleanBriefFromItsParts",
		"--gate", "./cmd/nova-card/ ./internal/card/",
		"--tier", "pro",
		"--needs", "dep-card-1",
	}

	exit, stdout, stderr := runCard(args...)
	require.Equal(t, 0, exit, "stderr: %s", stderr)
	assert.Empty(t, stderr)

	// Verify required sections and lines
	assert.Contains(t, stdout, "RESULT: card-test-1")
	assert.Contains(t, stdout, "tier: pro")
	assert.Contains(t, stdout, "REPO: mas-bandwidth/nova-sprint")
	assert.Contains(t, stdout, "BASE: main")
	assert.Contains(t, stdout, "KIND: fix-red")
	assert.Contains(t, stdout, "DEPENDS-ON: dep-card-1")
	assert.Contains(t, stdout, "PATHS: cmd/nova-card/**,internal/card/**,docs/SPEC-CARD-CONTRACT.md,docs/CLI.md")
	assert.Contains(t, stdout, "SHARED: docs/CLI.md")
	assert.Contains(t, stdout, "TEST: ./cmd/nova-card TestNewWritesALintCleanBriefFromItsParts")
	assert.Contains(t, stdout, "You are a child of the coordinator: one task, one worktree, one branch, unattended.")
	assert.Contains(t, stdout, "Deadline: finish within 60 minutes.")

	// RULES lines
	assert.Contains(t, stdout, "RULES.\n")
	assert.Contains(t, stdout, "Work only in the job directory this card names.")
	assert.Contains(t, stdout, "Never force-push or rebase a shared branch.")
	assert.Contains(t, stdout, "Never kill a process you did not start.")
	assert.Contains(t, stdout, "Never start a server on this machine.")
	assert.Contains(t, stdout, "No `rm -rf` outside the job directory.")
	assert.Contains(t, stdout, "Report what was not done.")

	// Attribution sentence
	assert.Contains(t, stdout, "ATTRIBUTION.")

	// Task and libraries
	assert.Contains(t, stdout, "THE TASK. "+taskText)
	assert.Contains(t, stdout, "Libraries considered:")

	// STEPS
	assert.Contains(t, stdout, "STEP 1.")
	assert.Contains(t, stdout, "STEP 2.")
	assert.Contains(t, stdout, "STEP 3.")
	assert.Contains(t, stdout, "STEP 4. Run the gate: go test -count=1 -timeout 600s ./cmd/nova-card/ ./internal/card/")
	assert.Contains(t, stdout, "STEP 5.")
	assert.Contains(t, stdout, "against main")
	assert.Contains(t, stdout, "STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md)")

	// Lint clean: zero findings
	findings := cardgen.Lint("card-test-1", stdout)
	assert.Empty(t, findings, "brief must be lint clean; got %v", findings)

	// 2. Writing to --out <file>
	outFile := filepath.Join(taskDir, "out-card.md")
	outArgs := append(slicesClone(args), "--out", outFile)
	exit, outStdout, outStderr := runCard(outArgs...)
	require.Equal(t, 0, exit, "stderr: %s", outStderr)
	assert.Empty(t, outStdout)

	outContent, err := os.ReadFile(outFile)
	require.NoError(t, err)
	outFileFindings := cardgen.Lint("card-test-1", string(outContent))
	assert.Empty(t, outFileFindings, "written file must be lint clean; got %v", outFileFindings)

	// 3. Refusals on missing required flags
	missingCases := []struct {
		name    string
		dropArg string
		what    string
	}{
		{"missing id", "id", "id"},
		{"missing repo", "--repo", "repo"},
		{"missing base", "--base", "base"},
		{"missing task-file", "--task-file", "task-file"},
		{"missing paths", "--paths", "paths"},
		{"missing test", "--test", "test"},
		{"missing gate", "--gate", "gate"},
		{"missing tier", "--tier", "tier"},
	}

	for _, tc := range missingCases {
		t.Run(tc.name, func(t *testing.T) {
			var filtered []string
			if tc.dropArg == "id" {
				filtered = []string{"new"}
				for i := 2; i < len(args); i++ {
					filtered = append(filtered, args[i])
				}
			} else {
				filtered = []string{"new", "card-test-1"}
				for i := 2; i < len(args); i++ {
					if args[i] == tc.dropArg {
						i++ // skip flag and value
						continue
					}
					filtered = append(filtered, args[i])
				}
			}
			exit, _, stderr := runCard(filtered...)
			assert.Equal(t, 2, exit, "expected exit 2 for %s", tc.name)
			assert.Contains(t, stderr, tc.what, "expected refusal to name %s", tc.what)
		})
	}

	// 4. Batch mode: nova-card new --batch <tsv> --out <dir>
	batchTSV := filepath.Join(taskDir, "cards.tsv")
	batchDir := filepath.Join(taskDir, "batch-cards")
	tsvContent := "id\trepo\tbase\ttask_file\tpaths\tshared\ttest\tgate\ttier\tneeds\n" +
		"batch-card-1\tmas-bandwidth/nova-sprint\tmain\t" + taskFile + "\tcmd/nova-card/**\tdocs/CLI.md\t./cmd/nova-card TestA\t./cmd/nova-card/\tpro\t-\n" +
		"batch-card-2\tmas-bandwidth/nova-sprint\tmain\t" + taskFile + "\tinternal/cardgen/**\t-\t./internal/cardgen TestB\t./internal/cardgen/\tflash\tbatch-card-1\n"
	require.NoError(t, os.WriteFile(batchTSV, []byte(tsvContent), 0o644))

	batchExit, batchStdout, batchStderr := runCard("new", "--batch", batchTSV, "--out", batchDir)
	require.Equal(t, 0, batchExit, "batch stderr: %s", batchStderr)
	assert.Contains(t, batchStdout, "CARDS OK")

	for _, id := range []string{"batch-card-1", "batch-card-2"} {
		fPath := filepath.Join(batchDir, id+".md")
		assert.FileExists(t, fPath)
		bContent, err := os.ReadFile(fPath)
		require.NoError(t, err)
		bFindings := cardgen.Lint(id, string(bContent))
		assert.Empty(t, bFindings, "%s must be lint clean; got %v", id, bFindings)
	}
}

func slicesClone(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	return out
}
