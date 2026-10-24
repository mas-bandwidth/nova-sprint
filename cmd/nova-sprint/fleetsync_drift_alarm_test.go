package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/stretchr/testify/require"
)

// openConfigDrift is the open config-drift judgments of the test app's store.
func (ta *testApp) openConfigDrift() []sprint.Open {
	ta.t.Helper()
	open, err := ta.m.OpenNotes(context.Background())
	require.NoError(ta.t, err)
	var out []sprint.Open
	for _, o := range open {
		if o.Note.Type == sprint.NConfigDrift {
			out = append(out, o)
		}
	}
	return out
}

// A width changed in nova-config is raised by the tick as one judgment naming the
// member, the field and the command that applies it (the drift fleet sync --check
// reports, by the same plan), and the tick applies none of it; once fleet sync has
// run, the next tick's read closes the judgment.
func TestTheTickRaisesConfigDriftAndAppliesNothing(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	inv.set("m2", 4)
	ta.ok("fleet sync")
	ta.probeUp("m1", "m2")
	ta.ok("start")
	ta.ok("tick")
	require.Empty(t, ta.openConfigDrift(), "no drift raised a judgment")

	inv.set("m1", 8)
	_, check, _ := ta.do("fleet sync --check")
	require.Contains(t, check, "DRIFT width m1 has width 4 and the inventory says 8")
	ta.ok("tick")
	open := ta.openConfigDrift()
	require.Len(t, open, 1, "the drift raised no judgment")
	what := open[0].Note.What
	for _, want := range []string{"m1 has width 4 and the inventory says 8", "run: nova-sprint fleet sync"} {
		require.Contains(t, what, want, "the judgment lacks %q: %s", want, what)
	}
	require.NotContains(t, what, "m2", "the judgment names a member that does not differ: %s", what)
	require.Equal(t, "4", ta.fleetRows()["m1"]["width"], "the tick applied the drift")

	ta.ok("fleet sync")
	require.Equal(t, "8", ta.fleetRows()["m1"]["width"])
	ta.ok("tick")
	require.Empty(t, ta.openConfigDrift(), "the judgment stayed open after the sync")
	ta.clean()
}

// A tick whose config read fails, or holds no machine row, neither raises nor
// closes the judgment: never act on a missing read.
func TestTheTickKeepsConfigDriftWhenTheConfigCannotBeRead(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	ta.ok("fleet sync")
	ta.probeUp("m1")
	ta.ok("start")
	inv.set("m1", 2)
	ta.ok("tick")
	require.Len(t, ta.openConfigDrift(), 1, "the drift raised no judgment")

	inv.fail = errors.New("postgres at 127.0.0.1: connection refused")
	ta.ok("tick")
	require.Len(t, ta.openConfigDrift(), 1, "a failed read closed the judgment")
	inv.fail = nil
	inv.remove("m1")
	ta.ok("tick")
	require.Len(t, ta.openConfigDrift(), 1, "an empty inventory closed or replaced the judgment")
	ta.clean()
}

// The tick's config read runs at most every configDriftEvery by the app's clock:
// a call before then reads nothing and says no read; the first after it reads again.
func TestConfigWantReadsAtMostEveryFewMinutes(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	reads := 0
	read := inv.fn()
	ta.a.inventory = func(ctx context.Context, pg string) ([]config.MachineWidth, error) {
		reads++
		return read(ctx, pg)
	}
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	want := ta.a.configWant(st)
	ctx := context.Background()

	got, machines, ok := want(ctx)
	require.True(t, ok, "the first read is due")
	require.Len(t, got, 1)
	require.Equal(t, "m1", got[0].Name)
	require.Equal(t, 4, got[0].Width)
	require.Equal(t, []string{"m1"}, machines)
	require.Equal(t, 1, reads)

	ta.a.sleep(configDriftEvery - time.Second)
	_, _, ok = want(ctx)
	require.False(t, ok, "a read before configDriftEvery is not due")
	require.Equal(t, 1, reads, "the config was read before configDriftEvery")

	ta.a.sleep(time.Second)
	_, _, ok = want(ctx)
	require.True(t, ok, "the read after configDriftEvery is due")
	require.Equal(t, 2, reads)

	// a failed read is no read, and waits its interval like any
	inv.fail = errors.New("connection refused")
	ta.a.sleep(configDriftEvery)
	_, _, ok = want(ctx)
	require.False(t, ok, "a failed read said ok")
	require.Equal(t, 3, reads)
}
