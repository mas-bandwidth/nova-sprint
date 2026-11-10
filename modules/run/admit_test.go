package run

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestAdmissionProvesOnBaseMove (item 4, 2026-11-08: cards rotted in
// waiting when their stream's base moved): a stream is re-proved once per
// head, at the cold start and again when its base moves, never every tick;
// the refusals are what the next tick reads, and a proof that answers after
// a newer one began is dropped.
func TestAdmissionProvesOnBaseMove(t *testing.T) {
	t.Parallel()
	type call struct {
		stream string
		done   chan proof
	}
	calls := make(chan call, 8)
	a := &Admission{Admit: func(_ context.Context, _ store.Snapshot, stream string) (map[string]string, error) {
		c := call{stream, make(chan proof)}
		calls <- c
		p := <-c.done
		return p.refused, p.err
	}}
	ctx := context.Background()
	snap := store.Snapshot{}
	a.Prove(ctx, snap, map[string]string{"s1": "h1"})
	c := <-calls
	if c.stream != "s1" {
		t.Fatalf("proved %q", c.stream)
	}
	a.Prove(ctx, snap, map[string]string{"s1": "h1"}) // the same head: no second proof
	c.done <- proof{refused: map[string]string{"p1": "card p1: PATHS a/x: gone: fix"}}
	a.Wait()
	if len(calls) != 0 {
		t.Fatal("the same head was proved twice")
	}
	if got := a.Unadmitted(); got["p1"] == "" || len(got) != 1 {
		t.Fatalf("unadmitted %v", got)
	}

	a.Prove(ctx, snap, map[string]string{"s1": "h2"}) // the base moved
	old := <-calls
	a.Prove(ctx, snap, map[string]string{"s1": "h3"}) // and again, before the first answers
	cur := <-calls
	cur.done <- proof{}
	old.done <- proof{refused: map[string]string{"p1": "stale"}}
	a.Wait()
	if got := a.Unadmitted(); len(got) != 0 {
		t.Fatalf("the h3 proof passed every card, got %v", got)
	}

	var said []string
	a.Fault = func(what string, err error) { said = append(said, what+": "+err.Error()) }
	a.Prove(ctx, snap, map[string]string{"s1": "h4"})
	c = <-calls
	c.done <- proof{err: errors.New("nova-config unreadable")}
	a.Wait()
	if len(said) != 1 || said[0] != "admission stream s1: nova-config unreadable" {
		t.Fatalf("said %q", said)
	}
	if got := a.Unadmitted(); len(got) != 0 {
		t.Fatalf("a failed proof keeps the last answer, got %v", got)
	}
}

type proof struct {
	refused map[string]string
	err     error
}

// TestAdmissionStartsOverAtAClear: a clear's new epoch may reuse the old
// card ids at the same head, so a new epoch proves its streams again and
// the old epoch's refusals are dropped, those of a proof of the old epoch
// that answers late too.
func TestAdmissionStartsOverAtAClear(t *testing.T) {
	t.Parallel()
	late := make(chan struct{})
	a := &Admission{Admit: func(_ context.Context, snap store.Snapshot, _ string) (map[string]string, error) {
		switch snap.Epoch {
		case 2:
			return nil, nil
		case 3:
			<-late
		}
		return map[string]string{"p1": fmt.Sprintf("epoch %d's refusal", snap.Epoch)}, nil
	}}
	ctx, heads := context.Background(), map[string]string{"s1": "h1"}
	a.Prove(ctx, store.Snapshot{Epoch: 1}, heads)
	a.Wait()
	if got := a.Unadmitted(); got["p1"] != "epoch 1's refusal" {
		t.Fatalf("epoch 1: unadmitted %v", got)
	}
	a.Prove(ctx, store.Snapshot{Epoch: 2}, heads) // cleared: the same head, the same card id
	a.Wait()
	if got := a.Unadmitted(); len(got) != 0 {
		t.Fatalf("epoch 2 kept epoch 1's refusals: %v", got)
	}
	a.Prove(ctx, store.Snapshot{Epoch: 3}, heads) // its proof answers after epoch 4's
	a.Prove(ctx, store.Snapshot{Epoch: 4}, heads)
	for a.Unadmitted()["p1"] != "epoch 4's refusal" {
		runtime.Gosched()
	}
	close(late)
	a.Wait()
	if got := a.Unadmitted(); got["p1"] != "epoch 4's refusal" {
		t.Fatalf("epoch 3's late proof replaced epoch 4's: %v", got)
	}
}

// TestTickDealsNoUnadmittedCard: the tick passes the latest proof's
// refusals to the steps: the refused producer is not dealt, and its stream's
// admission judgement opens for the coordinator.
func TestTickDealsNoUnadmittedCard(t *testing.T) {
	t.Parallel()
	f, ro := seeded(t)
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		o := Observed{Now: t0.Add(time.Duration(i+1) * time.Second), Present: []string{"alice"}, Roster: ro,
			Heads: map[string]string{"s": strings.Repeat("a", 40)}, Unadmitted: map[string]string{"p1": "card p1: PATHS a: gone: fix the path"}}
		if _, err := tk.Tick(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	snap := tk.Snapshot()
	if c := snap.Cards["p1"]; c.State != "ready" {
		t.Fatalf("p1 is %s, want ready and undealt", c.State)
	}
	j, ok := snap.Judgements["admission.s.1"]
	if !ok || j.State != "open" || !strings.Contains(j.Question, "fix the path") {
		t.Fatalf("judgements %+v", snap.Judgements)
	}
}
