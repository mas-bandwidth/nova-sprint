package main

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A member's width is nova-config's alone (the owner, 2026-10-18: "all fleet machines and
// friends must follow the one true definition of width, in nova-config. No exceptions.";
// tla/WidthHard.tla OneSource, witness MCWidthHardBrokenSecondSource): fleet up --width is
// refused a width that is not the member's nova-config machine row's, writing nothing and
// naming the config verbs; the config's own width is taken, and --width 0 still drains.
func TestFleetUpWidthIsRefusedWhenItIsNotNovaConfigs(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 8)
	ta.ok("fleet sync")
	ta.probeUp("m1")
	require.Equal(t, "8", ta.fleetRows()["m1"]["width"])

	code, out, errs := ta.do("fleet up m1 --width 16")
	assert.NotEqual(t, 0, code, "a width that is not nova-config's is refused: %s %s", out, errs)
	assert.Contains(t, out+errs, "--width 16 is not m1's nova-config width (nova-config names 8)")
	assert.Contains(t, out+errs, "nova-config machine set m1 --width 16, then nova-config apply")
	assert.Equal(t, "8", ta.fleetRows()["m1"]["width"], "nothing was changed")

	// a member nova-config does not name is refused any width
	code, out, errs = ta.do("fleet up m9 --width 4")
	assert.NotEqual(t, 0, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "m9 has no nova-config machine row")

	// the config's own width is taken, and the drain stays
	ta.ok("fleet up m1 --width 8")
	assert.Equal(t, "8", ta.fleetRows()["m1"]["width"])
	ta.ok("fleet up m1 --width 0")
	assert.Equal(t, "0", ta.fleetRows()["m1"]["width"], "--width 0 drains")
}

// A friend's width is her nova-config friend row's alone: friend up --width and friend
// beat --width are refused, writing nothing, a width that is not her row's, naming the
// config verbs; her row's own width is taken. On a machine that names no config store the
// friends table's copy (friend sync's, from her row) stands in, and is held the same
// (the reader's finding on w2: friends.go allowed any friend width).
func TestFriendWidthIsRefusedWhenItIsNotNovaConfigs(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	_, _, err := cfg.Update(context.Background(), config.KindFriend, "amy", map[string]string{"width": "2"}, "t")
	require.NoError(t, err)
	ta.ok("friend sync --root " + t.TempDir())
	require.Contains(t, ta.ok("friend beat amy"), "row_width=2")

	for _, verb := range []string{"friend up amy --width 4", "friend beat amy --width 4"} {
		code, out, errs := ta.do(verb)
		assert.NotEqual(t, 0, code, "%s: %s %s", verb, out, errs)
		assert.Contains(t, out+errs, "--width 4 is not friend amy's nova-config width (her friend row names 2)", verb)
		assert.Contains(t, out+errs, "nova-config friend set amy --width 4, then nova-config apply", verb)
	}
	assert.Contains(t, ta.ok("friend beat amy"), "row_width=2", "nothing was changed")
	ta.ok("friend up amy --width 2")
	ta.ok("friend beat amy --width 2")

	// no config store named: the friends table's copy of her row stands in
	read := ta.a.friends
	ta.a.friends = func(context.Context, string) ([]config.Row, error) { return nil, errors.New("no config store") }
	code, out, errs := ta.do("friend up amy --width 4")
	assert.NotEqual(t, 0, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "her friend row names 2")
	ta.ok("friend up amy --width 2")
	ta.a.friends = read

	// a friend nova-config does not name is refused any width
	code, out, errs = ta.do("friend up zed --width 1")
	assert.Equal(t, 1, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "friend zed has no nova-config friend row")

	// a config store named that cannot be read refuses friend up any width: never take one on a
	// missing read; friend beat never reads it (the beat lane never waits on Postgres) and
	// holds to the friends table's copy
	ta.a.friends = func(context.Context, string) ([]config.Row, error) { return nil, errors.New("connection refused") }
	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == "NOVA_PG_DSN" {
			return "postgres://nova@127.0.0.1:5432/nova"
		}
		return env(k)
	}
	code, out, errs = ta.do("friend up amy --width 2")
	assert.Equal(t, 1, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "nova-config cannot be read (connection refused)")
	ta.ok("friend beat amy --width 2")
	code, out, errs = ta.do("friend beat amy --width 4")
	assert.Equal(t, 1, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "her friend row names 2")
	ta.a.friends, ta.a.getenv = read, env
}

