package verbs

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// pong logs one op by the row under steps.PongOpID, writing nothing; a
// repeat applies once; a bad nonce is misuse and an unhooked row is refused.
func TestPongLogsTheAnswer(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	before := w.snap(t)
	want(t, w.do("alice", "pong", "--nonce", "a1b2"), 0, "PONG alice nonce=a1b2")
	want(t, w.do("alice", "pong", "--nonce", "a1b2"), 0, "PONG alice nonce=a1b2")
	events, err := w.f.Events(context.Background(), before.Seq, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "pong" || events[0].Actor != "alice" || events[0].Op != steps.PongOpID(before.Epoch, "alice", "a1b2") || !events[0].Accepted {
		t.Fatalf("events %+v", events)
	}
	want(t, w.do("alice", "pong", "--nonce", "a b"), 2, "--nonce takes the nonce")
	want(t, w.do("alice", "pong"), 2, "--nonce takes the nonce")
	want(t, w.do("alice", "unhook"), 0)
	want(t, w.do("alice", "pong", "--nonce", "c3"), 1, "hook in first")
}
