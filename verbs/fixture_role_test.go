package verbs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type fixtureRoster struct{}

func (fixtureRoster) ReadRoster(context.Context) (roster.Roster, error) {
	r := roster.Roster{Workers: map[string]roster.Worker{}, Kin: map[string]roster.Kin{}}
	for _, name := range []string{"worker", "reader", "rowan", "alice", "other", "w", "r", "writer"} {
		r.Kin[name] = roster.Kin{Name: name, Width: 2, Tiers: []string{"pro", "flash"}, Model: "sol", Harness: "codex"}
	}
	return r, nil
}
func hookFixture(t *testing.T, f *store.Fake, actor, role string) {
	t.Helper()
	ctx := context.Background()
	snap, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row, exists := snap.Rows[actor]
	if exists && row.Session != "" && row.Role == role {
		return
	}
	session := "fixture-" + actor
	if exists && row.Session != "" {
		session = row.Session
	}
	applyTest(t, f, store.Op{ID: fmt.Sprintf("test-hook:%s:%d", actor, snap.Seq), Epoch: snap.Epoch, Actor: "test", Kind: "hook", At: time.Unix(1000, 0), Writes: []store.Write{store.PutRow{Name: actor, Session: session, Role: role}}})
}
func wireFixture(t *testing.T, e *Engine, f *store.Fake, actors ...string) {
	t.Helper()
	for _, actor := range actors {
		hookFixture(t, f, actor, "coordinator")
	}
	e.Roles = SnapshotRoles{Store: f}
	e.Roster = fixtureRoster{}
}
