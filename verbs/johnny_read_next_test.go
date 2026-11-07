package verbs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Regression cases by Johnny, adapted from v2-break-johnny e5884e8d.

// TestBreakReadOfFinishedWorkExitsZero: finish a work card, then read that
// same card@gen. read is not a retry of finish. CORRECT-USE: every verb runs
// its prerequisite table before doing anything; a read is the read step, and
// exit 0 means done. LAYER-6 guarantee 7: nothing exits 0 on a failure.
func breakEpoch(t *testing.T) (*store.Fake, time.Time) {
	t.Helper()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	if _, err := f.Clear(context.Background(), store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: at}); err != nil {
		t.Fatal(err)
	}
	recreateWork(t, f, at)
	return f, at
}

func TestBreakReadOfFinishedWorkExitsZero(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "worker", Now: func() time.Time { return at }, Reports: CardReportPlanner{}, Heads: headFake{}}
	if r := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"}); r.Exit != 0 {
		t.Fatal(r)
	}
	head := strings.Repeat("b", 40)
	if r := e.Execute(ctx, []string{"finish", "p.w1@1", "--head", head, "--epoch", "2"}); r.Exit != 0 {
		t.Fatal(r)
	}
	got := e.Execute(ctx, []string{"read", "p.w1@1", "yes", "--as", "worker", "--epoch", "2"})
	if got.Exit == 0 {
		t.Fatalf("read of work card %s exited 0: %s", "p.w1@1", got.Output)
	}
	if !strings.Contains(got.Output, "not a read") && !strings.Contains(got.Output, "REFUSED") {
		t.Fatalf("read of a work card was not refused: %+v", got)
	}
}

// TestBreakReadByAuthorExitsZero: the reader of a head is that head's author.
// CORRECT-USE step 9: read refuses until the reader is not the author, and
// the chain is enforced by the verb's table before it acts, not only at deal.
func TestBreakReadByAuthorExitsZero(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "worker", Now: func() time.Time { return at }, Reports: CardReportPlanner{}, Heads: headFake{}}
	if r := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"}); r.Exit != 0 {
		t.Fatal(r)
	}
	head := strings.Repeat("b", 40)
	if r := e.Execute(ctx, []string{"finish", "p.w1@1", "--head", head, "--as", "worker", "--epoch", "2"}); r.Exit != 0 {
		t.Fatal(r)
	}
	applyTest(t, f, store.Op{ID: "reader-row", Epoch: 2, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "reader", Kind: "worker", Width: 1, Tiers: []string{"pro"}}}})
	snap, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen, why := card.MoveRow(card.RowFromStore(snap.Rows["reader"]), card.Seen{})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "reader-up", Epoch: 2, Actor: "test", Kind: "seen", At: at, Expect: seen.Expect, Writes: seen.Writes})
	snap, _ = f.Snapshot(ctx)
	rows := map[string]card.Row{}
	for name, row := range snap.Rows {
		rows[name] = card.RowFromStore(row)
	}
	p := snap.Cards["p"]
	ask, why := card.Move(card.FromStore(p), consumers(snap, p.ID), rows, card.StreamFromStore(snap.Streams[p.Stream]), nil, card.AskRead{Row: "reader"})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "ask-read", Epoch: 2, Actor: "test", Kind: "ask", At: at, Expect: ask.Expect, Writes: ask.Writes})
	e.Actor = "reader"
	if r := e.Execute(ctx, []string{"take", "p.r1.1@0", "--as", "reader", "--epoch", "2"}); r.Exit != 0 {
		t.Fatal(r)
	}
	applyTest(t, f, store.Op{ID: "author-is-reader", Epoch: 2, Actor: "test", Kind: "author", At: at, Writes: []store.Write{store.SetCardAuthor{Card: "p", Author: "reader"}}})
	got := e.Execute(ctx, []string{"read", "p.r1.1@1", "yes", "--as", "reader", "--epoch", "2"})
	if got.Exit == 0 {
		t.Fatalf("reader authored the head and read exited 0: %s", got.Output)
	}
}

// TestBreakNextDoneStreamHidesEmpty: stream "a" is fully landed and stream
// "b" has no cards. CORRECT-USE question 6 is an empty stream (add) or
// everything landed (done); LAYER-6 section 4 prints the first question that
// is not settled. An empty stream is not settled, so next must name add.
func TestBreakNextDoneStreamHidesEmpty(t *testing.T) {
	snap := store.Snapshot{
		Streams: map[string]store.Stream{"a": {Name: "a"}, "b": {Name: "b"}},
		Cards: map[string]store.Card{
			"pa": {ID: "pa", Stream: "a", Kind: "producer", State: "landed"},
		},
	}
	e := Engine{Store: fakeReader{snap}, Env: fakeEnv{green: true, schema: true, hooked: true}, Actor: "rowan"}
	got := e.Execute(context.Background(), []string{"next"})
	if got.Exit != 0 || !strings.Contains(got.Output, "stream b has no cards") || !strings.Contains(got.Output, "nova-sprint add --stream b") {
		t.Fatalf("empty stream b should be the unsettled next, got %+v", got)
	}
}

// TestBreakNextPartialDoneIsNotEverything: one landed stream beside a stream
// that is still in progress. CORRECT-USE question 6 fires for an empty
// stream or when everything has landed; otherwise question 7 is
// "nothing; wait for the bus". One finished stream is not everything.
func TestBreakNextPartialDoneIsNotEverything(t *testing.T) {
	snap := store.Snapshot{
		Streams: map[string]store.Stream{"a": {Name: "a"}, "b": {Name: "b"}},
		Cards: map[string]store.Card{
			"pa": {ID: "pa", Stream: "a", Kind: "producer", State: "landed"},
			"pb": {ID: "pb", Stream: "b", Kind: "producer", State: "ready"},
		},
	}
	e := Engine{Store: fakeReader{snap}, Env: fakeEnv{green: true, schema: true, hooked: true}, Actor: "rowan"}
	got := e.Execute(context.Background(), []string{"next"})
	if got.Exit != 0 || strings.Contains(got.Output, "is done") || !strings.Contains(got.Output, "nothing needs you") {
		t.Fatalf("partial landing must not answer done, got %+v", got)
	}
}
