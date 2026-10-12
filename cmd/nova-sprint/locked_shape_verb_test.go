package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// oldFleetShape is the fleet table's v1.2.9 columns and hidden list
// (internal/sprint/store/migration_test.go, fleetBeforeVerdicts): what the live
// store held before the readers'-verdicts migration.
const oldFleetShape = "ready,working,width:text:sum,done:sum(ok+failed),okpct:pct(ok/ok+failed):pooled:ok%,status:text,load:text,withdrawn,ok,failed,defect,ctl:first:none"

// lockedFleetShapes is a backend that answers Shapes for the fleet table with
// that old shape: the test Backend the card names, embedding the twin's Mem so
// every other call passes through.
type lockedFleetShapes struct {
	*store.Mem
	fleet string
}

func (b lockedFleetShapes) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	out, err := b.Mem.Shapes(ctx, tables)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Name != b.fleet {
			continue
		}
		cols, err := ntable.ParseColumns(oldFleetShape)
		if err != nil {
			return nil, err
		}
		out[i].Columns = cols
		out[i].Hidden = []string{sprint.Withdrawn, sprint.DoneOK, sprint.DoneFailed, sprint.DoneDefect, sprint.Ctl}
	}
	return out, nil
}

// fleetOldStore is a test app whose store's fleet table looks v1.2.9.
func fleetOldStore(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	for _, l := range []string{"init --readers reader-a --members m1", "add --stream s1 --count 1 --one", "start"} {
		code, _, errs := ta.do(l)
		require.Equal(t, 0, code, "%s: %s", l, errs)
	}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return lockedFleetShapes{Mem: ta.m, fleet: sprint.Names{}.Table(sprint.Fleet)}, nil
	}
	return ta
}

// TestShadowTickRefusesAStoreMissingTheLockedFleetColumns: tick --shadow refuses
// the store before it plans when its fleet table lacks the locked columns, prints
// SHADOW TICK FAILED ... wrote=nothing, puts the three TABLES.lock commands in
// the --json error, and exits 2. docs/SPEC-SPRINT.md section 14
// (install-canary-shadow-tick-r.w1).
func TestShadowTickRefusesAStoreMissingTheLockedFleetColumns(t *testing.T) {
	t.Parallel()
	ta := fleetOldStore(t)
	var out, errb bytes.Buffer
	code := runInto(ta.a, []string{"tick", "--shadow"}, &out, &errb)
	require.Equal(t, 2, code)
	assert.Contains(t, out.String(), "SHADOW TICK FAILED")
	assert.Contains(t, out.String(), "wrote=nothing")
	for _, cmd := range []string{
		"nova-table col add fleet provider --after okpct",
		"nova-table col add fleet redealt --after no_result",
		"nova-table col add fleet finished --after withdrawn",
		"nova-table set fleet --hide provider",
		"nova-table set fleet --hide finished",
	} {
		assert.Contains(t, errb.String(), cmd)
	}

	out.Reset()
	errb.Reset()
	code = runInto(ta.a, []string{"tick", "--shadow", "--json"}, &out, &errb)
	require.Equal(t, 2, code)
	for _, cmd := range []string{
		"nova-table col add fleet provider --after okpct",
		"nova-table col add fleet redealt --after no_result",
		"nova-table col add fleet finished --after withdrawn",
		"nova-table set fleet --hide provider",
		"nova-table set fleet --hide finished",
	} {
		assert.Contains(t, out.String(), cmd)
	}
}

// TestRunRefusesAStoreMissingTheLockedFleetColumns: run refuses before it reads
// a key, binds a port or begins a loop when a locked column is missing, through
// refuse() with the named sentence; the shadow test's backend is the store it
// opens, so run sees the same v1.2.9 fleet table.
func TestRunRefusesAStoreMissingTheLockedFleetColumns(t *testing.T) {
	t.Parallel()
	ta := fleetOldStore(t)
	var out, errb bytes.Buffer
	code := runInto(ta.a, []string{"run"}, &out, &errb)
	require.Equal(t, 2, code)
	assert.Contains(t, errb.String(), "the store's tables lack the columns this build writes:")
	assert.Contains(t, errb.String(), "this server does not start")
	assert.Contains(t, errb.String(), "nova-table col add fleet finished --after withdrawn")
	assert.NotContains(t, out.String(), "RUN ticking")
}
