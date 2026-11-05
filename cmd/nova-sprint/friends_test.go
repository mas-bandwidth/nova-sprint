package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/hook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// The friends table (docs/SPEC-SPRINT.md section 1; the owner, 2026-10-02: "add a
// friends table, above fleet and below merge. friends | status for now.
// up/down/held"; and the same day: "please give friends in the friends table the
// same ready, working, width, done, ok%, status that we have for machines, but no
// load"): its rows are nova-config's friend rows, its counts the friend's sprint
// cards (the cards dealt to her fleet row friend.<name>, their states and their
// finish verdicts), its status the friends' rule (down after 15 s without a
// beat) over her beats and the coordinator's hold, its order the fleet's.

// emptyFriends is the friends table with no friend: its header, one rule and
// the footer, as every empty table is.
const emptyFriends = "friends | ready | working | width | done | ok%  | redealt | status | active | tokens\n" +
	"--------+-------+---------+-------+------+------+---------+--------+--------+-------\n" +
	"        |     0 |       0 |     0 |    0 | 0.0% |       0 |        |        |      0"

// friendApp is a test app with an initialised sprint whose friend sync reads the
// friend rows of nova-config's in-memory store, each named in friends.
func friendApp(t *testing.T, friends ...string) (*testApp, *config.Mem) {
	t.Helper()
	ta := newTestApp(t)
	// the friends' working directories are under HOME, <HOME>/<friend>-working;
	// none is there until a test writes one (jobs)
	home, prev := t.TempDir(), ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
		return prev(k)
	}
	cfg := config.NewMem()
	ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) {
		return cfg.List(ctx, config.KindFriend)
	}
	for _, f := range friends {
		addFriendRow(t, cfg, f)
	}
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	return ta, cfg
}

// A friend's width is her friend row's (the owner, 2026-10-02: "6/1 seems a bit
// wrong -- need to setup width for friends? Start at 8 for each?"): friend sync
// writes the row's width, 8 when the row has no width field; a width changed
// in nova-config moves the friends table at the next sync (updated=<friend>);
// and a row whose width is below 1 is refused in one line naming the
// nova-config set that fixes it, with nothing written.
func TestFriendSyncWritesTheConfiguredWidth(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "cat", Fields: map[string]string{"tiers": "flash", "width": "3"}}, "t")
	require.NoError(t, err)
	ta.ok("friend sync")
	width := func() map[string]string {
		var w whereView
		ta.json("where", &w)
		out := map[string]string{}
		for f, row := range w.Tables[sprint.Friends] {
			out[f] = cellText(row[sprint.FieldWidth])
		}
		return out
	}
	assert.Equal(t, map[string]string{"amy": "8", "cat": "3"}, width(), "amy's row has no width: the default; cat's is 3")

	_, _, err = cfg.Update(context.Background(), config.KindFriend, "amy", map[string]string{"width": "2"}, "t")
	require.NoError(t, err)
	assert.Contains(t, ta.ok("friend sync"), "FRIEND-SYNC OK added=- removed=- updated=amy friends=2")
	assert.Equal(t, map[string]string{"amy": "2", "cat": "3"}, width())

	ta.a.friends = func(context.Context, string) ([]config.Row, error) {
		return []config.Row{{Name: "amy", Fields: map[string]string{"width": "0"}}, {Name: "cat", Fields: map[string]string{"width": "5"}}}, nil
	}
	code, out, errs := ta.do("friend sync")
	assert.Equal(t, 1, code)
	assert.Empty(t, out)
	assert.Equal(t, "nova-sprint friend sync: friend amy has width 0, and a friend's width is at least 1; run: nova-config friend set amy --width <n>; nothing was changed\n", errs)
	assert.Equal(t, map[string]string{"amy": "2", "cat": "3"}, width(), "nothing was written")
}

// friendRows is a friendsFn over friend rows of the names given, each with
// no width field (so the default width).
func friendRows(names ...string) friendsFn {
	return func(context.Context, string) ([]config.Row, error) {
		rows := make([]config.Row, len(names))
		for i, n := range names {
			rows[i] = config.Row{Name: n, Fields: map[string]string{"tiers": "flash"}}
		}
		return rows, nil
	}
}

func addFriendRow(t *testing.T, cfg *config.Mem, name string) {
	t.Helper()
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: name, Fields: map[string]string{"tiers": "flash"}}, "t")
	require.NoError(t, err)
}

