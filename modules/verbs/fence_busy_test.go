package verbs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// busyStore is a store a running sprint keeps moving: before each of the
// first n ops of kind reaches it, one write by another actor lands (the
// i-th is write(i)), as the run's ticks, a bench's report or a worker's
// hook land between a verb's read and its commit.
type busyStore struct {
	*store.Fake
	kind   string
	n      int
	write  func(i int) store.Write
	landed int
}

func (b *busyStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == b.kind && b.landed < b.n {
		b.landed++
		other := store.Op{ID: fmt.Sprintf("busy-%s-%d", b.kind, b.landed), Epoch: 1, Actor: "alice", Kind: "busy", At: t0, Writes: []store.Write{b.write(b.landed)}}
		if r, err := b.Fake.Apply(ctx, other); err != nil || !r.Accepted {
			return store.Receipt{}, fmt.Errorf("the write between, %d, did not land: %s %v", b.landed, r.Reason, err)
		}
	}
	return b.Fake.Apply(ctx, op)
}

func busy(w *world, kind string, n int, write func(int) store.Write) *busyStore {
	return &busyStore{Fake: w.f, kind: kind, n: n, write: write}
}

// zedHooks is another row's hook: a worker on a bench hooking in, then
// unhooking, then hooking in again.
func zedHooks(i int) store.Write {
	if i%2 == 0 {
		return store.PutRow{Name: "zed"}
	}
	return store.PutRow{Name: "zed", Session: fmt.Sprintf("s-z%d", i), Role: "worker"}
}

// otherCards is a card of another stream, its priority flipped each time:
// a write to a record the verb names nowhere.
func otherCards(i int) store.Write {
	return store.SetCardPriority{Card: "q1", Priority: map[bool]string{true: "high", false: "normal"}[i%2 == 1]}
}

// withOther adds stream t and its producer q1.
func withOther(s *store.Snapshot) {
	s.Streams["t"] = store.Stream{Name: "t", Repo: "o/r2", Base: "main", Mode: "auto", Pos: 1}
	s.Cards["q1"] = store.Card{ID: "q1", Kind: "producer", Stream: "t", State: "ready", Gen: 1, Attempt: 1, Tier: "pro", Priority: "normal"}
}

// TestHoldStreamCommitsOverAnUnrelatedWrite is the live refusal of
// 2026-11-10: `hold schema-main` refused "the store changed since seq
// 95581: seq 95583 wrote cards", a bench's report on a card of another
// stream. A stream's hold is fenced on every card (a producer of the
// stream made ready after the read would be left unparked); a write to a
// card it does not park is not its own, so it reads again and commits.
func TestHoldStreamCommitsOverAnUnrelatedWrite(t *testing.T) {
	t.Parallel()
	s := sprint()
	withOther(&s)
	w := worldFrom(t, s)
	b := busy(w, "holdstream", 1, otherCards)
	w.e.Store = b
	want(t, w.do("carol", "hold", "s", "--reason", "base red"), 0, "stream s held=true")
	if b.landed != 1 {
		t.Fatalf("%d writes landed between", b.landed)
	}
}

// TestHoldStreamRefusesItsOwnChange: the stream held by another between
// the read and the commit is the hold's own record: refused, naming it.
func TestHoldStreamRefusesItsOwnChange(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.e.Store = busy(w, "holdstream", 1, func(int) store.Write { return store.SetStreamHeld{Stream: "s", Held: true} })
	want(t, w.do("carol", "hold", "s", "--reason", "base red"), 1, "stream s is held, expected not held", "stream s changed since this verb read it (held=false, now held=true)")
}

// TestCoordinatorVerbsCommitOverAnotherRowsHook: stream add, stream set,
// identity, stop and stats reset are fenced on the hooks, so that the
// caller is still the coordinator it was read as; another row hooking in
// between (a bench worker) is not the caller's hook: each reads again and
// commits.
func TestCoordinatorVerbsCommitOverAnotherRowsHook(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		kind string
		args []string
		ok   string
	}{
		{"stream-add", []string{"stream", "add", "u", "--repo", "o/r", "--forge", "github"}, "STREAM u repo=o/r"},
		{"stream-set", []string{"stream", "set", "s", "--gate", "make check"}, "STREAM s mode=auto"},
		{"identity", []string{"identity", "--name", "New Bot", "--email", "3+new-bot@users.noreply.example.test", "--reason", "r"}, "IDENTITY New Bot"},
		{"stop", []string{"stop", "--reason", "deploy"}, "STOPPED applied at seq"},
		{"stats-reset", []string{"stats", "reset", "--reason", "r"}, "STATS RESET applied at seq"},
	} {
		t.Run(c.kind, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			b := busy(w, c.kind, 1, zedHooks)
			w.e.Store = b
			want(t, w.do("carol", c.args...), 0, c.ok)
			if b.landed != 1 {
				t.Fatalf("%d writes landed between", b.landed)
			}
		})
	}
}

