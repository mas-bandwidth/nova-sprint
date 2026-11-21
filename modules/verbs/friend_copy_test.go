package verbs

import (
	"reflect"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestFriendCopies: each friend the store has no settings for is copied
// from its old nova-config columns (tiers in the store's order); one with a
// record, readable or not, is never copied over; one whose old settings
// serve no tier is not copied and its line says it works at the defaults
// and how to set it.
func TestFriendCopies(t *testing.T) {
	t.Parallel()
	friends := []configFriend{{Name: "alex", Width: 1, Tiers: "flash,frontier,heavy,pro"}, {Name: "emma", Width: 4, Tiers: "pro"}, {Name: "nil", Width: 1, Tiers: ""}, {Name: "set", Width: 9, Tiers: "pro"}, {Name: "stella", Width: 3, Tiers: "heavy,pro"}}
	have := map[string]roster.FriendRecord{"set": {Setting: roster.FriendSetting{Width: 2, Tiers: []string{"flash"}}, Version: 7}, "emma": {Version: 8, Unread: "bad"}}
	writes, lines := friendCopies(friends, have)
	wantWrites := []store.Write{
		store.PutPolicy{Kind: "data", Name: "friend:alex", Body: `{"width":1,"tiers":["flash","pro","heavy","frontier"]}`},
		store.PutPolicy{Kind: "data", Name: "friend:stella", Body: `{"width":3,"tiers":["pro","heavy"]}`},
	}
	if !reflect.DeepEqual(writes, wantWrites) {
		t.Fatalf("writes %+v", writes)
	}
	wantLines := []string{
		"friend alex: width 1 tiers flash,pro,heavy,frontier copied from nova-config",
		`friend nil: nova-config's width 1 tiers "" not copied (it serves no tier: name one or more of flash,pro,heavy,frontier); it works at width 1 tiers flash,pro,heavy,frontier until nova-sprint friend set nil --width <n> --tiers <a,b>`,
		"friend stella: width 3 tiers pro,heavy copied from nova-config",
	}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("lines %q", lines)
	}
	if w, l := friendCopies(friends[:1], map[string]roster.FriendRecord{"alex": {Version: 1}}); w != nil || l != nil {
		t.Fatalf("a second copy wrote %v %v", w, l)
	}
}
