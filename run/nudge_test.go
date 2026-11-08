package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestDealNoticeTellsTheHand: the deal's push names the card, the repo,
// base and branch, the brief, take (the acknowledgement) and the finish
// command (defect 52: the deal is the take).
func TestDealNoticeTellsTheHand(t *testing.T) {
	t.Parallel()
	e := store.Event{Accepted: true, Epoch: 3, Before: &store.State{Cards: map[string]store.Card{}}, After: &store.State{Cards: map[string]store.Card{
		"p.w1": {ID: "p.w1", Kind: "work", Of: "p", Stream: "s", State: "working", Holder: "alice", Tier: "pro", Attempt: 1, Gen: 1,
			Branch: "sprint/s/p", BaseSHA: strings.Repeat("b", 40), Brief: "do p"},
	}}}
	after := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s", Repo: "o/r"}}}
	got, err := EventNotices{Coordinator: "carol"}.ForEvent(e, after, "alice")
	if err != nil || len(got) != 1 || got[0].Subject != "DEAL" {
		t.Fatalf("notices %+v, %v", got, err)
	}
	for _, w := range []string{"card=p.w1", "repo=o/r", "branch=sprint/s/p", "base=" + strings.Repeat("b", 40), "brief=do p",
		"in your hand now", "nova-sprint take p.w1@1 --epoch 3 --as alice", "nova-sprint finish p.w1@1 --head <sha> --as alice --epoch 3"} {
		if !strings.Contains(got[0].Line, w) {
			t.Errorf("the deal's line lacks %q:\n%s", w, got[0].Line)
		}
	}
}

// TestPusherNudgesOutsideTheLog: a tick's nudges ride in that tick's
// envelope, move no cursor and are not resent by the pusher; the next
// tick's nudges replace them.
func TestPusherNudgesOutsideTheLog(t *testing.T) {
	t.Parallel()
	f := started(t)
	p := NewPusher()
	p.Nudge([]steps.Nudge{{Row: "alice", Card: "p.w1", Line: "NUDGE card=p.w1 dealt 10s ago"}})
	b := &fakeBus{}
	if failed := p.Push(context.Background(), b, f, 1, t0); len(failed) != 0 {
		t.Fatalf("failed %v", failed)
	}
	if len(b.sent) != 1 || b.sent[0].to != "alice" || b.sent[0].subject != "NUDGE" || !strings.Contains(b.sent[0].body, "card=p.w1") {
		t.Fatalf("sent %+v", b.sent)
	}
	snap, err := f.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, moved := snap.Cursors["push:alice"]; moved {
		t.Fatalf("a nudge moved the push cursor: %v", snap.Cursors)
	}
	b.sent = nil
	p.Push(context.Background(), b, f, 1, t0.Add(time.Second))
	if len(b.sent) != 0 {
		t.Fatalf("a nudge was sent twice: %+v", b.sent)
	}
	b.fail = map[string]bool{"alice": true}
	p.Nudge([]steps.Nudge{{Row: "alice", Card: "p.w1", Line: "NUDGE card=p.w1 dealt 11s ago"}})
	if failed := p.Push(context.Background(), b, f, 1, t0); len(failed) != 1 {
		t.Fatalf("a failed nudge is a push failure: %v", failed)
	}
	b.fail = nil
	p.Push(context.Background(), b, f, 1, t0)
	if len(b.sent) != 0 {
		t.Fatalf("a failed nudge is derived again by the next tick, not resent: %+v", b.sent)
	}
}

// TestTickNudgesAnUnacknowledgedDeal: the run loop pushes the steps' nudge
// for a card dealt and not acknowledged past timeouts ready_nag (clock passed in).
func TestTickNudgesAnUnacknowledgedDeal(t *testing.T) {
	t.Parallel()
	f, ro := seeded(t)
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 3)
	b := &fakeBus{}
	tk.Push(context.Background(), b, t0)
	now := t0.Add(time.Duration(4+testReadyNag) * time.Second)
	if _, err := tk.Tick(context.Background(), Observed{Now: now, Present: []string{"alice"}, Roster: ro, Heads: map[string]string{"s": strings.Repeat("a", 40)}}); err != nil {
		t.Fatal(err)
	}
	b.sent = nil
	tk.Push(context.Background(), b, now)
	if len(b.sent) != 1 || b.sent[0].to != "alice" || !strings.Contains(b.sent[0].body, "NUDGE you have a new ready card.") || !strings.Contains(b.sent[0].body, "card=p1.w1") || !strings.Contains(b.sent[0].body, "nova-sprint take p1.w1@") {
		t.Fatalf("sent %+v", b.sent)
	}
}
