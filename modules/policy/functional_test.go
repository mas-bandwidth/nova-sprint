package policy

// Functional tests: the verb suite on a throwaway PostgreSQL of each test's
// own (initdb into t.TempDir(), a free port, fsync off; skipped when initdb
// is not found, as modules/store/functional_test.go), and the load measurement,
// which reads real time. Run them alone with: go test -run Functional ./modules/policy/

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/modules/pgbin"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestFunctionalVerbsPG(t *testing.T) {
	t.Parallel()
	verbSuite(t, func(t testing.TB) store.Store { return startPG(t) })
}

// startPG starts a PostgreSQL of this test's own and returns the store on it, migrated.
func startPG(t testing.TB) *store.PG {
	t.Helper()
	// -m immediate: the postmaster stops its children before it exits, so
	// none writes into the removed TempDir.
	dsn := pgbin.Start(t, pgbin.Options{Stop: "immediate"}).DSN
	p, err := store.Open(context.Background(), dsn, "")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	require.NoError(t, p.Migrate(context.Background()))
	return p
}

// TestFunctionalLoad: policy's share of a tick at 5,000 ready producers
// over 100 rows that all serve every tier (the breaker's case: every rows
// question has 100 candidates), with a cold cache and a warm one; the cold
// tick must fit TickTime. It runs alone, before the parallel tests, since a
// timing contends for the cores. The number is in LAYER-5's Built section.
func TestFunctionalLoad(t *testing.T) { // unitlint: not parallel, a timing contends for the cores
	if raceSlowdown > 1 {
		breakTiming(t)
		t.Skip("timing under -race measures the race detector")
	}
	snap, ro := loadSnap(5000, 100)
	q := FullQuestions(snap, nil)
	e := timedEngine()
	vs := Versions{Active: pv(1, defaultBody)}
	start := time.Now()
	a, fs := e.Answer(vs, snap, ro, q)
	cold := time.Since(start)
	require.Empty(t, fs)
	require.Len(t, a.Rows, 5000)
	start = time.Now()
	_, fs = e.Answer(vs, snap, ro, q)
	warm := time.Since(start)
	require.Empty(t, fs)
	t.Logf("policy at 5000 producers, 100 rows: cold %s, warm %s (TickTime %s)", cold, warm, TickTime)
	require.Less(t, cold, TickTime, "the cold tick fits the tick's time budget")
}