// frame is one where frame, as where prints it.
func (ta *testApp) frame() string {
	ta.t.Helper()
	return ta.ok("where")
}

// friendStatus is each friend's status as where --json carries it.
func (ta *testApp) friendStatus() map[string]string {
	ta.t.Helper()
	var w whereView
	ta.json("where", &w)
	out := map[string]string{}
	for f, row := range w.Tables[sprint.Friends] {
		out[f] = cellText(row[sprint.Status])
	}
	return out
}

// The table of two friends, one up and one held: the header, a rule, the friend
// up first and then the one held, a rule and the summary row with its cell blank.
// The empty store draws the header, its one rule and the summary row, as every
// empty table does. Either way it stands after work and before fleet in the default
// frame, which hides the readers and merge tables, and after merge and before fleet
// in the frame of where --all.
func TestTheFriendsTableShowsAfterMergeAndBeforeFleet(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		friends []string
		lines   []string
		want    string
	}{
		{name: "the empty store", want: emptyFriends},
		{name: "two friends, one up and one held", friends: []string{"friend-a", "friend-b"},
			lines: []string{"friend sync", "friend beat friend-b", "friend down friend-a"},
			want: "friends  | ready | working | width | done | ok%  | redealt | status | active | tokens\n" +
				"---------+-------+---------+-------+------+------+---------+--------+--------+-------\n" +
				"friend-b |     0 |       0 |     8 |    0 | 0.0% |       0 | up     | -      |      0\n" +
				"friend-a |     0 |       0 |     8 |    0 | 0.0% |       0 | held   | -      |      0\n" +
				"---------+-------+---------+-------+------+------+---------+--------+--------+-------\n" +
				"         |     0 |       0 |    16 |    0 | 0.0% |       0 |        |        |      0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ta, _ := friendApp(t, tc.friends...)
			if len(tc.friends) > 0 {
				ta.pong("friend-a")
				ta.pong("friend-b")
			}
			for _, l := range tc.lines {
				ta.ok(l)
			}
			frame := ta.frame()
			assert.Equal(t, tc.want, tableOf(frame, sprint.Friends))
			titles := func(frame string) []string {
				var order []string
				for _, block := range strings.Split(frame, "\n\n") {
					if title, _, ok := strings.Cut(block, " |"); ok && !strings.Contains(title, "\n") {
						order = append(order, strings.TrimSpace(title))
					}
				}
				return order
			}
			assert.Equal(t, []string{"work", "friends", "fleet"}, titles(frame))
			assert.Equal(t, []string{"work", "readers", "merge", "friends", "fleet"}, titles(ta.ok("where --all")))
		})
	}
}

// pong is a wake ping the friend's session answered, as the coordinator writes it: friend
// health --state up at the clock now, by the seat's holder at its generation.
func (ta *testApp) pong(friend string) {
	ta.t.Helper()
	prev := ta.a.readFriendHooks
	ta.a.readFriendHooks = func(ctx context.Context, names ...string) ([]hook.Status, time.Time, error) {
		by := map[string]hook.Status{}
		if prev != nil {
			rows, _, err := prev(ctx, names...)
			if err != nil {
				return nil, time.Time{}, err
			}
			for _, r := range rows {
				by[r.Name] = r
			}
		}
		by[friend] = hook.Status{Name: friend, Role: hook.Friend, State: hook.StateProven}
		var out []hook.Status
		for _, n := range names {
			if r, ok := by[n]; ok {
				out = append(out, r)
			}
		}
		return out, ta.now, nil
	}
}

// beatUp is a friend's beat and a wake ping her session answered: a friend up, as the
// tests that deal to her want her (a beat alone is no evidence).
func (ta *testApp) beatUp(friend string) {
	ta.t.Helper()
	ta.ok("friend beat " + friend)
	ta.pong(friend)
}

