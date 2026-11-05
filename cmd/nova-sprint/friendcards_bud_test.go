package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/bus/bustest"
)

// staleRoster is the bus store as the fleet had it on 2026-10-05: a Redis
// apart from the sprint store, whose `friends` set was filled once by hand and
// never by nova-config's apply, so a bud's row is no name there until
// someone tells it (bus.Enroller, which the fake alone is not).
type staleRoster struct {
	*bustest.Fake
	enrolled []string
}

func (s *staleRoster) Roster(ctx context.Context) ([]string, time.Time, error) {
	names, now, err := s.Fake.Roster(ctx)
	return slices.Concat(names, s.enrolled), now, err
}

func (s *staleRoster) Members(ctx context.Context) ([]string, []string, time.Time, error) {
	friends, machines, now, err := s.Fake.Members(ctx)
	return slices.Concat(friends, s.enrolled), machines, now, err
}

func (s *staleRoster) Enroll(_ context.Context, friends ...string) error {
	for _, f := range friends {
		if !slices.Contains(s.enrolled, f) {
			s.enrolled = append(s.enrolled, f)
		}
	}
	return nil
}

// Each friend sync pass puts every friend row's name on the bus store's roster,
// cards or none (Aurora, 2026-10-12: a new row with nothing dealt her was no
// known bus name, and nova-bus send --as aurora was refused). Witness: with no
// bus store named the pass tells the bus nothing; with one, one pass adds every
// row and a second adds none.
func TestFriendSyncEnrollsEveryFriendRowOnTheBusStore(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy", "bob", "cyd")
	store := &staleRoster{Fake: bustest.NewFake(time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC))}
	ta.a.busOpen = func(context.Context, string, string) (*bus.Bus, func(), error) {
		return &bus.Bus{Store: store}, func() {}, nil
	}
	out := ta.ok("friend sync")
	assert.NotContains(t, out, "BUS-NAMES", "no bus store named: nothing is enrolled")
	assert.Empty(t, store.enrolled)

	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		return map[string]string{busRedisEnv: "127.0.0.1:6381"}[k] + env(k)
	}
	out = ta.ok("friend sync")
	assert.Contains(t, out, "FRIEND-SYNC BUS-NAMES added=amy,bob,cyd: the bus store now knows the friend rows")
	assert.ElementsMatch(t, []string{"amy", "bob", "cyd"}, store.enrolled, "every row is a known bus name after one pass")
	out = ta.ok("friend sync")
	assert.NotContains(t, out, "BUS-NAMES", "a second pass adds none")
	assert.Len(t, store.enrolled, 3)
	ta.clean()
}
