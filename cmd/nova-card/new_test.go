package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
)

// nova-card new writes a brief from its parts that the add's lint admits with no
// edit, to stdout or --out; a missing part is refused by its flag and nothing is
// written; --batch writes one admitted brief per row, and a task_file that cannot
// be read refuses, naming the row and the path, rather than becoming the task.
func TestNewWritesALintCleanBriefFromItsParts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	taskFile := filepath.Join(dir, "TASK.md")
	taskText := "Add nova-card new writing a lint-clean brief from its parts."
	require.NoError(t, os.WriteFile(taskFile, []byte(taskText+"\n"), 0o644))
	parts := [][2]string{
		{"--repo", "mas-bandwidth/nova-sprint"},
		{"--base", "main"},
		{"--task-file", taskFile},
		{"--paths", "cmd/nova-card/**,internal/card/**"},
		{"--test", "./cmd/nova-card TestNewWritesALintCleanBriefFromItsParts"},
		{"--gate", "./cmd/nova-card/ ./internal/card/"},
		{"--tier", "pro"},
	}
	args := func(id string, drop string, extra ...string) []string {
		a := []string{"new"}
		if id != "" {
			a = append(a, id)
		}
		for _, p := range parts {
			if p[0] != drop {
				a = append(a, p[0], p[1])
			}
		}
		return append(a, extra...)
	}

	code, stdout, stderr := runStub(args("card-one", "", "--shared", "docs/CLI.md", "--needs", "card-zero")...)
	require.Equal(t, 0, code, stderr)
	assert.Empty(t, stderr)
	assert.Empty(t, cardgen.Lint("card-one", stdout), "the brief is admitted by the add's lint with no edit")
	for _, want := range []string{
		"RESULT: card-one tier: pro\n",
		"REPO: mas-bandwidth/nova-sprint\n",
		"BASE: main\n",
		"DEPENDS-ON: card-zero\n",
		"PATHS: cmd/nova-card/**,internal/card/**\n",
		"SHARED: docs/CLI.md\n",
		"TEST: ./cmd/nova-card TestNewWritesALintCleanBriefFromItsParts\n",
		"Deadline: finish within 60 minutes.\n",
		"You are a child of the coordinator: one task",
		"RULES.\n",
		"ATTRIBUTION. ",
		"THE TASK. " + taskText + " ",
		"STEP 1. ", "STEP 2. ", "STEP 3. ",
		"STEP 4. Run the gate: go vet ./cmd/nova-card/ ./internal/card/ and go test -count=1 -timeout 600s ./cmd/nova-card/ ./internal/card/, and read the last line of each. Run gofmt -l",
		"STEP 5. ", "against main", "STEP 6. End as JOB.md says",
	} {
		assert.Contains(t, stdout, want)
	}

	// --out writes the same brief to the file, and says so on stdout
	out := filepath.Join(dir, "card-one.md")
	code, stdout2, stderr := runStub(args("card-one", "", "--shared", "docs/CLI.md", "--needs", "card-zero", "--out", out)...)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "CARD OK file="+out+"\n", stdout2)
	written, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, stdout, string(written))

	// each missing part is refused by its flag, exit 2, nothing on stdout
	for _, drop := range []string{"<id>", "--repo", "--base", "--task-file", "--paths", "--test", "--gate", "--tier"} {
		id := "card-one"
		if drop == "<id>" {
			id = ""
		}
		code, stdout, stderr := runStub(args(id, drop)...)
		assert.Equal(t, 2, code, drop)
		assert.Empty(t, stdout, drop)
		assert.Contains(t, stderr, "nova-card new REFUSED: missing "+drop, drop)
	}
	code, _, stderr = runStub("new")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "missing <id>, --repo, --base, --task-file, --paths, --test, --gate, --tier;")

	// a wrong part is refused naming it
	for _, tc := range []struct{ flag, value, says string }{
		{"--task-file", filepath.Join(dir, "TSAK.md"), "cannot read --task-file " + filepath.Join(dir, "TSAK.md")},
		{"--tier", "huge", `--tier "huge"; want frontier, heavy, pro or flash`},
		{"--gate", "go test ./cmd/nova-card/", "is a command; name the packages"},
		{"--test", "TestOnly", "--test: TEST TestOnly is not"},
	} {
		a := args("card-one", tc.flag, tc.flag, tc.value)
		code, stdout, stderr := runStub(a...)
		assert.Equal(t, 2, code, tc.flag)
		assert.Empty(t, stdout, tc.flag)
		assert.Contains(t, stderr, tc.says, tc.flag)
	}

	// --batch with a header: task_file is read, task is inline text
	table := filepath.Join(dir, "cards.tsv")
	require.NoError(t, os.WriteFile(table, []byte(
		"id\ttask_file\ttask\tpaths\ttest\tgate\ttier\tneeds\n"+
			"batch-one\t"+taskFile+"\t\tcmd/nova-card/**\t./cmd/nova-card TestA\t./cmd/nova-card/\tpro\t-\n"+
			"# a comment row\n"+
			"batch-two\t\tFix the inline task.\tinternal/card/**\t./internal/card TestB\t./internal/card/\tflash\tbatch-one\n"), 0o644))
	briefs := filepath.Join(dir, "briefs")
	code, stdout, stderr = runStub("new", "--batch", table, "--out", briefs, "--repo", "mas-bandwidth/nova-sprint", "--base", "main")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "CARD batch-one task=task_file "+taskFile+"\nCARD batch-two task=task\nCARDS OK dir="+briefs+" cards=2\n", stdout)
	for id, task := range map[string]string{"batch-one": taskText, "batch-two": "Fix the inline task."} {
		raw, err := os.ReadFile(filepath.Join(briefs, id+".md"))
		require.NoError(t, err, id)
		assert.Empty(t, cardgen.Lint(id, string(raw)), id)
		assert.Contains(t, string(raw), "THE TASK. "+task+" ", id)
	}

	// a task_file that cannot be read is refused by row and path, and nothing is written
	missing := filepath.Join(dir, "NO-SUCH-TASK.md")
	require.NoError(t, os.WriteFile(table, []byte(
		"id\ttask_file\tpaths\ttest\tgate\ttier\n"+
			"batch-three\t"+taskFile+"\tcmd/nova-card/**\t./cmd/nova-card TestA\t./cmd/nova-card/\tpro\n"+
			"batch-four\t"+missing+"\tcmd/nova-card/**\t./cmd/nova-card TestA\t./cmd/nova-card/\tpro\n"), 0o644))
	empty := filepath.Join(dir, "empty")
	code, stdout, stderr = runStub("new", "--batch", table, "--out", empty, "--repo", "mas-bandwidth/nova-sprint", "--base", "main")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "row 3 (batch-four): cannot read task_file "+missing)
	assert.NoDirExists(t, empty)

	// a header column the table does not know is refused, never left at its default
	require.NoError(t, os.WriteFile(table, []byte("id\ttask_fiel\n"), 0o644))
	code, _, stderr = runStub("new", "--batch", table, "--out", empty)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, `header column "task_fiel" is not one of`)

	// with no header, column 2 is a file when one is there and the text otherwise, and the line says which
	require.NoError(t, os.WriteFile(table, []byte(
		"plain-one\t"+taskFile+"\tcmd/nova-card/**\t./cmd/nova-card TestA\t./cmd/nova-card/\tpro\n"+
			"plain-two\tFix it inline.\tcmd/nova-card/**\t./cmd/nova-card TestA\t./cmd/nova-card/\tflash\n"), 0o644))
	plain := filepath.Join(dir, "plain")
	code, stdout, stderr = runStub("new", "--batch", table, "--out", plain, "--repo", "mas-bandwidth/nova-sprint", "--base", "main")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "CARD plain-one task=column 2 file "+taskFile+"\nCARD plain-two task=column 2 inline\nCARDS OK dir="+plain+" cards=2\n", stdout)

	// a batch row missing a part is refused by its id
	require.NoError(t, os.WriteFile(table, []byte("plain-one\t"+taskFile+"\tcmd/nova-card/**\n"), 0o644))
	code, _, stderr = runStub("new", "--batch", table, "--out", filepath.Join(dir, "none"), "--repo", "r/r", "--base", "main")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "card plain-one is missing --test, --gate, --tier")

	// -h is the verb's help, exit 0
	code, stdout, _ = runStub("new", "-h")
	assert.Equal(t, 0, code)
	assert.True(t, strings.Contains(stdout, "--task-file") && strings.Contains(stdout, "--batch"), stdout)
}
