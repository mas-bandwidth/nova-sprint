package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// pagedClient_internal is a Client whose gh answers a commit's checks a
// page of 100 at a time (unit: edge.FakeExec plays gh, clock from 1000),
// the first page with the per-state counts and their total, and calls,
// which counts the gh api graphql calls made.
func pagedClient_internal(state string, total int, counts map[string]int, nodes []map[string]string) (*Client, func() int) {
	page := func(from int) string {
		to := min(from+100, len(nodes))
		ns := make([]any, 0, 100)
		for _, n := range nodes[from:to] {
			ns = append(ns, n)
		}
		contexts := map[string]any{"nodes": ns, "pageInfo": map[string]any{"hasNextPage": to < len(nodes), "endCursor": fmt.Sprintf("at%d", to)}}
		if from == 0 {
			var by []any
			for _, st := range slices.Sorted(maps.Keys(counts)) {
				by = append(by, map[string]any{"state": st, "count": counts[st]})
			}
			contexts["totalCount"], contexts["checkRunCountsByState"], contexts["statusContextCountsByState"] = total, by, []any{}
		}
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{
			"c0": map[string]any{"oid": breakSHA, "statusCheckRollup": map[string]any{"state": state, "contexts": contexts}},
		}}})
		return string(b)
	}
	fex := edge.NewFakeExec()
	calls := 0
	fex.On("gh", func(call edge.FakeCall) edge.FakeOutcome {
		argv := call.Cmd.Argv
		if len(argv) < 5 || argv[1] != "api" || argv[2] != "graphql" {
			return edge.FakeOutcome{ExitCode: 1, Stderr: "unexpected gh call"}
		}
		calls++
		from := 0
		if _, rest, ok := strings.Cut(argv[4], `after: "at`); ok {
			fmt.Sscanf(rest, "%d", &from)
		}
		return edge.FakeOutcome{Stdout: page(from)}
	})
	c := New(Config{Identity: Identity{Name: "sprint", Token: "tok"}, Exec: fex, Now: func() int64 { return 1000 }})
	return c, func() int { return calls }
}

// checkRuns_internal is n completed check runs named job-000 up, each with
// the conclusion its index gives.
func checkRuns_internal(n int, conclusion func(i int) string) []map[string]string {
	nodes := make([]map[string]string, n)
	for i := range nodes {
		nodes[i] = map[string]string{"__typename": "CheckRun", "name": fmt.Sprintf("job-%03d", i), "status": "COMPLETED", "conclusion": conclusion(i), "detailsUrl": fmt.Sprintf("https://ci.example/%d", i)}
		if nodes[i]["conclusion"] == "" {
			nodes[i]["status"] = "IN_PROGRESS"
		}
	}
	return nodes
}

// A landing asks a pending CI again every LandPoll, so one read of a
// commit's checks while nothing has failed is one gh call however many
// checks the commit has (326 here, four pages of 100): GitHub's per-state
// counts say that no page holds a failed or faulted check, and whether any
// is still running. Only a commit with a failed or faulted check, or counts
// that do not account for every check, is read page by page, for the names.
func TestChecksReadsEveryPageOnlyForAFailingCommit(t *testing.T) {
	t.Parallel()
	green := func(int) string { return "SUCCESS" }
	for _, c := range []struct {
		name, state string
		total       int
		counts      map[string]int
		conclusion  func(int) string
		want        CheckState
		calls       int
	}{
		{"running, nothing failed", "PENDING", 326, map[string]int{"SUCCESS": 250, "IN_PROGRESS": 50, "QUEUED": 26},
			func(i int) string { return map[bool]string{true: "SUCCESS"}[i < 250] }, CheckPending, 1},
		{"rollup success, running on a later page", "SUCCESS", 326, map[string]int{"SUCCESS": 300, "IN_PROGRESS": 26},
			func(i int) string { return map[bool]string{true: "SUCCESS"}[i < 300] }, CheckPending, 1},
		{"all green", "SUCCESS", 326, map[string]int{"SUCCESS": 300, "SKIPPED": 20, "NEUTRAL": 6, "FAILURE": 0}, green, CheckGreen, 1},
		{"one failed on the last page", "FAILURE", 326, map[string]int{"SUCCESS": 325, "FAILURE": 1},
			func(i int) string { return map[bool]string{true: "FAILURE", false: "SUCCESS"}[i == 325] }, CheckRed, 4},
		{"one cancelled on the last page", "FAILURE", 326, map[string]int{"SUCCESS": 325, "CANCELLED": 1},
			func(i int) string { return map[bool]string{true: "CANCELLED", false: "SUCCESS"}[i == 325] }, CheckFault, 4},
		{"counts short of the total", "SUCCESS", 326, map[string]int{"SUCCESS": 100}, green, CheckGreen, 4},
		{"a state this build does not know", "SUCCESS", 326, map[string]int{"SUCCESS": 325, "MYSTERY": 1}, green, CheckGreen, 4},
		{"no counts at all", "SUCCESS", 0, nil, green, CheckGreen, 4},
	} {
		cl, calls := pagedClient_internal(c.state, c.total, c.counts, checkRuns_internal(326, c.conclusion))
		m, err := cl.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{breakSHA})
		r := m[breakSHA]
		if err != nil || r.State != c.want || calls() != c.calls {
			t.Errorf("%s: %s in %d gh calls (err %v), want %s in %d", c.name, r.State, calls(), err, c.want, c.calls)
		}
		if c.want == CheckRed && (!slices.Equal(r.Failing, []string{"job-325"}) || r.URL != "https://ci.example/325" || !slices.Equal(r.URLs, []string{"https://ci.example/325"})) {
			t.Errorf("%s: failing %v at %q (each at %q), want job-325 with its url", c.name, r.Failing, r.URL, r.URLs)
		}
	}
}
