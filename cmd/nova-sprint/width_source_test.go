package main

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"

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
