package forge

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// Ran names every check read on a commit; one settled by GitHub's counts
// without every page read is RanPartial, so the landing never calls a
// check missing from Ran "not run" (LANDING.md, "A red base").
func TestChecksNameEveryCheckThatRan(t *testing.T) {
	t.Parallel()
	green := func(int) string { return "SUCCESS" }
	cl, _ := pagedClient_internal("FAILURE", 326, map[string]int{"SUCCESS": 325, "FAILURE": 1},
		checkRuns_internal(326, func(i int) string { return map[bool]string{true: "FAILURE", false: "SUCCESS"}[i == 325] }))
	m, err := cl.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{breakSHA})
	if r := m[breakSHA]; err != nil || len(r.Ran) != 326 || r.Ran[0] != "job-000" || r.Ran[325] != "job-325" || r.RanPartial {
		t.Fatalf("every page read: ran %d (partial %v), err %v", len(r.Ran), r.RanPartial, err)
	}
	cl, _ = pagedClient_internal("SUCCESS", 326, map[string]int{"SUCCESS": 326}, checkRuns_internal(326, green))
	m, err = cl.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{breakSHA})
	if r := m[breakSHA]; err != nil || len(r.Ran) != 100 || !r.RanPartial {
		t.Fatalf("settled by its counts: ran %d (partial %v), err %v; want the first page and partial", len(r.Ran), r.RanPartial, err)
	}
}

// basePRClient is a Client whose gh answers BasePR's query with raw.
func basePRClient(raw string) *Client {
	fex := edge.NewFakeExec()
	fex.On("gh", func(call edge.FakeCall) edge.FakeOutcome {
		if a := call.Cmd.Argv; len(a) < 5 || a[1] != "api" || a[2] != "graphql" || !strings.Contains(a[4], "associatedPullRequests") {
			return edge.FakeOutcome{ExitCode: 1, Stderr: "unexpected gh call"}
		}
		return edge.FakeOutcome{Stdout: raw}
	})
	return New(Config{Identity: Identity{Name: "sprint", Token: "tok"}, Exec: fex, Now: func() int64 { return 1000 }})
}

// BasePR is the merged PR whose merge commit is the sha, with whether its
// head's tree is the sha's tree; a sha pushed with no PR, or associated
// only with PRs that did not merge it, has none.
func TestBasePRIsThePRThatMergedTheTip(t *testing.T) {
	t.Parallel()
	head := strings.Repeat("d", 40)
	pr := func(merged bool, mergeOID, lastOID, tree string) string {
		return fmt.Sprintf(`{"number": 7, "url": "https://github.com/o/r/pull/7", "merged": %v, "headRefOid": %q, "mergeCommit": {"oid": %q},
			"commits": {"nodes": [{"commit": {"oid": %q, "tree": {"oid": %q}}}]}}`, merged, head, mergeOID, lastOID, tree)
	}
	raw := func(prs ...string) string {
		return fmt.Sprintf(`{"data": {"repository": {"c0": {"oid": %q, "tree": {"oid": "t1"}, "associatedPullRequests": {"nodes": [%s]}}}}}`, breakSHA, strings.Join(prs, ","))
	}
	got, ok, err := basePRClient(raw(pr(true, breakSHA, head, "t1"))).BasePR(context.Background(), "o/r", breakSHA)
	if err != nil || !ok || got != (BasePR{Number: 7, URL: "https://github.com/o/r/pull/7", HeadSHA: head, SameTree: true}) {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	got, ok, _ = basePRClient(raw(pr(true, breakSHA, head, "t2"))).BasePR(context.Background(), "o/r", breakSHA)
	if !ok || got.SameTree {
		t.Fatalf("another tree: %+v %v", got, ok)
	}
	got, ok, _ = basePRClient(raw(pr(true, breakSHA, strings.Repeat("e", 40), "t1"))).BasePR(context.Background(), "o/r", breakSHA)
	if !ok || got.SameTree {
		t.Fatalf("a last commit that is not the head: %+v %v", got, ok)
	}
	for name, r := range map[string]string{
		"no PR":                 raw(),
		"a PR not merged":       raw(pr(false, breakSHA, head, "t1")),
		"merged as another sha": raw(pr(true, strings.Repeat("f", 40), head, "t1")),
	} {
		if got, ok, err := basePRClient(r).BasePR(context.Background(), "o/r", breakSHA); ok || err != nil {
			t.Errorf("%s: %+v %v %v, want none", name, got, ok, err)
		}
	}
}
