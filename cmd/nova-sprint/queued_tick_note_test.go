package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// 2026-10-17 from 14:02: add printed its cards admitted while every tick drain
// was refused, so the queued changes were never applied and nothing said so. A
// step whose work-table changes queue behind a failing tick says it beside its OK.
func TestAQueuedStepNamesAFailingTick(t *testing.T) {
	t.Parallel()
	running := store.Machine{State: store.Running}
	failing := store.Heartbeat{Ticks: 1003, Failures: 4, Error: "tick drain: table work: 65 properties, over the bound of 64"}
	note := queuedTickNote(running, failing, true)
	require.Contains(t, note, "queued")
	require.Contains(t, note, "4 ticks")
	require.Contains(t, note, failing.Error)
	require.Contains(t, queuedTickNote(running, store.Heartbeat{Ticks: 1003, Failures: 1, Error: "x"}, true), "the last tick failed")
	require.Empty(t, queuedTickNote(running, store.Heartbeat{Ticks: 1003}, true), "a healthy tick says nothing")
	require.Empty(t, queuedTickNote(store.Machine{State: store.Stopped}, failing, true), "a STOPPED machine drains before the step")
	require.Empty(t, queuedTickNote(running, failing, false), "nothing queued, nothing said")
}
