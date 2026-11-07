package forge

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// Cold read, 2026-11-06, at 649d1b4c. One failing probe per confirmed defect.

const crSHA = "0123456789abcdef0123456789abcdef01234567"

func crClient_internal(fe *edge.FakeExec) *Client {
	fe.On("gh", func(call edge.FakeCall) edge.FakeOutcome {
		args := strings.Join(call.Cmd.Argv[1:], " ")
		switch {
		case strings.HasPrefix(args, "auth status"):
			return edge.FakeOutcome{Stdout: "Logged in to github.com account rowan-claude (keyring)"}
		case strings.HasPrefix(args, "pr view"):
			return edge.FakeOutcome{Stdout: `{"number":7,"headRefOid":"` + crSHA + `","baseRefName":"v2","state":"OPEN"}`}
		case strings.HasPrefix(args, "api graphql"):
			return edge.FakeOutcome{Stdout: `{"data":{"repository":{"c0":{"statusCheckRollup":{"state":"SUCCESS","contexts":{"nodes":[{"__typename":"CheckRun","name":"rate limit regression","status":"COMPLETED","conclusion":"SUCCESS"}]}}}}}}`}
		}
		return edge.FakeOutcome{}
	})
	n := int64(1000)
	return New(Config{Identity: Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok"}, Exec: fe, Timeout: 30e9, Now: func() int64 { return n }})
}

// Ruling: landing is fast-forward only and base gets the exact sha CI
// passed. Merge runs `gh pr merge --merge` (forge.go:465), which writes a new
// merge commit on base: base advances to a sha no check ran at.
func TestColdreadDefectForgeMergeIsNotFastForward(t *testing.T) {
	t.Parallel()
	fe := edge.NewFakeExec()
	c := crClient_internal(fe)
	if err := c.Merge(context.Background(), "mas-bandwidth/nova-sprint", 7, crSHA, "v2"); err != nil {
		t.Fatal(err)
	}
	for _, call := range fe.Calls() {
		if slices.Contains(call.Cmd.Argv, "merge") && slices.Contains(call.Cmd.Argv, "--merge") {
			t.Errorf("forge merge argv %q creates a GitHub merge commit; base tip will not be %s", strings.Join(call.Cmd.Argv, " "), crSHA)
		}
	}
}

// Design: "A 403 or 429 with a retry time rests that identity". The detector
// scans stdout as well as stderr for "rate limit" (forge.go:227-228), so a
// green check run whose name contains the words rests the identity and turns
// every read of that repository's CI into a fault.
func TestColdreadDefectForgeRateLimitMatchedInStdout(t *testing.T) {
	t.Parallel()
	c := crClient_internal(edge.NewFakeExec())
	m, err := c.Checks(context.Background(), "mas-bandwidth/nova-sprint", []string{crSHA})
	var fe *FaultError
	if errors.As(err, &fe) {
		t.Errorf("a green check named %q was read as a rate limit: %v (identity rests until %d)", "rate limit regression", err, c.RestedUntil())
		return
	}
	if err != nil || m[crSHA].State != CheckGreen {
		t.Errorf("want green, got %+v err=%v", m[crSHA], err)
	}
}