// Between a nova-config change and the next friend sync the friends table's copy of her row
// trails nova-config (the reader's finding on w3). friend up --width is held to nova-config's
// applied friend row as it is now, never the copy: the copy's old width is refused and the
// new one taken. friend beat --width stays on the copy and never reads nova-config (the beat
// lane never waits: the coordinator's ruling on w4); her beat's width writes only her beat
// report, and friend sync brings the copy to the applied row.
func TestFriendWidthIsHeldToTheAppliedConfigBeforeFriendSync(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	ctx := context.Background()
	_, _, err := cfg.Update(ctx, config.KindFriend, "amy", map[string]string{"width": "2"}, "t")
	require.NoError(t, err)
	ta.ok("friend sync --root " + t.TempDir())
	require.Contains(t, ta.ok("friend beat amy"), "row_width=2")

	// nova-config friend set amy --width 3, then nova-config apply: no friend sync yet
	_, _, err = cfg.Update(ctx, config.KindFriend, "amy", map[string]string{"width": "3"}, "t")
	require.NoError(t, err)
	code, out, errs := ta.do("friend up amy --width 2")
	assert.Equal(t, 1, code, "the copy's old width is refused: %s %s", out, errs)
	assert.Contains(t, out+errs, "--width 2 is not friend amy's nova-config width (her friend row names 3)")

	// the beat never reads nova-config, with or without --width
	read, reads := ta.a.friends, 0
	ta.a.friends = func(ctx context.Context, pg string) ([]config.Row, error) {
		reads++
		return read(ctx, pg)
	}
	assert.Contains(t, ta.ok("friend beat amy"), "row_width=2", "the copy trails until friend sync")
	ta.ok("friend beat amy --width 2")
	assert.Zero(t, reads, "a beat read nova-config")
	ta.ok("friend up amy --width 3")
	assert.Equal(t, 1, reads, "friend up reads nova-config")
	ta.a.friends = read

	// friend sync brings the copy to the applied row, and the beat holds to it
	ta.ok("friend sync --root " + t.TempDir())
	assert.Contains(t, ta.ok("friend beat amy"), "row_width=3")
	code, out, errs = ta.do("friend beat amy --width 2")
	assert.Equal(t, 1, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "her friend row names 3")
}

