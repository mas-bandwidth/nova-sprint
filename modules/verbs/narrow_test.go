package verbs

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// wholeReads is the fake store, counting each read of every card.
type wholeReads struct {
	*store.Fake
	n int
}

func (s *wholeReads) Snapshot(ctx context.Context) (store.Snapshot, error) {
	s.n++
	return s.Fake.Snapshot(ctx)
}

func (s *wholeReads) Read(ctx context.Context, scope store.Scope) (store.Snapshot, error) {
	if scope.All {
		s.n++
	}
	return s.Fake.Read(ctx, scope)
}

// The worker's verbs read only the cards they touch, never every card: a
// far bench pays for every byte (2026-11-09: 36 MB, 20-44 s a read from
// hetzner1, hetzner2 and space; beats timed out and deals expired untaken).
func TestWorkerVerbsReadOnlyTheirCards(t *testing.T) {
	t.Parallel()
	beat := []string{"beat", "--load1", "1", "--cores", "8", "--slots-used", "1", "--slots-total", "8"}
	for _, c := range []struct {
		name  string
		steps [][]string // name, then the command line
	}{
		{"beat", [][]string{append([]string{"alice"}, beat...)}},
		{"take then finish", [][]string{{"alice", "take", "p1.w1@1"}, {"alice", "finish", "p1.w1@1", "--head", head}}},
		{"take then fail", [][]string{{"alice", "take", "p1.w1@1"}, {"alice", "finish", "p1.w1@1", "--failed", "--reason", "gate red"}}},
		{"take then read", [][]string{{"bob", "take", "p2.r1@1"}, {"bob", "read", "p2.r1@1", "yes"}}},
		{"release", [][]string{{"alice", "release", "p1.w1@1", "--reason", "tier"}}},
		{"next", [][]string{{"alice", "next"}, {"alice", "next", "--json"}}},
		{"unhook then hook", [][]string{{"alice", "unhook"}, {"alice", "hook", "--role", "worker"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			st := &wholeReads{Fake: w.f}
			w.e.Store = st
			for _, s := range c.steps {
				if r := w.do(s[0], s[1:]...); r.Exit != 0 {
					t.Fatalf("%v: exit %d\n%s", s, r.Exit, r.Output)
				}
			}
			if st.n != 0 {
				t.Fatalf("%d reads of every card", st.n)
			}
		})
	}
}
