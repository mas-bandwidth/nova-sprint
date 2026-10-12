package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// The three nova-table commands internal/sprint/TABLES.lock (the Change of
// 2026-10-06) names, in the order the lock writes them, to take a v1.2.9 fleet
// table to the build's locked shape.
var lockedFleetCommands = []string{
	"nova-table col add fleet provider --after okpct",
	"nova-table col add fleet coordination --after provider",
	"nova-table col add fleet environment --after coordination",
	"nova-table col add fleet harness --after environment",
	"nova-table col add fleet no_result --after harness",
	"nova-table col add fleet redealt --after no_result",
	"nova-table col add fleet finished --after withdrawn",
	"nova-table set fleet --hide finished",
	"nova-table set fleet --hide provider",
	"nova-table set fleet --hide coordination",
	"nova-table set fleet --hide environment",
	"nova-table set fleet --hide harness",
	"nova-table set fleet --hide no_result",
}

// oldFleet makes the twin's fleet table the v1.2.9 shape: fleetBeforeVerdicts
// (migration_test.go) and its hidden list without finished, as
// TestATickOnAStoreWithTheLockedFleetTableWritesNoColumnItLacks sets ft.def.
func oldFleet(t *testing.T, m *Mem, name string) {
	t.Helper()
	cols, err := ntable.ParseColumns(fleetBeforeVerdicts)
	require.NoError(t, err)
	m.mu.Lock()
	defer m.mu.Unlock()
	ft := m.tables[name]
	require.NotNil(t, ft)
	ft.def.Columns = cols
	ft.def.Hidden = []string{sprint.Withdrawn, sprint.DoneOK, sprint.DoneFailed, sprint.DoneDefect, sprint.Ctl}
}

// TestLockedShapeRefusesTheFleetTableBeforeTheVerdictsMigration: a store whose
// fleet table is the v1.2.9 shape is refused, and the refusal carries each of
// the TABLES.lock commands exactly; the locked shape, one extra column
// beside it, and a store with no tables yet are accepted.
func TestLockedShapeRefusesTheFleetTableBeforeTheVerdictsMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	names := sprint.Names{}
	fleet := names.Table(sprint.Fleet)

	t.Run("the v1.2.9 fleet table is refused with the lock's commands", func(t *testing.T) {
		m := NewMem()
		st := &Store{B: m, Names: names, Actor: "coordinator"}
		require.NoError(t, st.Init(ctx))
		oldFleet(t, m, fleet)
		err := st.LockedShape(ctx)
		require.Error(t, err)
		for _, cmd := range lockedFleetCommands {
			assert.Contains(t, err.Error(), cmd)
		}
		assert.Contains(t, err.Error(), "(internal/sprint/TABLES.lock)")
		assert.Contains(t, err.Error(), "on a STOPPED machine, before this build is installed")
	})

	t.Run("the locked fleet table passes", func(t *testing.T) {
		m := NewMem()
		st := &Store{B: m, Names: names, Actor: "coordinator"}
		require.NoError(t, st.Init(ctx))
		m.mu.Lock()
		ft := m.tables[fleet]
		ft.def.Columns, _ = ntable.ParseColumns(lockedFleet)
		ft.def.Hidden = []string{sprint.Withdrawn, sprint.Finished, sprint.DoneOK, sprint.DoneFailed, sprint.DoneDefect, sprint.ProviderFailures, sprint.Coordination, sprint.Environment, sprint.Harness, sprint.NoResult, sprint.Ctl}
		m.mu.Unlock()
		assert.NoError(t, st.LockedShape(ctx))
	})

	t.Run("one extra column beside the lock passes", func(t *testing.T) {
		m := NewMem()
		st := &Store{B: m, Names: names, Actor: "coordinator"}
		require.NoError(t, st.Init(ctx))
		m.mu.Lock()
		ft := m.tables[fleet]
		ft.def.Columns = append(ft.def.Columns, ntable.Column{Name: "extra", Projection: ntable.Count, Fold: ntable.Sum})
		m.mu.Unlock()
		assert.NoError(t, st.LockedShape(ctx))
	})

	t.Run("a store with no tables yet passes", func(t *testing.T) {
		m := NewMem()
		st := &Store{B: m, Names: names, Actor: "coordinator"}
		assert.NoError(t, st.LockedShape(ctx))
	})

	t.Run("a readers table without tiers passes", func(t *testing.T) {
		m := NewMem()
		st := &Store{B: m, Names: names, Actor: "coordinator"}
		readers := names.Table(sprint.Readers)
		def := names.Definitions()
		for _, d := range def {
			if d.Name != readers {
				continue
			}
			d.Columns = d.Columns[:len(d.Columns)-1]
			require.NoError(t, m.Create(ctx, d))
		}
		assert.NoError(t, st.LockedShape(ctx))
	})
}
