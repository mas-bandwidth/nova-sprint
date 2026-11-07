package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// A row with two ready cards is nagged at t+1 and t+2 (the DEAL went at
// t), once each, with Glenn's words and both take lines; after a take
// leaves nothing ready, never again.
func TestReadyNagUntilTaken(t *testing.T) {
	s := store.Snapshot{Epoch: 3, Rows: map[string]store.Row{"alice": {Name: "alice", Session: "s1"}}, Cards: map[string]store.Card{
		"p.w1": {ID: "p.w1", Kind: "work", Stream: "s", State: "ready", Holder: "alice", Tier: "pro", Attempt: 1, Gen: 1},
		"q.w1": {ID: "q.w1", Kind: "work", Stream: "s", State: "ready", Holder: "alice", Tier: "pro", Attempt: 1, Gen: 2},
		"r.w1": {ID: "r.w1", Kind: "work", Stream: "s", State: "working", Holder: "alice", Tier: "pro", Attempt: 1, Gen: 1},
	}}
	ro := roster.Roster{Kin: map[string]roster.Kin{"alice": {Name: "alice", Width: 4}}}
	b, n := &fakeBus{}, &Nagger{}
	for _, at := range []int{0, 1, 2} {
		n.Nag(context.Background(), b, s, ro, time.Second, t0.Add(time.Duration(at)*time.Second))
	}
	if len(b.sent) != 2 {
		t.Fatalf("sent %d nags, want 2 (t+1, t+2): %+v", len(b.sent), b.sent)
	}
	want := "you have a new ready card. you have 2 ready cards, you are currently working on 1, and your width is 4. Please work to width!"
	for _, m := range b.sent {
		lines := strings.Split(m.body, "\n")
		if m.to != "alice" || len(lines) != 3 || lines[0] != want ||
			!strings.HasSuffix(lines[1], "take p.w1@1 --epoch 3 --as alice") || !strings.HasSuffix(lines[2], "take q.w1@2 --epoch 3 --as alice") {
			t.Fatalf("nag = %+v", m)
		}
	}
	for _, id := range []string{"p.w1", "q.w1"} {
		c := s.Cards[id]
		c.State = "working"
		s.Cards[id] = c
	}
	n.Nag(context.Background(), b, s, ro, time.Second, t0.Add(3*time.Second))
	n.Nag(context.Background(), b, s, ro, time.Second, t0.Add(4*time.Second))
	if len(b.sent) != 2 {
		t.Fatalf("nagged after the take: %+v", b.sent[2:])
	}
}