// TestStreamVerbsCommitOverAnotherStream: another stream added between is
// not stream set's own; stream add reads the positions again, so the new
// stream lands after it.
func TestStreamVerbsCommitOverAnotherStream(t *testing.T) {
	t.Parallel()
	other := func(int) store.Write { return store.PutStream{Name: "v", Repo: "o/r", Base: "main", Pos: 9} }
	t.Run("stream add", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		w.e.Store = busy(w, "stream-add", 1, other)
		want(t, w.do("carol", "stream", "add", "u", "--repo", "o/r", "--forge", "github"), 0, "STREAM u repo=o/r")
		if got := w.snap(t).Streams["u"].Pos; got != 10 {
			t.Fatalf("stream u at pos %d, want 10 (after v)", got)
		}
	})
	t.Run("stream set", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		w.e.Store = busy(w, "stream-set", 1, other)
		want(t, w.do("carol", "stream", "set", "s", "--gate", "make check"), 0, "STREAM s mode=auto")
	})
}

// TestFencedVerbsAreBounded: a verb applies its op fenceTries times at
// most. A write it did not read landing before each of the first
// fenceTries-1 applies, it commits on the last; landing before every one,
// it stops, saying how many times and why, and nothing it would write is
// written.
func TestFencedVerbsAreBounded(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, kind string
		world      func(*testing.T) *world
		write      func(int) store.Write
		args       []string
		ok         string
		applied    func(store.Snapshot) bool
	}{
		{"stop", "stop", newWorld, zedHooks, []string{"stop", "--reason", "deploy"}, "STOPPED applied at seq",
			func(s store.Snapshot) bool { return s.Stopped }},
		{"hold of a stream", "holdstream", func(t *testing.T) *world { s := sprint(); withOther(&s); return worldFrom(t, s) }, otherCards,
			[]string{"hold", "s", "--reason", "base red"}, "stream s held=true",
			func(s store.Snapshot) bool { return s.Streams["s"].Held }},
	} {
		t.Run(c.name+": commits on the last try", func(t *testing.T) {
			t.Parallel()
			w := c.world(t)
			w.e.Store = busy(w, c.kind, fenceTries-1, c.write)
			want(t, w.do("carol", c.args...), 0, c.ok)
		})
		t.Run(c.name+": stops after the last", func(t *testing.T) {
			t.Parallel()
			w := c.world(t)
			b := busy(w, c.kind, fenceTries, c.write)
			w.e.Store = b
			want(t, w.do("carol", c.args...), 1, "the store changed since seq", fmt.Sprintf("refused %d times", fenceTries), "the store moves faster than it can plan")
			if b.landed != fenceTries {
				t.Fatalf("%d tries, want %d", b.landed, fenceTries)
			}
			if c.applied(w.snap(t)) {
				t.Fatal("the verb's write landed")
			}
		})
	}
}

// TestPolicyActivateCommitsOverAJudgement: activate answers the policy
// judgements it read open, so it is fenced on the judgements; the run
// opening another judgement between (a red, a down) is not one it answers:
// it reads again and commits.
func TestPolicyActivateCommitsOverAJudgement(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	withPolicy(t, w)
	file := filepath.Join(t.TempDir(), "mine.star")
	if err := os.WriteFile(file, []byte("# mine\n"+policy.DefaultBody()), 0o600); err != nil {
		t.Fatal(err)
	}
	want(t, w.do("carol", "policy", "set", file, "--reason", "try"), 0, "POLICY STORED v2")
	b := busy(w, "policy-activate", 1, func(i int) store.Write {
		return store.PutJudgement{ID: fmt.Sprintf("red-%d", i), Kind: "red", About: "p1"}
	})
	w.e.Store = b
	want(t, w.do("carol", "policy", "activate", "2", "--reason", "go"), 0, "POLICY ACTIVE v2")
	if b.landed != 1 {
		t.Fatalf("%d writes landed between", b.landed)
	}
}

// landsFirst is a store where, before the first op of kind, each of writes
// lands as its own op by another actor, in order.
type landsFirst struct {
	*store.Fake
	kind   string
	writes []store.Write
	tries  int
}

