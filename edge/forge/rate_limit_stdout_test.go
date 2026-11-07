package forge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

const rateSHA = "0123456789abcdef0123456789abcdef01234567"

func rateClient_internal(fe *edge.FakeExec) *Client {
	fe.On("gh", func(call edge.FakeCall) edge.FakeOutcome {
		args := strings.Join(call.Cmd.Argv[1:], " ")
		switch {
		case strings.HasPrefix(args, "auth status"):
			return edge.FakeOutcome{Stdout: "Logged in to github.com account ci-bot (keyring)"}
		case strings.HasPrefix(args, "pr view"):
			return edge.FakeOutcome{Stdout: `{"number":7,"headRefOid":"` + rateSHA + `","baseRefName":"v2","state":"OPEN"}`}
		case strings.HasPrefix(args, "api graphql"):
			return edge.FakeOutcome{Stdout: `{"data":{"repository":{"c0":{"statusCheckRollup":{"state":"SUCCESS","contexts":{"nodes":[{"__typename":"CheckRun","name":"rate limit regression","status":"COMPLETED","conclusion":"SUCCESS"}]}}}}}}`}
		}
		return edge.FakeOutcome{}
	})
	n := int64(1000)
	return New(Config{Identity: Identity{Name: "sprint", Token: "tok"}, Exec: fe, Timeout: 30e9, Now: func() int64 { return n }})
}

// A 403 or 429 with a retry time rests that identity. The detector scans
// stdout as well as stderr for "rate limit", so a green check run whose name
// contains the words must not rest the identity or turn every read of that
// repository's CI into a fault.
func TestForgeRateLimitMatchedInStdout(t *testing.T) {
	t.Parallel()
	c := rateClient_internal(edge.NewFakeExec())
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{rateSHA})
	var fe *FaultError
	if errors.As(err, &fe) {
		t.Errorf("a green check named %q was read as a rate limit: %v (identity rests until %d)", "rate limit regression", err, c.RestedUntil())
		return
	}
	if err != nil || m[rateSHA].State != CheckGreen {
		t.Errorf("want green, got %+v err=%v", m[rateSHA], err)
	}
}
