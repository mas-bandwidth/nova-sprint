package main

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The land loop's NOTE for a LAND OK behind a failing tick reads the store's machine
// only when a land line says LAND OK: hasLandOK is that test, and a round whose land
// run says none never opens the store for it.
func TestALandRoundWithNoLandOKReadsNoMachine(t *testing.T) {
	t.Parallel()
	require.False(t, hasLandOK([]string{
		"LAND REFUSED stream=s1 cards=1 base=main tip=- ids=s1-1",
		"LAND DONE landed=0",
	}), "a refused or done line is no LAND OK")
	require.True(t, hasLandOK([]string{
		"LAND OK stream=s1 cards=1 base=main tip=abc ids=s1-1",
	}), "a LAND OK line is a LAND OK")

	// A landOnce round whose land run prints no LAND OK opens no store. The merge
	// queue and land itself are the loop's seams (queueSeam and landSeam), so any
	// backend call here is the round's read of the machine for the queued note.
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.mu.Lock()
	ta.now = time.Unix(1000, 0).UTC()
	ta.mu.Unlock()
	opens := 0
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		opens++
		return ta.m, nil
	}
	b := ta.a.landState()
	b.mu.Lock()
	b.queueSeam = func(context.Context, string) (int, string, string, error) {
		return 1, "coordinator", "s1", nil
	}
	b.landSeam = func(_ []string, stdout, _ io.Writer, _ **hookGateError) int {
		_, _ = fmt.Fprintln(stdout, "LAND DONE landed=1")
		return 0
	}
	b.mu.Unlock()

	code, _ := ta.a.landOnce(context.Background(), "mem:0", nil, io.Discard)
	require.Equal(t, 0, code)
	require.Zero(t, opens, "a round with no LAND OK opened the store")
}
