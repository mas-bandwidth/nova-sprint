package main

import (
	"strings"
)

// reportFiles are the files a card's child writes in its job directory to report: its
// result and its job. They live in the job directory only, never in the repository; a
// head that carries one to the repository's root poisons every checkout staged from the
// base after it (rocketnet main carried a card's RESULT.md from 3bb8e5f1, so every
// staged checkout began with that card's `Head:` line and pushes were refused across the
// fleet, 2026-10-24; removed in rocketnet#569).
var reportFiles = map[string]bool{"RESULT.md": true, "JOB.md": true}

// rootReportFiles is the report files (reportFiles) at the repository's root that a git
// diff (git diff -M, as checkCard reads the merge) adds or modifies, in diff order, each
// once. A deletion is not one: a change that takes the file away is the fix. A rename is
// judged by its new name. A report file below the root (a testdata fixture, a doc's
// example) is not one.
func rootReportFiles(diff string) []string {
	var out []string
	seen := map[string]bool{}
	file, deleted := "", false
	flush := func() {
		if file != "" && !deleted && reportFiles[file] && !seen[file] {
			seen[file] = true
			out = append(out, file)
		}
		file, deleted = "", false
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			// diff --git a/<old> b/<new>: the new side is after the last " b/"
			if i := strings.LastIndex(line, " b/"); i >= 0 {
				file = strings.Trim(line[i+len(" b/"):], "\"")
			}
		case strings.HasPrefix(line, "deleted file mode"), line == "+++ /dev/null":
			deleted = true
		case strings.HasPrefix(line, "rename to "):
			file = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
		}
	}
	flush()
	return out
}

// reportFileRefusal is the lander's refusal of a head whose diff adds or modifies a
// report file at the repository's root, one line per file; nil when it touches none.
func reportFileRefusal(diff string) []string {
	var why []string
	for _, f := range rootReportFiles(diff) {
		why = append(why, "it adds or modifies "+f+" at the repository root: report files live in the job dir only; remove "+f+" from the branch (git rm --cached "+f+") and push again")
	}
	return why
}