// A read of nova-config's inventory that answers with no machine row is a likely-wrong
// config (an empty or wrong database; fleet sync refuses it), never a fleet of width 0 (the
// coordinator's ruling on w4): the last applied widths are kept, no zero is written, no
// member is retired, and the tick raises one judgment, "nova-config inventory is empty",
// closed when a read holds rows again. A read with rows and none for a member still gives
// that member 0, and drains its copy.
func TestAnEmptyInventoryKeepsTheWidthsAndIsJudged(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	inv.set("m2", 3)
	ta.ok("fleet sync")
	ta.probeUp("m1", "m2")
	require.Equal(t, "4", ta.fleetRows()["m1"]["width"])

	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	st.Inventory, st.Widths = ta.a.inventoryMachines, ta.a.inventoryWidths(st)
	_, _, _, err = st.SetMachine(ctx, true) // a RUNNING machine: its tick keeps the judgments
	require.NoError(t, err)
	width := func(m string) int {
		t.Helper()
		snap, err := st.Load(ctx, []string{sprint.Fleet}, nil)
		require.NoError(t, err)
		return snap.Width(m)
	}
	openEmpty := func() int {
		t.Helper()
		open, err := ta.m.OpenNotes(ctx)
		require.NoError(t, err)
		n := 0
		for _, o := range open {
			assert.NotContains(t, o.Note.What, "("+sprint.NInventoryEmpty+") names", "the judgment is no orphan (check rule 8)")
			if o.Note.Kind == sprint.Judgment && o.Note.Type == sprint.NInventoryEmpty {
				n++
				assert.Contains(t, o.Note.What, "nova-config inventory is empty; widths kept")
			}
		}
		return n
	}
	take := func(id, m string) store.Result {
		t.Helper()
		res, err := st.Run(ctx, store.Step{Verb: "width probe", Load: []string{sprint.Fleet}, Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Units: []sprint.Unit{{Key: id, Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: id,
				Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: m, Col: sprint.Working}, Set: map[string]string{"kind": "work", "gen": "1"}}}},
				Moved: id + " created working on " + m}}}
		}})
		require.NoError(t, err)
		return res
	}
	_, err = st.Tick(ctx)
	require.NoError(t, err)
	require.Zero(t, openEmpty(), "a read with rows raises nothing")

	// nova-config machine remove m1 and m2, then nova-config apply: the read answers with no row
	inv.remove("m1")
	inv.remove("m2")
	_, err = st.Tick(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, openEmpty(), "the empty inventory is judged, once")
	_, err = st.Tick(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, openEmpty(), "and only once while it holds")
	assert.Equal(t, 4, width("m1"), "the last applied width is kept, never written 0")
	assert.Contains(t, ta.fleetRows(), "m1", "no member is retired on an empty read")
	// the kept widths are the last applied read's, not the copies: a copy written 1 behind
	// the server's back is made 4 again by the next step, which the kept 4 holds (at w3 the
	// empty read was no read, the copy's 1 stood, and the take was refused)
	snap, err := st.Load(ctx, []string{sprint.Fleet}, nil)
	require.NoError(t, err)
	ctl := snap.MemberCtl("m1")
	require.NotNil(t, ctl)
	res, err := st.Run(ctx, store.Step{Verb: "width copy", Load: []string{sprint.Fleet}, Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Units: []sprint.Unit{{Key: ctl.ID, Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: ctl.ID,
			Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(ctl.Rev, 10)}, Set: map[string]string{sprint.FieldWidth: "1"}}}},
			Moved: "m1 width=1"}}}
	}})
	require.NoError(t, err)
	require.Empty(t, res.Refused, "%v", res.Refused)
	for _, id := range []string{"z0", "z1"} {
		res = take(id, "m1")
		assert.Empty(t, res.Refused, "the kept width 4 holds the step, not the copy's 1: %v", res.Refused)
	}
	assert.Equal(t, 4, width("m1"), "the copy is the kept width again")

	// a read that fails neither raises nor closes it (and, a missing read, leaves the copies)
	inv.fail = errors.New("connection refused")
	_, err = st.Tick(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, openEmpty(), "no read is never a clear")
	inv.fail = nil

	// the rows come back, without m1: m1 (no row in a read with rows) is 0 at once, and the
	// step drains its copy; the next tick closes the judgment (and retires m1, whose row is gone)
	inv.set("m2", 3)
	_, ok := ta.a.inventoryMachines(ctx)
	require.True(t, ok)
	res = take("z2", "m1")
	require.Len(t, res.Refused, 1, "m1 has no row in a read with rows: %v", res.Refused)
	assert.Regexp(t, `m1 is at its width \(\d+ working of 0\)`, res.Refused[0].Why)
	assert.Equal(t, 0, width("m1"), "its copy is drained to nova-config's 0")
	_, err = st.Tick(ctx)
	require.NoError(t, err)
	assert.Zero(t, openEmpty(), "a read with rows closes it")
	assert.Equal(t, 3, width("m2"))
}

// A member whose nova-config machine row has the default width is half the cores its beat
// reports, at fleet up and at every step the server holds to the config read
// (inventoryWidths), as fleet sync resolves it; a row of width 0 is 0. No step falls back to
// a width nova-config does not name.
func TestADefaultWidthMemberIsHalfItsCores(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	inv.setDefault("m3")
	inv.set("m4", 0)
	ta.ok("fleet beat m3 --cores 32 --load 0")
	ta.ok("fleet sync")
	ta.probeUp("m1", "m3")
	require.Equal(t, "16", ta.fleetRows()["m3"]["width"])

	code, out, errs := ta.do("fleet up m3 --width 8")
	assert.NotEqual(t, 0, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "--width 8 is not m3's nova-config width (nova-config names 16)")
	ta.ok("fleet up m3 --width 16")

	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	ws, ok := ta.a.inventoryWidths(st)(context.Background())
	require.True(t, ok)
	assert.Equal(t, map[string]int{"m1": 4, "m3": 16, "m4": 0}, ws)
}

// configFriendWidth sets the friend's nova-config friend row's width, as nova-config friend
// set <friend> --width <n> then nova-config apply does: friend up --width and friend sync
// take it from there (a friend's width is her row's alone).
func (ta *testApp) configFriendWidth(friend string, width int) {
	ta.t.Helper()
	read := ta.a.friends
	ta.a.friends = func(ctx context.Context, pg string) ([]config.Row, error) {
		rows, err := read(ctx, pg)
		for i := range rows {
			if rows[i].Name == friend {
				fields := map[string]string{}
				for k, v := range rows[i].Fields {
					fields[k] = v
				}
				fields["width"] = strconv.Itoa(width)
				rows[i].Fields = fields
			}
		}
		return rows, err
	}
}
