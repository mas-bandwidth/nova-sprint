package forge

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// Both callers of OpenPR (the landing and nova-sprint land) look the PR up
// first and call OpenPR only when there is none. OpenPR looked again, and
// a failure of that second lookup was swallowed (it went on to create).
// OpenPR now creates, then reads back what it created: one lookup.
func TestColdread3OpenPRLooksUpOnce(t *testing.T) {
	t.Parallel()
	fe := edge.NewFakeExec()
	var mu sync.Mutex
	var calls []string
	fe.On("gh", func(call edge.FakeCall) edge.FakeOutcome {
		args := strings.Join(call.Cmd.Argv[1:], " ")
		mu.Lock()
		calls = append(calls, args)
		mu.Unlock()
		if strings.HasPrefix(args, "pr list") {
			return edge.FakeOutcome{Stdout: `[{"number":9,"url":"https://github.com/o/r/pull/9","headRefName":"h","baseRefName":"main","state":"OPEN"}]`}
		}
		return edge.FakeOutcome{}
	})
	c := New(Config{Identity: Identity{Name: "sprint", Token: "tok"}, Exec: fe, Timeout: 30e9, Now: func() int64 { return 1000 }})
	pr, err := c.OpenPR(context.Background(), "o/r", "h", "main", "t", "b")
	if err != nil || pr.Number != 9 {
		t.Fatalf("OpenPR = %+v, %v", pr, err)
	}
	lists, first := 0, ""
	for _, a := range calls {
		if strings.HasPrefix(a, "pr list") {
			lists++
		}
		if first == "" && strings.HasPrefix(a, "pr ") {
			first = a
		}
	}
	if lists != 1 || !strings.HasPrefix(first, "pr create") {
		t.Fatalf("OpenPR must create first and look up once after; gh calls: %q", calls)
	}
}
