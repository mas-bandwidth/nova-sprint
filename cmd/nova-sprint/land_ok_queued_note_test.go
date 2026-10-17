package main

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// 2026-10-17 from 14:02: the server's land loop printed LAND OK for batches whose
// merging -> landed moves were queued behind a tick drain the store refused every
// tick. A LAND OK behind a failing tick is followed by the NOTE that says so.
func TestALandOKBehindAFailingTickSaysSo(t *testing.T) {
	t.Parallel()
	running := store.Machine{State: store.Running}
	failing := store.Heartbeat{Ticks: 1003, Failures: 4, Error: "tick drain: table work: 65 properties, over the bound of 64"}
	ok := []string{"LAND OK stream=s1 cards=1 base=main tip=abc ids=s1-1"}
	got := landOKQueuedNote(ok, running, failing)
	require.Len(t, got, 2)
	require.Equal(t, ok[0], got[0])
	require.True(t, strings.HasPrefix(got[1], "NOTE queued:"), got[1])
	require.Contains(t, got[1], failing.Error)
	require.Equal(t, ok, landOKQueuedNote(ok, running, store.Heartbeat{Ticks: 1003}), "a healthy tick adds nothing")
	refused := []string{"LAND REFUSED stream=s1 cards=1 base=main tip=- ids=s1-1"}
	require.Equal(t, refused, landOKQueuedNote(refused, running, failing), "no LAND OK, nothing added")
}