func (l *landsFirst) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == l.kind {
		l.tries++
		for i, wr := range l.writes {
			other := store.Op{ID: fmt.Sprintf("first-%s-%d", l.kind, i), Epoch: 1, Actor: "alice", Kind: "busy", At: t0, Writes: []store.Write{wr}}
			if r, err := l.Fake.Apply(ctx, other); err != nil || !r.Accepted {
				return store.Receipt{}, fmt.Errorf("the write between, %d, did not land: %s %v", i, r.Reason, err)
			}
		}
		l.writes = nil
	}
	return l.Fake.Apply(ctx, op)
}

// TestPolicyActivateRefusesItsOwnChange is the cold read's probe of
// 2026-11-10: v2 and v3 stored, `policy activate 2` sent, and between its
// read and its commit the run opens a judgement, then another session
// activates v3. The refusal names the judgement (the first write between),
// but the active policy is activate's own record: it is refused, naming
// the change, and v3 stays active. So is a version stored between, and the
// caller's own hook changing (it may no longer be the coordinator).
func TestPolicyActivateRefusesItsOwnChange(t *testing.T) {
	t.Parallel()
	red := store.PutJudgement{ID: "red-1", Kind: "red", About: "p1"}
	for _, c := range []struct {
		name   string
		then   store.Write
		why    string
		active int
	}{
		{"another activation", store.SetActivePolicy{Version: 3}, "the active policy changed since this verb read it (v1, now v3)", 3},
		{"another version stored", store.PutPolicy{Kind: "script", Name: "other.star", Body: "# other\n" + policy.DefaultBody()}, "policy v4 was stored since this verb read it", 1},
		{"the caller's hook", store.PutRow{Name: "carol"}, "row carol's hook changed since this verb read it", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			withPolicy(t, w)
			for i, body := range []string{"# mine\n", "# mine too\n"} {
				file := filepath.Join(t.TempDir(), fmt.Sprintf("mine%d.star", i))
				if err := os.WriteFile(file, []byte(body+policy.DefaultBody()), 0o600); err != nil {
					t.Fatal(err)
				}
				want(t, w.do("carol", "policy", "set", file, "--reason", "try"), 0, fmt.Sprintf("POLICY STORED v%d", i+2))
			}
			l := &landsFirst{Fake: w.f, kind: "policy-activate", writes: []store.Write{red, c.then}}
			w.e.Store = l
			want(t, w.do("carol", "policy", "activate", "2", "--reason", "go"), 1, "wrote judgements", c.why)
			if got := w.snap(t).Policy; got != c.active {
				t.Fatalf("active v%d, want v%d", got, c.active)
			}
			if l.tries != 1 {
				t.Fatalf("%d tries, want 1", l.tries)
			}
		})
	}
}

// TestFencedChecksItsOwnOnTheLastRefusal: the fifth refusal is checked
// against the verb's own record before it is called five writes it did not
// read: the stream held by another before the fifth apply is the hold's
// own, named.
func TestFencedChecksItsOwnOnTheLastRefusal(t *testing.T) {
	t.Parallel()
	s := sprint()
	withOther(&s)
	w := worldFrom(t, s)
	w.e.Store = busy(w, "holdstream", fenceTries, func(i int) store.Write {
		if i == fenceTries {
			return store.SetStreamHeld{Stream: "s", Held: true}
		}
		return otherCards(i)
	})
	r := w.do("carol", "hold", "s", "--reason", "base red")
	want(t, r, 1, "stream s changed since this verb read it (held=false, now held=true)")
	if strings.Contains(r.Output, "refused 5 times") {
		t.Fatalf("called five writes it did not read:\n%s", r.Output)
	}
}

// refusedOp is a store whose ops of kind carry an expect that never holds
// (card p1 at gen 99): refused on the verb's own read, nothing written
// between.
type refusedOp struct {
	*store.Fake
	kind  string
	tries int
}

func (s *refusedOp) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == s.kind {
		s.tries++
		op.Expect = append(op.Expect, store.CardGen{Card: "p1", Gen: 99})
	}
	return s.Fake.Apply(ctx, op)
}

// TestFencedRetriesOnlyAWriteBetween: a refusal with no write between the
// verb's read and its commit is not a fence another writer tripped (the
// refused op's own event is the only one since the read): it is the verb's
// refusal at once, never read again and never called five writes.
func TestFencedRetriesOnlyAWriteBetween(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := &refusedOp{Fake: w.f, kind: "holdstream"}
	w.e.Store = s
	r := w.do("carol", "hold", "s", "--reason", "base red")
	want(t, r, 1, "card p1 is at gen 1, expected gen 99")
	if s.tries != 1 || strings.Contains(r.Output, "refused 5 times") {
		t.Fatalf("%d tries:\n%s", s.tries, r.Output)
	}
}
