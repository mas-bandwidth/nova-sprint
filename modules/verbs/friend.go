package verbs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// A friend's sprint settings (roster.FriendSetting): how many cards it works
// at once and the tiers it serves. They are the sprint's, not the fleet's
// (the owner, 2026-11-21: "If the config is sprint specific, it goes in nova
// sprint"), so they live in the store as data versions friend:<name>, the
// newest in force; nova-config keeps who the friend is.

// friendFix is the command that sets name's settings.
func friendFix(name string) string {
	return "nova-sprint friend set " + name + " --width <n> --tiers <a,b>"
}

// runFriendSet is `friend set`: the coordinator sets a friend's width, tiers
// or both as one new data version; a flag left out keeps the setting in
// force (the defaults when nothing is recorded); the same settings again
// change nothing. The friend must be one nova-config lists, enabled or not.
func runFriendSet(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name := in.arg(0)
	if !in.has("width") && !in.has("tiers") {
		return "", misuse("friend set", "friend set takes --width <n>, --tiers <a,b> or both")
	}
	width := 0
	if in.has("width") {
		n, err := strconv.Atoi(in.str("width"))
		if err != nil {
			return "", misuse("friend set", "--width takes a whole number ("+badNumber(in.str("width"), err)+")")
		}
		width = n
	}
	lim, _, err := e.rules(ctx, snap)
	if err != nil {
		return "", err
	}
	ro, err := e.roster(ctx, lim, snap)
	if err != nil {
		return "", err
	}
	if !ro.Friends[name] {
		return "", refuse("nova-config lists no friend "+name+"; a friend's sprint settings are set only for a friend it lists", "nova-config friend add "+name+" --harness <harness> --model <provider/model> --actor <you> --reason <why>, then friend set again")
	}
	read := func() (roster.FriendRecord, bool, error) {
		vs, err := e.Store.PoliciesNamed(ctx, "data", roster.FriendPrefix+name, 1)
		if err != nil {
			return roster.FriendRecord{}, false, err
		}
		rec, ok := roster.FriendSettings(vs)[name]
		return rec, ok, nil
	}
	was, recorded, err := read()
	if err != nil {
		return "", err
	}
	inForce := roster.DefaultFriend()
	if recorded {
		inForce = was.Setting
	}
	if recorded && was.Unread != "" && !(in.has("width") && in.has("tiers")) {
		return "", refuse("friend "+name+"'s sprint settings cannot be read ("+was.Unread+"), so a flag left out has nothing to keep", friendFix(name))
	}
	want := inForce
	if in.has("width") {
		want.Width = width
	}
	if in.has("tiers") {
		want.Tiers = strings.Split(in.str("tiers"), ",")
	}
	if p := want.Problem(); p != "" {
		return "", refuse("friend "+name+": "+p, friendFix(name))
	}
	body := want.Body()
	shown := func(s roster.FriendSetting) string {
		return fmt.Sprintf("width %d tiers %s", s.Width, store.JoinTiers(s.Tiers))
	}
	if body == inForce.Body() && (!recorded || was.Unread == "") {
		return done("nova-sprint show "+name, "FRIEND "+name+" already "+shown(want)+"; nothing changed"), nil
	}
	plan := func(cur store.Snapshot) (store.Op, error) {
		now, _, err := read()
		if err != nil {
			return store.Op{}, err
		}
		if now.Version != was.Version {
			return store.Op{}, refuse(fmt.Sprintf("friend %s's sprint settings changed since this verb read them (policy v%d, now v%d)", name, was.Version, now.Version), "nova-sprint show "+name+", then friend set again")
		}
		op := e.newOp("friend-set", in.actor, cur, name, body)
		op.Expect = []store.Expect{store.UnchangedSince{Seq: cur.Seq, Policies: true, Hooks: true}} // no other policy write, and the caller still the coordinator it was read as
		op.Writes, op.Note = []store.Write{store.PutPolicy{Kind: "data", Name: roster.FriendPrefix + name, Body: body}}, "friend "+name+" "+shown(want)
		return op, nil
	}
	// another row's hook or policy write between is not its own: it reads again (fenced)
	r, _, err := e.fenced(ctx, snap, "nova-sprint friend set "+name+" again", hookChange(in.actor), plan)
	if err != nil {
		return "", err
	}
	stored, _, err := read()
	if err != nil {
		return "", err
	}
	return done("nova-sprint show "+name, fmt.Sprintf("FRIEND %s %s applied at seq %d (policy v%d, %s%s); the run deals to it so from its next tick", name, shown(want), r.Seq, stored.Version, roster.FriendPrefix, name)), nil
}

// friendView is a friend's sprint settings as show prints them: the
// settings in force, the data version holding them (0: none, the defaults),
// why the record cannot be read, and the verb that changes them.
type friendView struct {
	Width   int      `json:"width"`
	Tiers   []string `json:"tiers"`
	Version int      `json:"version,omitempty"`
	Unread  string   `json:"unread,omitempty"`
	Set     string   `json:"set"`
}

// friendOf is name's friendView, read from the store.
func (e *Engine) friendOf(ctx context.Context, name string) (friendView, error) {
	vs, err := e.Store.PoliciesNamed(ctx, "data", roster.FriendPrefix+name, 1)
	if err != nil {
		return friendView{}, err
	}
	rec, ok := roster.FriendSettings(vs)[name]
	s := roster.DefaultFriend()
	if ok {
		s = rec.Setting
	}
	return friendView{Width: s.Width, Tiers: nonNil(s.Tiers), Version: rec.Version, Unread: rec.Unread, Set: friendFix(name)}, nil
}
