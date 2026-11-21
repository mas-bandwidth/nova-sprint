package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// putFriend records name's sprint settings in w's store as body.
func putFriend(t *testing.T, w *world, name, body string) {
	t.Helper()
	applyTest(t, w.f, store.Op{ID: "friend-" + name, Epoch: 1, Actor: "carol", Kind: "friend-set", At: t0, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: roster.FriendPrefix + name, Body: body}}})
}

// TestFriendSetVerb: the coordinator sets a friend's width, tiers or both,
// each a new data version friend:<name>; a flag left out keeps what is in
// force (the defaults when nothing is recorded); the same settings again
// change nothing; a width below 1, an unknown tier, no flag, a name
// nova-config lists as no friend, an unreadable roster and a worker caller
// are refused naming the fix.
func TestFriendSetVerb(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "width", as: "carol", args: []string{"friend", "set", "dave", "--width", "3"}, exit: 0,
			want: []string{"FRIEND dave width 3 tiers flash,pro,heavy,frontier applied at seq 11 (policy v1, friend:dave)", "next: nova-sprint show dave"}},
		{name: "tiers keep the width", as: "carol", args: []string{"friend", "set", "dave", "--tiers", "heavy,pro"}, exit: 0,
			prep: func(t *testing.T, w *world) { putFriend(t, w, "dave", `{"width":2,"tiers":["flash"]}`) },
			want: []string{"FRIEND dave width 2 tiers pro,heavy applied"}},
		{name: "a disabled friend", as: "carol", args: []string{"friend", "set", "erin", "--width", "2", "--tiers", "pro"}, exit: 0,
			prep: func(t *testing.T, w *world) { w.roster.ro.Friends["erin"] = true },
			want: []string{"FRIEND erin width 2 tiers pro applied"}},
		{name: "the same", as: "carol", args: []string{"friend", "set", "dave", "--width", "1"}, exit: 0,
			want: []string{"FRIEND dave already width 1 tiers flash,pro,heavy,frontier; nothing changed"}},
		{name: "an unreadable record replaced", as: "carol", args: []string{"friend", "set", "dave", "--width", "2", "--tiers", "pro"}, exit: 0,
			prep: func(t *testing.T, w *world) { putFriend(t, w, "dave", `{"width":0}`) },
			want: []string{"FRIEND dave width 2 tiers pro applied"}},
		{name: "an unreadable record and one flag", as: "carol", args: []string{"friend", "set", "dave", "--width", "2"}, exit: 1,
			prep: func(t *testing.T, w *world) { putFriend(t, w, "dave", `{"width":0}`) },
			want: []string{"friend dave's sprint settings cannot be read (policy v1 (friend:dave): width 0 is below 1", "nova-sprint friend set dave --width <n> --tiers <a,b>"}},
		{name: "width 0", as: "carol", args: []string{"friend", "set", "dave", "--width", "0"}, exit: 1,
			want: []string{"friend dave: width 0 is below 1", "nova-sprint friend set dave --width <n>"}},
		{name: "not a number", as: "carol", args: []string{"friend", "set", "dave", "--width", "two"}, exit: 2, want: []string{"--width"}},
		{name: "an unknown tier", as: "carol", args: []string{"friend", "set", "dave", "--tiers", "pro,turbo"}, exit: 1,
			want: []string{`friend dave: tier "turbo" is not one of flash,pro,heavy,frontier`}},
		{name: "no flag", as: "carol", args: []string{"friend", "set", "dave"}, exit: 2, want: []string{"friend set takes --width <n>, --tiers <a,b> or both"}},
		{name: "a bench", as: "carol", args: []string{"friend", "set", "alice", "--width", "2"}, exit: 1,
			want: []string{"nova-config lists no friend alice", "nova-config friend add alice --harness <harness>"}},
		{name: "the roster unreadable", as: "carol", args: []string{"friend", "set", "dave", "--width", "2"}, exit: 2,
			prep: func(t *testing.T, w *world) { w.roster.err = errBroken }, want: []string{"nova-config's roster cannot be read"}},
		{name: "a worker", as: "alice", args: []string{"friend", "set", "dave", "--width", "2"}, exit: 1, want: []string{"coordinator"}},
	})
	w := newWorld(t)
	w.ok(t, "carol", "friend", "set", "dave", "--width", "3", "--tiers", "frontier,pro")
	vs, err := w.f.PoliciesNamed(context.Background(), "data", "friend:dave", 1)
	if err != nil || len(vs) != 1 || vs[0].Body != `{"width":3,"tiers":["pro","frontier"]}` {
		t.Fatalf("stored %+v %v", vs, err)
	}
	if w.snap(t).Policy != 0 {
		t.Fatal("a friend's settings activated a policy")
	}
}

// TestShowFriendRow: show of a friend's row carries its sprint settings in
// force, its record's version, or the defaults when it has none.
func TestShowFriendRow(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	shown := func() friendView {
		t.Helper()
		out := w.ok(t, "carol", "show", "dave")
		var got struct {
			Friend *friendView `json:"friend"`
		}
		if err := json.Unmarshal([]byte(out[:strings.LastIndex(out, "\nnext: ")]), &got); err != nil || got.Friend == nil {
			t.Fatalf("show dave: %v\n%s", err, out)
		}
		return *got.Friend
	}
	if got := shown(); got.Width != 1 || len(got.Tiers) != 4 || got.Version != 0 || got.Set != "nova-sprint friend set dave --width <n> --tiers <a,b>" {
		t.Fatalf("defaults %+v", got)
	}
	w.ok(t, "carol", "friend", "set", "dave", "--width", "2", "--tiers", "pro")
	if got := shown(); got.Width != 2 || len(got.Tiers) != 1 || got.Version != 1 {
		t.Fatalf("recorded %+v", got)
	}
	putFriend(t, w, "dave", `{"width":0}`)
	if got := shown(); got.Unread == "" || got.Version != 2 {
		t.Fatalf("unreadable %+v", got)
	}
	w.roster.err = errors.New("nova-config down")
	want(t, w.do("carol", "show", "dave"), 0, `"roster_unread": "nova-config down"`)
}
