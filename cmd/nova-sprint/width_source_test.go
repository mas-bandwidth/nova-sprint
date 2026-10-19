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

	// a config store named that cannot be read refuses friend up and friend beat any width:
	// never take one on a missing read, and never the friends table's copy in its place; a
	// beat with no --width (what her daemon sends) reads no config and is taken
	ta.a.friends = func(context.Context, string) ([]config.Row, error) { return nil, errors.New("connection refused") }
	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == "NOVA_PG_DSN" {
			return "postgres://nova@127.0.0.1:5432/nova"
		}
		return env(k)
	}
	for _, verb := range []string{"friend up amy --width 2", "friend beat amy --width 2"} {
		code, out, errs = ta.do(verb)
		assert.Equal(t, 1, code, "%s: %s %s", verb, out, errs)
		assert.Contains(t, out+errs, "nova-config cannot be read (connection refused)", verb)
	}
	assert.Contains(t, ta.ok("friend beat amy --working 0"), "row_width=2")
	ta.a.friends, ta.a.getenv = read, env
}

// Between a nova-config change and the next friend sync the friends table's copy of her row
// is stale, and it is never the check: friend beat --width and friend up --width are held to
// nova-config's applied friend row as it is now (the reader's finding on w3: the beat checked
// the stale copy and took the old width). At w3 the beat took the old width 2 here and
// refused the new 3.
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
	require.Contains(t, ta.ok("friend beat amy"), "row_width=2", "the copy is stale until friend sync")

	for _, verb := range []string{"friend beat amy --width 2", "friend up amy --width 2"} {
		code, out, errs := ta.do(verb)
		assert.Equal(t, 1, code, "%s: the copy's old width is refused: %s %s", verb, out, errs)
		assert.Contains(t, out+errs, "--width 2 is not friend amy's nova-config width (her friend row names 3)", verb)
	}
	ta.ok("friend beat amy --width 3")
	ta.ok("friend up amy --width 3")

	// friend sync brings the copy to the applied row
	ta.ok("friend sync --root " + t.TempDir())
	assert.Contains(t, ta.ok("friend beat amy"), "row_width=3")
}

// A read of nova-config that answers with no machine row is a read, never a missing one
// (the reader's finding on w3: it was kept as no read, so the control card's stale copy
// still admitted takes after the last machine row was removed). Every member's width is 0 in
// it: the server's steps admit no take and drain the copy (sprint.WidthRefresh); the retire
// keeps its own rule and retires nothing on it. Only a read that fails leaves the copies.
// At w3 the take below applied (the copy's 4 admitted it) and the copy stayed 4.
func TestAnEmptyInventoryHoldsEveryMemberAtZero(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	ta.ok("fleet sync")
	ta.probeUp("m1")
	require.Equal(t, "4", ta.fleetRows()["m1"]["width"])

	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	st.Widths = ta.a.inventoryWidths(st)

	// a read that fails: the copies stand, as before
	inv.fail = errors.New("connection refused")
	_, ok := ta.a.inventoryMachines(ctx)
	require.False(t, ok)
	_, ok = st.Widths(ctx)
	require.False(t, ok, "a failed read is no read")

	// nova-config machine remove m1, then nova-config apply: the read answers with no row
	inv.fail = nil
	inv.remove("m1")
	names, ok := ta.a.inventoryMachines(ctx)
	assert.False(t, ok, "the retire acts on no read with no row: %v", names)
	ws, ok := st.Widths(ctx)
	require.True(t, ok, "a read that answered with no row is a read")
	assert.Empty(t, ws)

	res, err := st.Run(ctx, store.Step{Verb: "width probe", Load: []string{sprint.Fleet}, Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Units: []sprint.Unit{{Key: "z1", Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: "z1",
			Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: "m1", Col: sprint.Working}, Set: map[string]string{"kind": "work", "gen": "1"}}}},
			Moved: "z1 created working on m1"}}}
	}})
	require.NoError(t, err)
	require.Len(t, res.Refused, 1, "the stale copy's 4 admits nothing: %v", res.Refused)
	assert.Contains(t, res.Refused[0].Why, "m1 is at its width (0 working of 0)")
	assert.Equal(t, "0", ta.fleetRows()["m1"]["width"], "the copy is drained to nova-config's 0")
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
