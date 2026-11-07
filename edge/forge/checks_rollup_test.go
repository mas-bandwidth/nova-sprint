package forge

// The GitHub edge's check classification (unit: edge.FakeExec plays gh, no
// network, clock from 1000).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

const rollupSHA = "abcdefabcdefabcdefabcdefabcdefabcdefabcd"

// rollupClient_internal is a Client whose gh is fex: auth status answers as
// ci-bot and every `gh api graphql` answers graphql.
func rollupClient_internal(graphql string) (*Client, *edge.FakeExec) {
	fex := edge.NewFakeExec()
	fex.On("gh", func(call edge.FakeCall) edge.FakeOutcome {
		argv := call.Cmd.Argv
		switch {
		case len(argv) >= 3 && argv[1] == "auth" && argv[2] == "status":
			return edge.FakeOutcome{Stdout: "github.com\n  Logged in to github.com account ci-bot (GH_TOKEN)\n"}
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

// TestChecksGreenWhileRollupFails: a commit with 101 check runs whose
// statusCheckRollup.state is FAILURE (the 101st failed) while the first 100
// contexts are green is never CheckGreen. Base advances only to a sha whose
// checks passed at that exact sha, so the rollup's own state counts even when
// every visible node is green.
func TestChecksGreenWhileRollupFails(t *testing.T) {
	t.Parallel()
	nodes := make([]map[string]string, 100)
	for i := range nodes {
		nodes[i] = map[string]string{"__typename": "CheckRun", "name": fmt.Sprintf("job-%03d", i), "status": "COMPLETED", "conclusion": "SUCCESS"}
	}
	c, _ := rollupClient_internal(rollupJSON_internal("FAILURE", nodes))
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{rollupSHA})
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	if m[rollupSHA].State == CheckGreen {
		t.Fatalf("rollup state FAILURE classified %s: the 101st check is never read and the rollup's own state is ignored", m[rollupSHA].State)
	}
}

// TestCheckNamedRateLimitRestsIdentity: a green commit whose CI includes a
// job named "API rate limit test" (COMPLETED, SUCCESS) is read as green. The
// words "rate limit" in a check name, PR title or log URL of a successful gh
// response must not be classified as FaultRateLimit, which would rest the
// identity and fault the stream's landing forever on a green sha.
func TestCheckNamedRateLimitRestsIdentity(t *testing.T) {
	t.Parallel()
	nodes := []map[string]string{{"__typename": "CheckRun", "name": "API rate limit test", "status": "COMPLETED", "conclusion": "SUCCESS"}}
	c, _ := rollupClient_internal(rollupJSON_internal("SUCCESS", nodes))
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{rollupSHA})
	var fe *FaultError
	if errors.As(err, &fe) {
		t.Fatalf("green response (exit 0) classified as %s, identity rests until %d: %v", fe.Kind, c.RestedUntil(), err)
	}
	if err != nil || m[rollupSHA].State != CheckGreen {
		t.Fatalf("want green, got %+v err=%v", m[rollupSHA], err)
	}
}

// TestChecksTrustsResponseForOtherSHA: when gh (or a proxy or Enterprise
// shim) answers the c0 alias with a different commit (oid 0000...) whose
// rollup is SUCCESS, that is not green for the sha asked. The query selects
// `oid` and parseChecksGraphQL_internal checks that the object answered is
// the sha asked (defence in depth: GitHub resolves object(oid:) exactly).
func TestChecksTrustsResponseForOtherSHA(t *testing.T) {
	t.Parallel()
	other := strings.Repeat("0", 40)
	raw := fmt.Sprintf(`{"data":{"repository":{"c0":{"oid":%q,"statusCheckRollup":{"state":"SUCCESS","contexts":{"nodes":[{"__typename":"CheckRun","name":"ci","status":"COMPLETED","conclusion":"SUCCESS"}]}}}}}}`, other)
	c, fex := rollupClient_internal(raw)
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{rollupSHA})
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	var q string
	for _, call := range fex.Calls() {
		if len(call.Cmd.Argv) > 4 && call.Cmd.Argv[2] == "graphql" {
			q = call.Cmd.Argv[4]
		}
	}
	if m[rollupSHA].State == CheckGreen {
		t.Fatalf("green accepted for %s from an object with oid %s (query selects oid: %v)", rollupSHA, other, strings.Contains(q, " oid "))
	}
}
