// Opus breaker probe tests (l4l5-break, 2026-11-06).
package forge

// Breaker findings against the GitHub edge (unit: edge.FakeExec plays gh, no
// network, clock from 1000). Each test fails on 6bb85564.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

const breakSHA = "abcdefabcdefabcdefabcdefabcdefabcdefabcd"

// breakClient_internal is a Client whose gh is fex: auth status answers as
// rowan-claude and every `gh api graphql` answers graphql.
func breakClient_internal(graphql string) (*Client, *edge.FakeExec) {
	fex := edge.NewFakeExec()
	fex.On("gh", func(call edge.FakeCall) edge.FakeOutcome {
		argv := call.Cmd.Argv
		switch {
		case len(argv) >= 3 && argv[1] == "auth" && argv[2] == "status":
			return edge.FakeOutcome{Stdout: "github.com\n  Logged in to github.com account rowan-claude (GH_TOKEN)\n"}
		case len(argv) >= 3 && argv[1] == "api" && argv[2] == "graphql":
			return edge.FakeOutcome{Stdout: graphql}
		}
		return edge.FakeOutcome{ExitCode: 1, Stderr: "unexpected gh call"}
	})
	now := int64(1000)
	c := New(Config{Identity: Identity{Name: "sprint", Token: "tok"}, Exec: fex, Now: func() int64 { return now }})
	return c, fex
}

func rollupJSON_internal(state string, nodes []map[string]string) string {
	ns := make([]any, len(nodes))
	for i, n := range nodes {
		ns[i] = n
	}
	b, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{
		"c0": map[string]any{"statusCheckRollup": map[string]any{"state": state, "contexts": map[string]any{"nodes": ns}}},
	}}})
	return string(b)
}

// TestBreakChecksGreenWhileRollupFails
//
// Sequence: a commit with 101 check runs; GitHub's statusCheckRollup.state
// is FAILURE because the 101st failed, and contexts(first: 100) returns the
// 100 green ones. Checks(sha) is asked.
//
// Broken: Guarantee 4 ("Base advances only to a sha whose checks passed at
// that exact sha"). Checks asks for the first 100 contexts with no pageInfo,
// and classifyRollup_internal reads the rollup's own state only when no node
// came back, so a FAILURE rollup with 100 visible green nodes is CheckGreen
// and LandPass pushes base.
func TestBreakChecksGreenWhileRollupFails(t *testing.T) {
	t.Parallel()
	nodes := make([]map[string]string, 100)
	for i := range nodes {
		nodes[i] = map[string]string{"__typename": "CheckRun", "name": fmt.Sprintf("job-%03d", i), "status": "COMPLETED", "conclusion": "SUCCESS"}
	}
	c, _ := breakClient_internal(rollupJSON_internal("FAILURE", nodes))
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{breakSHA})
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	if m[breakSHA].State == CheckGreen {
		t.Fatalf("rollup state FAILURE classified %s: the 101st check is never read and the rollup's own state is ignored", m[breakSHA].State)
	}
}

// TestBreakCheckNamedRateLimitRestsIdentity
//
// Sequence: a green commit whose CI includes a job named "API rate limit
// test" (status COMPLETED, conclusion SUCCESS). Checks(sha) is asked, then
// asked again.
//
// Broken: LAYER-4-EDGES.md Built, "classifies outcomes (green, red,
// pending, fault, rate_limit)": runGH_internal runs detectRateLimit_internal
// over stdout+stderr, so the words "rate limit" in a check name (or a PR
// title, or a log URL) in a successful gh response make every Checks a
// FaultRateLimit and rest the identity for 60 s, again on each try: the
// stream's landing faults forever on a green sha.
func TestBreakCheckNamedRateLimitRestsIdentity(t *testing.T) {
	t.Parallel()
	nodes := []map[string]string{{"__typename": "CheckRun", "name": "API rate limit test", "status": "COMPLETED", "conclusion": "SUCCESS"}}
	c, _ := breakClient_internal(rollupJSON_internal("SUCCESS", nodes))
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{breakSHA})
	var fe *FaultError
	if errors.As(err, &fe) {
		t.Fatalf("green response (exit 0) classified as %s, identity rests until %d: %v", fe.Kind, c.RestedUntil(), err)
	}
	if err != nil || m[breakSHA].State != CheckGreen {
		t.Fatalf("want green, got %+v err=%v", m[breakSHA], err)
	}
}

// TestBreakChecksTrustsResponseForOtherSHA
//
// Sequence: a gh (or proxy, or GitHub Enterprise shim) answers the c0 alias
// with a different commit (oid 0000...) whose rollup is SUCCESS.
//
// Broken: Built, "Queries check runs at the exact 40-hex commit SHA": the
// GraphQL query never selects `oid`, so parseChecksGraphQL_internal cannot
// check that the object answered is the sha asked; green for another commit
// is taken as green for this one. (Real GitHub resolves object(oid:) to the
// sha asked; this is defence in depth the edge does not have.)
func TestBreakChecksTrustsResponseForOtherSHA(t *testing.T) {
	t.Parallel()
	other := strings.Repeat("0", 40)
	raw := fmt.Sprintf(`{"data":{"repository":{"c0":{"oid":%q,"statusCheckRollup":{"state":"SUCCESS","contexts":{"nodes":[{"__typename":"CheckRun","name":"ci","status":"COMPLETED","conclusion":"SUCCESS"}]}}}}}}`, other)
	c, fex := breakClient_internal(raw)
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{breakSHA})
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	var q string
	for _, call := range fex.Calls() {
		if len(call.Cmd.Argv) > 4 && call.Cmd.Argv[2] == "graphql" {
			q = call.Cmd.Argv[4]
		}
	}
	if m[breakSHA].State == CheckGreen {
		t.Fatalf("green accepted for %s from an object with oid %s (query selects oid: %v)", breakSHA, other, strings.Contains(q, " oid "))
	}
}
