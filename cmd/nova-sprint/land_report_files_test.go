package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The lander refuses a head whose diff adds or modifies RESULT.md or JOB.md at the
// repository root, naming the file and saying report files live in the job dir only; a
// deletion of one (the fix), a rename away from the root name, and a report file below the
// root land as before (docs/SPEC-SPRINT.md section 7, the lander's checks).
func TestLandRefusesAReportFileAtTheRepositoryRoot(t *testing.T) {
	t.Parallel()
	added := "diff --git a/RESULT.md b/RESULT.md\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ b/RESULT.md\n@@ -0,0 +1 @@\n+Head: 2dc17c32\n"
	modified := "diff --git a/JOB.md b/JOB.md\nindex 1111111..2222222 100644\n--- a/JOB.md\n+++ b/JOB.md\n@@ -1 +1 @@\n-a\n+b\n"
	deleted := "diff --git a/RESULT.md b/RESULT.md\ndeleted file mode 100644\nindex 1111111..0000000\n--- a/RESULT.md\n+++ /dev/null\n@@ -1 +0,0 @@\n-Head: 2dc17c32\n"
	renamedIn := "diff --git a/notes.md b/RESULT.md\nsimilarity index 100%\nrename from notes.md\nrename to RESULT.md\n"
	renamedOut := "diff --git a/RESULT.md b/docs/result-example.md\nsimilarity index 100%\nrename from RESULT.md\nrename to docs/result-example.md\n"
	nested := "diff --git a/testdata/RESULT.md b/testdata/RESULT.md\nnew file mode 100644\n--- /dev/null\n+++ b/testdata/RESULT.md\n@@ -0,0 +1 @@\n+Verdict: LAND\n"
	code := "diff --git a/a.go b/a.go\nindex 1..2 100644\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-package a\n+package a // b\n"
	for _, tc := range []struct {
		name, diff string
		want       []string
	}{
		{"an added RESULT.md", added, []string{"RESULT.md"}},
		{"a modified JOB.md", modified, []string{"JOB.md"}},
		{"both, beside code", code + added + modified, []string{"RESULT.md", "JOB.md"}},
		{"a rename onto RESULT.md", renamedIn, []string{"RESULT.md"}},
		{"a deleted RESULT.md is the fix", deleted, nil},
		{"a rename away from RESULT.md", renamedOut, nil},
		{"a RESULT.md below the root", nested, nil},
		{"code alone", code, nil},
		{"no diff", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rootReportFiles(tc.diff))
			why := reportFileRefusal(tc.diff)
			assert.Len(t, why, len(tc.want))
			for i, f := range tc.want {
				assert.Contains(t, why[i], "it adds or modifies "+f+" at the repository root: report files live in the job dir only")
			}
		})
	}
}
