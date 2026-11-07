package forge

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/mas-bandwidth/nova-tools": "mas-bandwidth/nova-tools", "git@github.com:o/r.git": "o/r",
		"mas-bandwidth/schema": "mas-bandwidth/schema", "https://github.com/o/r/": "o/r",
	} {
		if got := Slug(in); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// The first failing lines of a real failed job's log (nova-tools run
// 42347621761, 2026-11-07), as gh run view --log-failed prints them.
func TestPickFailing(t *testing.T) {
	log := "unit (linux, x64, bench)\tunit tests\t2026-11-07T06:38:36.2060033Z ok  \tgithub.com/mas-bandwidth/nova-tools/v2/sandbox\t(cached)\n" +
		"unit (linux, x64, bench)\tunit tests\t2026-11-07T06:38:36.2450987Z --- FAIL: TestBudgets (0.00s)\n" +
		"unit (linux, x64, bench)\tunit tests\t2026-11-07T06:38:36.2452566Z     boundary_test.go:343: bus is 3030 non-test .go lines, budget 3025\n" +
		"unit (linux, x64, bench)\tunit tests\t2026-11-07T06:38:36.2513609Z ##[error]Process completed with exit code 1.\n"
	got := PickFailing(log, 2)
	want := []string{"--- FAIL: TestBudgets (0.00s)", "boundary_test.go:343: bus is 3030 non-test .go lines, budget 3025"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	if got := PickFailing("a\nb\nc\n", 2); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("no failure lines: the last ones, got %q", got)
	}
}

// The host's gh is used when the deployer chose it, its login not refused;
// MergePR falls through a method the ruleset refuses and returns the merge
// commit; Runs counts the reported checks.
func TestHostIdentityMergePRAndRuns(t *testing.T) {
	var calls []string
	c := New(Config{Identity: Identity{Name: "studio", Host: true}, Now: func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			line := strings.Join(cmd.Argv[1:], " ")
			calls = append(calls, line)
			switch {
			case strings.HasPrefix(line, "auth status"):
				return edge.Result{Stdout: "Logged in to github.com account gafferongames"}, nil
			case strings.Contains(line, "pr merge") && strings.Contains(line, "--merge"):
				return edge.Result{ExitCode: 1, Stderr: "Merge commits are not allowed on this repository"}, nil
			case strings.Contains(line, "pr merge"):
				return edge.Result{}, nil
			case strings.HasPrefix(line, "pr view"):
				return edge.Result{Stdout: `{"number":7,"url":"u","state":"MERGED","headRefOid":"` + strings.Repeat("a", 40) + `","mergeCommit":{"oid":"` + strings.Repeat("m", 40) + `"}}`}, nil
			}
			return edge.Result{ExitCode: 1, Stderr: "unexpected " + line}, nil
		})})
	sha, err := c.MergePR(context.Background(), "https://github.com/o/r", 7, strings.Repeat("a", 40), MergeMethods)
	if err != nil || sha != strings.Repeat("m", 40) {
		t.Fatalf("sha %q err %v calls %q", sha, err, calls)
	}
	if !slices.ContainsFunc(calls, func(l string) bool { return strings.Contains(l, "--squash") && strings.Contains(l, "--repo o/r") }) {
		t.Fatalf("squash after merge refused, by slug: %q", calls)
	}
	if r := classifyRollup_internal("s", "PENDING", nil); r.Runs != 0 || r.State != CheckPending {
		t.Fatalf("no checks: %+v", r)
	}
	if r := classifyRollup_internal("s", "PENDING", []gqlNode{{Name: "unit", Status: "IN_PROGRESS"}}); r.Runs != 1 {
		t.Fatalf("one check: %+v", r)
	}
}