// A friend is up only on her session's evidence: a wake ping her session answered under
// the pong window old (or a card of hers finished); a beat is never evidence; held
// while friend down holds her whatever she does, and friend up releases the hold without
// counting as evidence. The rows go up, then held, then down, each by name
// (store.FleetOrder).
func TestFriendSyncFollowsTheConfigAndAHoldSurvivesIt(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy", "bob")
	assert.Contains(t, ta.ok("friend sync"), "FRIEND-SYNC OK added=amy,bob removed=- updated=- friends=2")
	ta.beatUp("bob")
	ta.pong("amy")
	ta.ok("friend down amy")
	assert.Contains(t, ta.ok("friend sync"), "nothing to do")

	_, err := cfg.Delete(context.Background(), config.KindFriend, "bob", "t")
	require.NoError(t, err)
	addFriendRow(t, cfg, "cat")
	assert.Contains(t, ta.ok("friend sync"), "FRIEND-SYNC OK added=cat removed=bob updated=- friends=2")
	assert.Equal(t, map[string]string{"amy": "held", "cat": "down"}, ta.friendStatus(), "amy's hold survived the sync")

	addFriendRow(t, cfg, "bob")
	ta.ok("friend sync")
	assert.Equal(t, "up", ta.friendStatus()["bob"], "roster removal cannot erase the authoritative hook record")
}

// Each refusal names what it wants and changes nothing: a friend that is no row
// of the friends table, a config that cannot be read or holds no friend row
// (friend sync, down and up by another actor: TestEveryCoordinatorVerbIsTheCoordinators).
func TestTheFriendVerbsRefuse(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t)
	code, _, errs := ta.do("friend sync")
	assert.Equal(t, exitCannotRead, code)
	assert.Contains(t, errs, "holds no friend row")

	addFriendRow(t, cfg, "amy")
	ta.ok("friend sync")
	for _, line := range []string{"friend beat bob", "friend down bob", "friend up bob"} {
		code, out, errs := ta.do(line)
		assert.Equal(t, 1, code, line)
		assert.Empty(t, out, line)
		assert.Contains(t, errs, "no friend bob on the friends table (friends: amy)", line)
		assert.Contains(t, errs, "run: nova-sprint friend sync", line)
	}
	code, _, errs = ta.do("friend beat")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "wants one friend")

	ta.a.friends = func(context.Context, string) ([]config.Row, error) { return nil, errors.New("connection refused") }
	code, _, errs = ta.do("friend sync")
	assert.Equal(t, exitCannotRead, code)
	assert.Contains(t, errs, "the config cannot be read")
	assert.Equal(t, map[string]string{"amy": "down"}, ta.friendStatus(), "nothing was changed")
}

// A friend beats through the sprint's server as a member does: `friend beat
// <friend>` and nothing more, its actor the friend.
func TestAFriendBeatsThroughTheServer(t *testing.T) {
	t.Parallel()
	r := newServerRig(t, "nova-sprint init --readers reader-a,reader-b --members m1:2")
	r.a.friends = friendRows("amy")
	r.boss("nova-sprint friend sync")
	res := r.one("friend", "beat", "amy")
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, "FRIEND-BEAT OK amy")
	assert.Contains(t, r.boss("nova-sprint where"), "amy     |     0 |       0 |     8 |    0 | 0.0% |       0 | down", "a beat is recorded, and never makes her up")
	for name, argv := range map[string][]string{
		"no friend":       {"friend", "beat"},
		"another actor":   {"friend", "beat", "amy", "--actor", "boss"},
		"a hold":          {"friend", "down", "amy"},
		"a list":          {"friend", "beat", "amy,bob"},
		"a store":         {"friend", "beat", "amy", "--redis", "mem:/tmp/x"},
		"a second friend": {"friend", "beat", "amy", "bob"},
	} {
		res := r.one(argv...)
		assert.Equal(t, 2, res.Code, name)
		assert.Contains(t, res.Stderr, "nothing was changed", name)
	}
}

// A friend's beat answers her row as friend sync last wrote it, her width
// and pronouns, and a sync after the row changes is what the next beat says.
func TestAFriendsBeatAnswersHerRowsWidth(t *testing.T) {
	t.Parallel()
	r := newServerRig(t, "nova-sprint init --readers reader-a,reader-b --members m1:2")
	width := ""
	r.a.friends = func(context.Context, string) ([]config.Row, error) {
		f := map[string]string{"tiers": "flash"}
		if width != "" {
			f["width"] = width
		}
		return []config.Row{{Name: "amy", Fields: f}}, nil
	}
	r.boss("nova-sprint friend sync")
	res := r.one("friend", "beat", "amy")
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, " row_width=8 row_pronouns=they/them")
	width = "1"
	r.boss("nova-sprint friend sync")
	res = r.one("friend", "beat", "amy")
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, " row_width=1 row_pronouns=they/them")
	assert.NotContains(t, res.Stdout, "row_mode")
}
