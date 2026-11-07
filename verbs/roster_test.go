package verbs

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type appendBeforeTake struct {
	*store.Fake
	at       time.Time
	appended bool
}

func (s *appendBeforeTake) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "take" && !s.appended {
		s.appended = true
		_, err := s.Fake.Apply(ctx, store.Op{ID: "unrelated", Epoch: op.Epoch, Actor: "test", Kind: "seed", At: s.at, Writes: []store.Write{store.PutStream{Name: "other", Repo: "owner/other", Base: "main", Pos: 2}}})
		if err != nil {
			return store.Receipt{}, err
		}
	}
	return s.Fake.Apply(ctx, op)
}
func TestTakeSurvivesUnrelatedAppendAndReplays(t *testing.T) {
	ctx := context.Background()
	at := time.Unix(1000, 0)
	f, _ := breakEpoch(t)
	st := &appendBeforeTake{Fake: f, at: at}
	e := Engine{Store: st, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: fixtureRoster{}, Actor: "worker", Now: func() time.Time { return at }}
	first := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"})
	if first.Exit != 0 || !st.appended {
		t.Fatal(first)
	}
	again := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"})
	if again != first {
		t.Fatal(first, again)
	}
}

type emptyRoster struct{}

func (emptyRoster) ReadRoster(context.Context) (roster.Roster, error) { return roster.Roster{}, nil }
func TestTakeRequiresRosterMembership(t *testing.T) {
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: emptyRoster{}, Actor: "worker", Now: func() time.Time { return at }}
	got := e.Execute(context.Background(), []string{"take", "p.w1@0", "--epoch", "2"})
	if got.Exit != 1 {
		t.Fatal(got)
	}
	snap, _ := f.Snapshot(context.Background())
	if snap.Cards["p.w1"].State != "ready" {
		t.Fatal(snap.Cards["p.w1"])
	}
}
